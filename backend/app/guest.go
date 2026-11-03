package everlore

import (
	"errors"
	"fmt"
	"html"
	"math/rand/v2"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"
)

func migrateGuestChapters() error {
	return execBatch(`CREATE TABLE IF NOT EXISTS guest_claims (work_id INTEGER PRIMARY KEY REFERENCES works(id) ON DELETE CASCADE, email TEXT NOT NULL COLLATE NOCASE, token_hash TEXT NOT NULL UNIQUE, expires INTEGER NOT NULL, verified INTEGER NOT NULL DEFAULT 0); CREATE INDEX IF NOT EXISTS guest_claims_email_idx ON guest_claims(email,verified); CREATE INDEX IF NOT EXISTS guest_claims_expiry_idx ON guest_claims(verified,expires); CREATE TABLE IF NOT EXISTS chapters (id INTEGER PRIMARY KEY, work_id INTEGER NOT NULL REFERENCES works(id) ON DELETE CASCADE, number INTEGER NOT NULL CHECK(number>=2), title TEXT NOT NULL, content TEXT NOT NULL, UNIQUE(work_id,number));`)
}
func submitGuestWork(email, name string, work Work) error {
	// Occasionally remove expired, unconfirmed drafts in bounded batches.
	if rand.IntN(32) == 0 {
		if err := cleanupExpiredGuestWorks(); err != nil {
			return errors.New("Could not submit story. Please try again.")
		}
	}
	email = strings.ToLower(strings.TrimSpace(email))
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || len(email) > 254 || len(name) < 2 || len(name) > 80 {
		return errors.New("Enter a valid email address and a display name of 2–80 characters.")
	}
	token, err := randomToken()
	if err != nil {
		return errors.New("Could not submit story. Please try again.")
	}
	tx, err := db.Begin()
	if err != nil {
		return errors.New("Could not submit story. Please try again.")
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO works(title,author,fandom,rating,warnings,tags,summary,content,status,language,words,updated,published) VALUES(?,?,?,?,?,?,?,?,?,?,?, ?,0)`, work.Title, "Guest: "+name, work.Fandom, work.Rating, work.Warnings, work.Tags, work.Summary, work.Content, work.Status, "English", len(strings.Fields(work.Content)), time.Now().Format("2006-01-02"))
	if err != nil {
		return errors.New("Could not submit story. Please try again.")
	}
	id, err := res.LastInsertId()
	if err != nil {
		return errors.New("Could not submit story. Please try again.")
	}
	_, err = tx.Exec(`INSERT INTO guest_claims(work_id,email,token_hash,expires) VALUES(?,?,?,?)`, id, email, sessionHash(token), time.Now().Add(24*time.Hour).Unix())
	if err != nil {
		return errors.New("Could not submit story. Please try again.")
	}
	if err = tx.Commit(); err != nil {
		return errors.New("Could not submit story. Please try again.")
	}
	if err = sendMail(email, "guest", token); err != nil {
		cleanup, e := db.Begin()
		if e == nil {
			_, e = cleanup.Exec("DELETE FROM guest_claims WHERE work_id=? AND verified=0", id)
			if e == nil {
				_, e = cleanup.Exec("DELETE FROM works WHERE id=? AND published=0", id)
			}
			if e == nil {
				e = cleanup.Commit()
			} else {
				cleanup.Rollback()
			}
		}
		return errors.New("We could not send a confirmation email. Your story was not published; please try again.")
	}
	return nil
}
func cleanupExpiredGuestWorks() error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Keep expired links recoverable for six additional days.
	now := time.Now().Add(-6 * 24 * time.Hour).Unix()
	rows, err := tx.Query(`SELECT work_id FROM guest_claims WHERE verified=0 AND expires<? LIMIT 50`, now)
	if err != nil {
		return err
	}
	var ids []int
	for rows.Next() {
		var id int
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		res, e := tx.Exec(`DELETE FROM guest_claims WHERE work_id=? AND verified=0 AND expires<?`, id, now)
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if e != nil {
			return e
		}
		if n == 1 {
			if _, e = tx.Exec(`DELETE FROM works WHERE id=? AND user_id IS NULL AND published=0`, id); e != nil {
				return e
			}
		}
	}
	return tx.Commit()
}
func guestVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "same-origin")
		fmt.Fprintf(w, `<!doctype html><html lang="en"><meta charset="utf-8"><title>Confirm your story</title><body><main><h1>Confirm your story</h1><p>Confirm to publish your story in Everlore.</p><form method="post"><input type="hidden" name="csrf" value="%s"><input type="hidden" name="token" value="%s"><button>Publish story</button></form></main></body></html>`, html.EscapeString(csrfToken(w, r)), html.EscapeString(r.URL.Query().Get("token")))
		return
	}
	if !apiPost(w, r) {
		return
	}
	token := r.FormValue("token")
	if len(token) != 64 {
		http.Error(w, "Invalid or expired confirmation link.", 400)
		return
	}
	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Could not publish story.", 500)
		return
	}
	defer tx.Rollback()
	var id int
	err = tx.QueryRow(`SELECT work_id FROM guest_claims WHERE token_hash=? AND verified=0 AND expires>?`, sessionHash(token), time.Now().Unix()).Scan(&id)
	if err != nil {
		http.Error(w, "Invalid or expired confirmation link.", 400)
		return
	}
	if _, err = tx.Exec("UPDATE guest_claims SET verified=1 WHERE work_id=? AND verified=0", id); err == nil {
		_, err = tx.Exec("UPDATE works SET published=1 WHERE id=? AND user_id IS NULL", id)
	}
	if err != nil || tx.Commit() != nil {
		http.Error(w, "Could not publish story.", 500)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/work/%d", id), 303)
}
func claimableWorks(uid int) []Work {
	var email string
	var verified int
	if db.QueryRow("SELECT COALESCE(email,''),email_verified FROM users WHERE id=?", uid).Scan(&email, &verified) != nil || email == "" || verified != 1 {
		return nil
	}
	return queryWorks(" AND user_id IS NULL AND id IN (SELECT work_id FROM guest_claims WHERE verified=1 AND email=? COLLATE NOCASE) ORDER BY id DESC", email)
}
func claimGuestWork(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	uid, name := current(r)
	if uid == 0 {
		http.Redirect(w, r, "/login", 303)
		return
	}
	id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/guest/claim/"))
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Could not claim story", 500)
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE works SET user_id=?,author=? WHERE id=? AND user_id IS NULL AND id IN (SELECT g.work_id FROM guest_claims g JOIN users u ON u.id=? AND u.email_verified=1 AND u.suspended=0 AND u.email=g.email COLLATE NOCASE WHERE g.verified=1)`, uid, name, id, uid)
	if err != nil {
		http.Error(w, "Could not claim story", 500)
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		http.NotFound(w, r)
		return
	}
	if _, err = tx.Exec("DELETE FROM guest_claims WHERE work_id=?", id); err != nil {
		http.Error(w, "Could not claim story", 500)
		return
	}
	if err = tx.Commit(); err != nil {
		http.Error(w, "Could not claim story", 500)
		return
	}
	http.Redirect(w, r, "/dashboard", 303)
}

// resendGuestConfirmation never reveals whether a given email has a submission.
func resendGuestConfirmation(w http.ResponseWriter, r *http.Request) {
	if !apiPost(w, r) {
		return
	}
	if !allowAuth(r) {
		apiError(w, 429, "Too many attempts. Please try again later.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	rows, err := db.Query(`SELECT work_id FROM guest_claims WHERE email=? COLLATE NOCASE AND verified=0 AND expires>? ORDER BY work_id DESC LIMIT 5`, email, time.Now().Add(-6*24*time.Hour).Unix())
	if err == nil {
		var ids []int
		for rows.Next() {
			var id int
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		for _, id := range ids {
			token, e := randomToken()
			if e != nil {
				continue
			}
			// Do not invalidate an existing link when email delivery fails.
			if sendMail(email, "guest", token) == nil {
				_, _ = db.Exec(`UPDATE guest_claims SET token_hash=?,expires=? WHERE work_id=? AND verified=0`, sessionHash(token), time.Now().Add(24*time.Hour).Unix(), id)
			}
		}
	}
	apiReply(w, 200, map[string]string{"message": "If there are eligible submissions, confirmation links have been sent."})
}
