package everlore

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var validUsername = regexp.MustCompile(`^[A-Za-z0-9_-]{3,30}$`)

func sessionHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func migrate() {
	for _, q := range []string{
		`ALTER TABLE users ADD COLUMN display_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN bio TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := db.Exec(q); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			must(err)
		}
	}
	err := execBatch(`CREATE TABLE IF NOT EXISTS pseudonyms (id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, name TEXT NOT NULL COLLATE NOCASE UNIQUE); CREATE INDEX IF NOT EXISTS works_owner_idx ON works(user_id); CREATE INDEX IF NOT EXISTS works_recent_published_idx ON works(updated DESC, id DESC) WHERE published=1; CREATE INDEX IF NOT EXISTS kudos_work_idx ON kudos(work_id); CREATE INDEX IF NOT EXISTS comments_work_idx ON comments(work_id, id); CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions(expires); CREATE UNIQUE INDEX IF NOT EXISTS users_name_nocase ON users(username COLLATE NOCASE); DELETE FROM sessions WHERE expires < strftime('%s','now');`)
	must(err)
}
func csrfToken(w http.ResponseWriter, r *http.Request) string {
	if c, e := r.Cookie("csrf"); e == nil && len(c.Value) == 64 {
		return c.Value
	}
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	token := hex.EncodeToString(b)
	http.SetCookie(w, &http.Cookie{Name: "csrf", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secureCookie(r), MaxAge: 86400})
	return token
}
func security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		if secureCookie(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if _, err := r.Cookie("session"); err == nil || strings.HasPrefix(r.URL.Path, "/api/account/") || strings.HasPrefix(r.URL.Path, "/guest/verify") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
			if err := r.ParseForm(); err != nil {
				http.Error(w, "Invalid request", 400)
				return
			}
			if !sameOrigin(r) {
				http.Error(w, "Cross-origin request rejected", http.StatusForbidden)
				return
			}
			c, e := r.Cookie("csrf")
			t := r.PostForm.Get("csrf")
			if e != nil || len(t) != 64 || subtle.ConstantTimeCompare([]byte(c.Value), []byte(t)) != 1 {
				http.Error(w, "Your form expired. Reload the page and try again.", http.StatusForbidden)
				return
			}
		} else if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method not allowed", 405)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin supplements the double-submit token. Browsers attach Origin to
// unsafe cross-site requests, preventing a sibling subdomain from pairing a
// forged CSRF cookie with a forged form token.
func sameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host != "" && strings.EqualFold(u.Host, r.Host) && (u.Scheme == "https" || u.Scheme == "http")
}
func pseudonyms(uid int) []Pseudonym {
	rows, e := db.Query("SELECT id,name FROM pseudonyms WHERE user_id=? ORDER BY name", uid)
	if e != nil {
		return nil
	}
	defer rows.Close()
	var out []Pseudonym
	for rows.Next() {
		var p Pseudonym
		rows.Scan(&p.ID, &p.Name)
		out = append(out, p)
	}
	return out
}
func profile(w http.ResponseWriter, r *http.Request) {
	uid, name := current(r)
	if uid == 0 {
		http.Redirect(w, r, "/login", 303)
		return
	}
	p := Page{Title: "Profile and account", ProfileName: name}
	if r.Method == "POST" {
		switch r.FormValue("action") {
		case "profile":
			display := strings.TrimSpace(r.FormValue("display_name"))
			bio := strings.TrimSpace(r.FormValue("bio"))
			if len(display) > 80 || len(bio) > 3000 {
				p.Error = "Display name or biography is too long."
			} else {
				_, e := db.Exec("UPDATE users SET display_name=?, bio=? WHERE id=?", display, bio, uid)
				if e != nil {
					p.Error = "Could not save your profile."
				} else {
					p.Message = "Profile saved."
				}
			}
		case "pseudonym":
			pseud := strings.TrimSpace(r.FormValue("pseudonym"))
			if !validUsername.MatchString(pseud) {
				p.Error = "Pseudonyms must be 3–30 letters, numbers, underscores or hyphens."
			} else {
				res, e := db.Exec("INSERT INTO pseudonyms(user_id,name) SELECT ?,? WHERE NOT EXISTS (SELECT 1 FROM users WHERE username=? COLLATE NOCASE)", uid, pseud, pseud)
				if e == nil {
					if n, _ := res.RowsAffected(); n != 1 {
						e = fmt.Errorf("name unavailable")
					}
				}
				if e != nil {
					p.Error = "That pseudonym is already in use."
				} else {
					p.Message = "Pseudonym added."
				}
			}
		case "password":
			old, newpass := r.FormValue("old_password"), r.FormValue("new_password")
			var hash string
			db.QueryRow("SELECT password FROM users WHERE id=?", uid).Scan(&hash)
			if bcrypt.CompareHashAndPassword([]byte(hash), []byte(old)) != nil {
				p.Error = "Current password is incorrect."
			} else if len(newpass) < 8 || len(newpass) > 72 {
				p.Error = "The new password must be 8–72 bytes."
			} else {
				newhash, e := bcrypt.GenerateFromPassword([]byte(newpass), bcrypt.DefaultCost)
				if e != nil {
					p.Error = "Could not change password."
				} else {
					tx, e := db.Begin()
					if e == nil {
						_, e = tx.Exec("UPDATE users SET password=? WHERE id=?", string(newhash), uid)
						if e == nil {
							_, e = tx.Exec("DELETE FROM sessions WHERE user_id=?", uid)
						}
						if e == nil {
							e = tx.Commit()
						} else {
							tx.Rollback()
						}
					}
					if e != nil {
						p.Error = "Could not change password."
					} else {
						clearSessionCookie(w, r)
						http.Redirect(w, r, "/login", 303)
						return
					}
				}
			}
		default:
			http.Error(w, "Invalid action", 400)
			return
		}
	}
	db.QueryRow("SELECT display_name,bio FROM users WHERE id=?", uid).Scan(&p.DisplayName, &p.Bio)
	p.Pseudonyms = pseudonyms(uid)
	render(w, r, "profile", p)
}
func publicProfile(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/people/")
	var uid int
	var display, bio, username string
	err := db.QueryRow("SELECT id,username,display_name,bio FROM users WHERE username=? COLLATE NOCASE", name).Scan(&uid, &username, &display, &bio)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	viewer, _ := current(r)
	render(w, r, "publicprofile", Page{Title: username, ProfileName: username, DisplayName: display, Bio: bio, Works: queryWorks(" AND user_id=? ORDER BY updated DESC", uid), ProfileID: uid, Subscribed: subscribed(viewer, "author", uid), CanSubscribe: viewer != 0 && viewer != uid})
}
func editWork(w http.ResponseWriter, r *http.Request) {
	uid, _ := current(r)
	if uid == 0 {
		http.Redirect(w, r, "/login", 303)
		return
	}
	id, e := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/edit/"))
	if e != nil {
		http.NotFound(w, r)
		return
	}
	works := queryWorksWithContent(" AND id=? AND user_id=?", id, uid)
	if len(works) == 0 {
		http.NotFound(w, r)
		return
	}
	p := Page{Title: "Edit story", Work: works[0]}
	if r.Method == "POST" {
		title := strings.TrimSpace(r.FormValue("title"))
		content := strings.TrimSpace(r.FormValue("content"))
		fandom := storedFandom(strings.TrimSpace(r.FormValue("fandom")))
		p.Work.Title, p.Work.Content, p.Work.Fandom = title, content, fandom
		p.Work.Summary, p.Work.Tags = r.FormValue("summary"), r.FormValue("tags")
		notices := r.Form["warnings"]
		if len(notices) > 0 {
			p.Work.Warnings = strings.Join(notices, ", ")
		}
		if title == "" || content == "" || fandom == "" || (len(notices) > 0 && !validWarnings(notices)) {
			p.Error = "Title, fandom and story text are required. Choose valid notices, with None apply on its own."
		} else {
			e := updateWorkWithChapterWords(uid, id, title, fandom, r.FormValue("summary"), r.FormValue("tags"), content, p.Work.Warnings)
			if e == nil {
				http.Redirect(w, r, fmt.Sprintf("/work/%d", id), 303)
				return
			}
			p.Error = "Could not save your story."
		}
	}
	render(w, r, "edit", p)
}
