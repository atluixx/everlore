package everlore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func apiReply(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
func apiError(w http.ResponseWriter, status int, msg string) {
	apiReply(w, status, map[string]string{"error": msg})
}
func apiUser(w http.ResponseWriter, r *http.Request) int {
	id, _ := current(r)
	if id == 0 {
		apiError(w, 401, "Authentication required")
	}
	return id
}
func apiPost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != "POST" {
		apiError(w, 405, "POST required")
		return false
	}
	return true
}
func apiSchema() {
	for _, q := range []string{`ALTER TABLE users ADD COLUMN email TEXT`, `ALTER TABLE users ADD COLUMN email_verified INTEGER NOT NULL DEFAULT 0`, `ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user'`, `ALTER TABLE users ADD COLUMN suspended INTEGER NOT NULL DEFAULT 0`} {
		if _, e := db.Exec(q); e != nil && !strings.Contains(e.Error(), "duplicate column name") {
			must(e)
		}
	}
	e := execBatch(`CREATE UNIQUE INDEX IF NOT EXISTS users_email_unique ON users(email COLLATE NOCASE) WHERE email IS NOT NULL; CREATE TABLE IF NOT EXISTS account_tokens (token_hash TEXT PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, purpose TEXT NOT NULL, expires INTEGER NOT NULL); CREATE TABLE IF NOT EXISTS rate_limits (key TEXT NOT NULL, created INTEGER NOT NULL); CREATE INDEX IF NOT EXISTS rate_limits_idx ON rate_limits(key,created); CREATE INDEX IF NOT EXISTS rate_limits_created_idx ON rate_limits(created); CREATE TABLE IF NOT EXISTS reports (id INTEGER PRIMARY KEY, reporter_id INTEGER REFERENCES users(id) ON DELETE SET NULL, work_id INTEGER REFERENCES works(id), reason TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'open', created INTEGER NOT NULL, resolved_by INTEGER REFERENCES users(id) ON DELETE SET NULL, resolution TEXT NOT NULL DEFAULT ''); CREATE TABLE IF NOT EXISTS audit_log (id INTEGER PRIMARY KEY, actor_id INTEGER REFERENCES users(id) ON DELETE SET NULL, action TEXT NOT NULL, target TEXT NOT NULL, created INTEGER NOT NULL);`)
	must(e)
}
func accountRegister(w http.ResponseWriter, r *http.Request) {
	if !apiPost(w, r) {
		return
	}
	if !allowAuth(r) {
		apiError(w, 429, "Too many attempts")
		return
	}
	name := strings.TrimSpace(r.FormValue("username"))
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	pass := r.FormValue("password")
	addr, e := mail.ParseAddress(email)
	if !validUsername.MatchString(name) || e != nil || addr.Address != email || len(email) > 254 || len(pass) < 8 || len(pass) > 72 {
		apiError(w, 400, "Invalid username, email, or password")
		return
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if e != nil {
		apiError(w, 500, "Could not create account")
		return
	}
	res, e := db.Exec("INSERT INTO users(username,password,email,email_verified) SELECT ?,?,?,0 WHERE NOT EXISTS (SELECT 1 FROM pseudonyms WHERE name=? COLLATE NOCASE)", name, string(hash), email, name)
	if e != nil {
		apiError(w, 409, "Username or email is unavailable")
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		apiError(w, 409, "Username or email is unavailable")
		return
	}
	id, _ := res.LastInsertId()
	if e = issueToken(int(id), email, "verify"); e != nil {
		log.Printf("verification delivery failed: %v", e)
		apiError(w, 503, "Account created, but email delivery failed. Request a new verification email later.")
		return
	}
	apiReply(w, 201, map[string]string{"message": "Check your email to verify your account."})
}
func accountRequestToken(w http.ResponseWriter, r *http.Request, purpose string) {
	if !apiPost(w, r) {
		return
	}
	if !allowAuth(r) {
		apiError(w, 429, "Too many attempts")
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	var id int
	var verified int
	e := db.QueryRow("SELECT id,email_verified FROM users WHERE email=? COLLATE NOCASE AND suspended=0", email).Scan(&id, &verified)
	if e == nil && (purpose == "reset" && verified == 1 || purpose == "verify" && verified == 0) {
		if e = issueToken(id, email, purpose); e != nil {
			log.Printf("account email delivery failed: %v", e)
		}
	}
	apiReply(w, 200, map[string]string{"message": "If the account is eligible, an email has been sent."})
}
func consumeToken(tx *sql.Tx, token, purpose string) (int, error) {
	var uid int
	if len(token) != 64 {
		return 0, errors.New("invalid token")
	}
	err := tx.QueryRow("SELECT user_id FROM account_tokens WHERE token_hash=? AND purpose=? AND expires>?", sessionHash(token), purpose, time.Now().Unix()).Scan(&uid)
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec("DELETE FROM account_tokens WHERE user_id=? AND purpose=?", uid, purpose)
	return uid, err
}
func accountVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "same-origin")
		fmt.Fprintf(w, `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Verifying your email — Everlore</title><link rel="stylesheet" href="/static/style.css"><script src="/static/verify.js" defer></script><body><main class="wrap page narrow"><h1>Verifying your email</h1><p id="verification-status" role="status" aria-live="polite">Please wait while we verify your email.</p><noscript><p>Enable JavaScript to verify your email automatically.</p></noscript><form id="verification-form" method="post"><input type="hidden" name="csrf" value="%s"><input type="hidden" name="token" value="%s"></form><p><a href="/login">Go to log in</a> · <a href="/verification">Request a new link</a></p></main></body></html>`, html.EscapeString(csrfToken(w, r)), html.EscapeString(r.URL.Query().Get("token")))
		return
	}
	if !apiPost(w, r) {
		return
	}
	tx, e := db.Begin()
	if e != nil {
		apiError(w, 500, "Could not verify account")
		return
	}
	defer tx.Rollback()
	uid, e := consumeToken(tx, r.FormValue("token"), "verify")
	if e == nil {
		_, e = tx.Exec("UPDATE users SET email_verified=1 WHERE id=?", uid)
	}
	if e != nil {
		apiError(w, 400, "Invalid or expired token")
		return
	}
	if e = tx.Commit(); e != nil {
		apiError(w, 500, "Could not verify account")
		return
	}
	apiReply(w, 200, map[string]string{"message": "Email verified."})
}
func accountReset(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		tokenForm(w, r, true)
		return
	}
	if !apiPost(w, r) {
		return
	}
	pass := r.FormValue("password")
	if len(pass) < 8 || len(pass) > 72 {
		apiError(w, 400, "Password must be 8–72 bytes")
		return
	}
	// Reject bogus tokens before running bcrypt. Otherwise an unauthenticated
	// caller can turn every cheap request into expensive password hashing.
	var tokenUser int
	if len(r.FormValue("token")) != 64 || db.QueryRow("SELECT user_id FROM account_tokens WHERE token_hash=? AND purpose='reset' AND expires>?", sessionHash(r.FormValue("token")), time.Now().Unix()).Scan(&tokenUser) != nil {
		apiError(w, 400, "Invalid or expired token")
		return
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if e != nil {
		apiError(w, 500, "Could not reset password")
		return
	}
	tx, e := db.Begin()
	if e != nil {
		apiError(w, 500, "Could not reset password")
		return
	}
	defer tx.Rollback()
	uid, e := consumeToken(tx, r.FormValue("token"), "reset")
	if e == nil {
		_, e = tx.Exec("UPDATE users SET password=? WHERE id=?", string(hash), uid)
	}
	if e == nil {
		_, e = tx.Exec("DELETE FROM sessions WHERE user_id=?", uid)
	}
	if e != nil {
		apiError(w, 400, "Invalid or expired token")
		return
	}
	if e = tx.Commit(); e != nil {
		apiError(w, 500, "Could not reset password")
		return
	}
	apiReply(w, 200, map[string]string{"message": "Password changed. Please log in."})
}
func accountDelete(w http.ResponseWriter, r *http.Request) {
	if !apiPost(w, r) {
		return
	}
	uid := apiUser(w, r)
	if uid == 0 {
		return
	}
	var hash string
	db.QueryRow("SELECT password FROM users WHERE id=?", uid).Scan(&hash)
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(r.FormValue("password"))) != nil {
		apiError(w, 403, "Incorrect password")
		return
	}
	tx, e := db.Begin()
	if e != nil {
		apiError(w, 500, "Could not delete account")
		return
	}
	defer tx.Rollback() // Published works and their author labels are retained as orphaned archive works.
	for _, q := range []string{"UPDATE works SET user_id=NULL WHERE user_id=?", "DELETE FROM comments WHERE user_id=?", "DELETE FROM bookmarks WHERE user_id=?", "DELETE FROM kudos WHERE user_id=?", "DELETE FROM subscriptions WHERE user_id=?", "DELETE FROM subscriptions WHERE kind='author' AND target_id=?", "DELETE FROM notifications WHERE user_id=?", "DELETE FROM drafts WHERE user_id=?", "DELETE FROM sessions WHERE user_id=?", "DELETE FROM account_tokens WHERE user_id=?", "DELETE FROM pseudonyms WHERE user_id=?", "UPDATE reports SET reporter_id=NULL WHERE reporter_id=?", "UPDATE reports SET resolved_by=NULL WHERE resolved_by=?", "UPDATE audit_log SET actor_id=NULL WHERE actor_id=?", "DELETE FROM users WHERE id=?"} {
		if _, e = tx.Exec(q, uid); e != nil {
			apiError(w, 500, "Could not delete account")
			return
		}
	}
	if e = tx.Commit(); e != nil {
		apiError(w, 500, "Could not delete account")
		return
	}
	clearSessionCookie(w, r)
	apiReply(w, 200, map[string]string{"message": "Account deleted; published stories were orphaned."})
}
func apiPseudonyms(w http.ResponseWriter, r *http.Request) {
	uid := apiUser(w, r)
	if uid == 0 {
		return
	}
	if r.Method == "GET" {
		apiReply(w, 200, pseudonyms(uid))
		return
	}
	if !apiPost(w, r) {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	switch r.FormValue("action") {
	case "create":
		if !validUsername.MatchString(name) {
			apiError(w, 400, "Invalid pseudonym")
			return
		}
		if res, e := db.Exec("INSERT INTO pseudonyms(user_id,name) SELECT ?,? WHERE NOT EXISTS (SELECT 1 FROM users WHERE username=? COLLATE NOCASE)", uid, name, name); e != nil {
			apiError(w, 409, "Pseudonym unavailable")
			return
		} else if n, _ := res.RowsAffected(); n != 1 {
			apiError(w, 409, "Pseudonym unavailable")
			return
		}
	case "delete":
		id, e := strconv.Atoi(r.FormValue("id"))
		if e != nil {
			apiError(w, 400, "Invalid ID")
			return
		}
		res, e := db.Exec("DELETE FROM pseudonyms WHERE id=? AND user_id=?", id, uid)
		if e != nil {
			apiError(w, 500, "Could not delete pseudonym")
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			apiError(w, 404, "Pseudonym not found")
			return
		}
	default:
		apiError(w, 400, "Invalid action")
		return
	}
	apiReply(w, 200, map[string]string{"message": "Saved"})
}
func reportWork(w http.ResponseWriter, r *http.Request) {
	if !apiPost(w, r) {
		return
	}
	uid := apiUser(w, r)
	if uid == 0 {
		return
	}
	id, e := strconv.Atoi(r.FormValue("work_id"))
	reason := strings.TrimSpace(r.FormValue("reason"))
	if e != nil || len(reason) < 10 || len(reason) > 3000 {
		apiError(w, 400, "Story and reason (10–3000 characters) are required")
		return
	}
	res, e := db.Exec("INSERT INTO reports(reporter_id,work_id,reason,created) SELECT ?,id,?,? FROM works WHERE id=? AND published=1", uid, reason, time.Now().Unix(), id)
	if e != nil {
		apiError(w, 500, "Could not submit report")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		apiError(w, 404, "Story not found")
		return
	}
	apiReply(w, 201, map[string]string{"message": "Report submitted"})
}
func adminReports(w http.ResponseWriter, r *http.Request) {
	uid := apiUser(w, r)
	if uid == 0 {
		return
	}
	var role string
	db.QueryRow("SELECT role FROM users WHERE id=?", uid).Scan(&role)
	if role != "admin" {
		apiError(w, 403, "Administrator required")
		return
	}
	if r.Method == "GET" {
		rows, e := db.Query("SELECT id,work_id,reason,status,created FROM reports ORDER BY id DESC LIMIT 100")
		if e != nil {
			apiError(w, 500, "Could not load reports")
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id int
			var workID sql.NullInt64
			var reason, status string
			var created int64
			if rows.Scan(&id, &workID, &reason, &status, &created) == nil {
				out = append(out, map[string]any{"id": id, "work_id": nullableInt(workID), "reason": reason, "status": status, "created": created})
			}
		}
		apiReply(w, 200, out)
		return
	}
	if !apiPost(w, r) {
		return
	}
	id, e := strconv.Atoi(r.FormValue("id"))
	resolution := strings.TrimSpace(r.FormValue("resolution"))
	if e != nil || len(resolution) < 3 || len(resolution) > 3000 {
		apiError(w, 400, "Report ID and resolution required")
		return
	}
	tx, e := db.Begin()
	if e != nil {
		apiError(w, 500, "Could not resolve report")
		return
	}
	defer tx.Rollback()
	res, e := tx.Exec("UPDATE reports SET status='resolved',resolved_by=?,resolution=? WHERE id=? AND status='open'", uid, resolution, id)
	if e != nil {
		apiError(w, 500, "Could not resolve report")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		apiError(w, 404, "Open report not found")
		return
	}
	_, e = tx.Exec("INSERT INTO audit_log(actor_id,action,target,created) VALUES(?,?,?,?)", uid, "resolve_report", fmt.Sprint(id), time.Now().Unix())
	if e != nil || tx.Commit() != nil {
		apiError(w, 500, "Could not resolve report")
		return
	}
	apiReply(w, 200, map[string]string{"message": "Report resolved"})
}
func secureCookie(r *http.Request) bool {
	return r.TLS != nil || os.Getenv("EVERLORE_SECURE_COOKIES") == "1"
}
func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "session", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secureCookie(r), MaxAge: -1, Expires: time.Unix(1, 0)})
}
func apiCSRF(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		apiError(w, 405, "GET required")
		return
	}
	apiReply(w, 200, map[string]string{"csrf": csrfToken(w, r)})
}

func tokenForm(w http.ResponseWriter, r *http.Request, reset bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// Preserve the origin on same-origin form submissions while withholding the
	// token-bearing URL from cross-origin requests.
	w.Header().Set("Referrer-Policy", "same-origin")
	token := html.EscapeString(r.URL.Query().Get("token"))
	csrf := html.EscapeString(csrfToken(w, r))
	title := "Verify email"
	field := ""
	if reset {
		title = "Reset password"
		field = `<label>New password <input type="password" name="password" minlength="8" maxlength="72" required></label>`
	}
	fmt.Fprintf(w, `<!doctype html><html lang="en"><meta charset="utf-8"><title>%s</title><body><main><h1>%s</h1><form method="post"><input type="hidden" name="csrf" value="%s"><input type="hidden" name="token" value="%s">%s<button>%s</button></form></main></body></html>`, title, title, csrf, token, field, title)
}
func adminAction(w http.ResponseWriter, r *http.Request) {
	if !apiPost(w, r) {
		return
	}
	uid := apiUser(w, r)
	if uid == 0 {
		return
	}
	var role string
	db.QueryRow("SELECT role FROM users WHERE id=?", uid).Scan(&role)
	if role != "admin" {
		apiError(w, 403, "Administrator required")
		return
	}
	target, e := strconv.Atoi(r.FormValue("id"))
	action := r.FormValue("action")
	reason := strings.TrimSpace(r.FormValue("reason"))
	if e != nil || target < 1 || len(reason) < 3 || len(reason) > 3000 {
		apiError(w, 400, "Target and reason are required")
		return
	}
	tx, e := db.Begin()
	if e != nil {
		apiError(w, 500, "Could not apply action")
		return
	}
	defer tx.Rollback()
	var res sql.Result
	switch action {
	case "hide_work":
		res, e = tx.Exec("UPDATE works SET published=0 WHERE id=? AND published=1", target)
	case "restore_work":
		res, e = tx.Exec("UPDATE works SET published=1 WHERE id=? AND published=0 AND NOT EXISTS (SELECT 1 FROM guest_claims WHERE work_id=? AND verified=0)", target, target)
	case "suspend_user":
		if target == uid {
			apiError(w, 400, "Cannot suspend your own account")
			return
		}
		res, e = tx.Exec("UPDATE users SET suspended=1 WHERE id=? AND suspended=0", target)
		if e == nil {
			_, e = tx.Exec("DELETE FROM sessions WHERE user_id=?", target)
		}
	case "restore_user":
		res, e = tx.Exec("UPDATE users SET suspended=0 WHERE id=? AND suspended=1", target)
	default:
		apiError(w, 400, "Unknown action")
		return
	}
	if e != nil {
		apiError(w, 500, "Could not apply action")
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		apiError(w, 404, "Target not found or already in that state")
		return
	}
	_, e = tx.Exec("INSERT INTO audit_log(actor_id,action,target,created) VALUES(?,?,?,?)", uid, action, fmt.Sprintf("%d: %s", target, reason), time.Now().Unix())
	if e != nil || tx.Commit() != nil {
		apiError(w, 500, "Could not apply action")
		return
	}
	apiReply(w, 200, map[string]string{"message": "Action recorded"})
}

func deleteWork(w http.ResponseWriter, r *http.Request) {
	if !apiPost(w, r) {
		return
	}
	uid := apiUser(w, r)
	if uid == 0 {
		return
	}
	id, e := strconv.Atoi(r.FormValue("id"))
	if e != nil || id < 1 {
		apiError(w, 400, "Invalid story ID")
		return
	}
	tx, e := db.Begin()
	if e != nil {
		apiError(w, 500, "Could not delete story")
		return
	}
	defer tx.Rollback()
	var exists int
	e = tx.QueryRow("SELECT 1 FROM works WHERE id=? AND user_id=?", id, uid).Scan(&exists)
	if e != nil {
		apiError(w, 404, "Story not found")
		return
	}
	for _, q := range []string{"UPDATE reports SET work_id=NULL WHERE work_id=?", "DELETE FROM comments WHERE work_id=?", "DELETE FROM bookmarks WHERE work_id=?", "DELETE FROM kudos WHERE work_id=?", "DELETE FROM chapters WHERE work_id=?", "DELETE FROM guest_claims WHERE work_id=?", "DELETE FROM subscriptions WHERE kind='story' AND target_id=?", "DELETE FROM notifications WHERE work_id=?", "DELETE FROM works WHERE id=?"} {
		if _, e = tx.Exec(q, id); e != nil {
			apiError(w, 500, "Could not delete story")
			return
		}
	}
	if e = tx.Commit(); e != nil {
		apiError(w, 500, "Could not delete story")
		return
	}
	apiReply(w, 200, map[string]string{"message": "Story deleted"})
}

func nullableInt(n sql.NullInt64) any {
	if !n.Valid {
		return nil
	}
	return n.Int64
}
func deleteComment(w http.ResponseWriter, r *http.Request) {
	if !apiPost(w, r) {
		return
	}
	uid := apiUser(w, r)
	if uid == 0 {
		return
	}
	id, e := strconv.Atoi(r.FormValue("id"))
	if e != nil {
		apiError(w, 400, "Invalid comment ID")
		return
	}
	res, e := db.Exec(`DELETE FROM comments WHERE id=? AND (user_id=? OR work_id IN (SELECT id FROM works WHERE user_id=?))`, id, uid, uid)
	if e != nil {
		apiError(w, 500, "Could not delete comment")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		apiError(w, 404, "Comment not found")
		return
	}
	apiReply(w, 200, map[string]string{"message": "Comment deleted"})
}
func adminAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		apiError(w, 405, "GET required")
		return
	}
	uid := apiUser(w, r)
	if uid == 0 {
		return
	}
	var role string
	db.QueryRow("SELECT role FROM users WHERE id=?", uid).Scan(&role)
	if role != "admin" {
		apiError(w, 403, "Administrator required")
		return
	}
	rows, e := db.Query("SELECT id,actor_id,action,target,created FROM audit_log ORDER BY id DESC LIMIT 100")
	if e != nil {
		apiError(w, 500, "Could not load audit log")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int
		var actor sql.NullInt64
		var action, target string
		var created int64
		if rows.Scan(&id, &actor, &action, &target, &created) == nil {
			out = append(out, map[string]any{"id": id, "actor_id": nullableInt(actor), "action": action, "target": target, "created": created})
		}
	}
	apiReply(w, 200, out)
}
