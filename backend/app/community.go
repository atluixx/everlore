package everlore

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Subscription struct {
	Kind       string
	TargetID   int
	Label, URL string
}
type Notification struct {
	ID      int
	StoryID int64
	Message string
	Created string
	Read    bool
}

func migrateCommunity() error {
	return execBatch(`CREATE TABLE IF NOT EXISTS subscriptions (user_id INTEGER NOT NULL REFERENCES users(id), kind TEXT NOT NULL CHECK(kind IN ('story','author')), target_id INTEGER NOT NULL, PRIMARY KEY(user_id,kind,target_id)); CREATE INDEX IF NOT EXISTS subscriptions_target_idx ON subscriptions(kind,target_id); CREATE TABLE IF NOT EXISTS notifications (id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id), work_id INTEGER REFERENCES works(id), message TEXT NOT NULL, created TEXT NOT NULL, read INTEGER NOT NULL DEFAULT 0); CREATE INDEX IF NOT EXISTS notifications_user_idx ON notifications(user_id,id DESC);`)
}
func subscribed(uid int, kind string, target int) bool {
	if uid == 0 || target == 0 {
		return false
	}
	var exists int
	return db.QueryRow("SELECT 1 FROM subscriptions WHERE user_id=? AND kind=? AND target_id=?", uid, kind, target).Scan(&exists) == nil
}
func subscriptionPage(w http.ResponseWriter, r *http.Request) {
	uid, _ := current(r)
	if uid == 0 {
		http.Redirect(w, r, "/login", 303)
		return
	}
	if r.Method == "POST" {
		kind, action := r.FormValue("kind"), r.FormValue("action")
		id, err := strconv.Atoi(r.FormValue("id"))
		if err != nil || id < 1 || (kind != "story" && kind != "author") || (action != "subscribe" && action != "unsubscribe") {
			http.Error(w, "Invalid subscription", 400)
			return
		}
		if action == "subscribe" {
			var owner int
			if kind == "story" {
				err = db.QueryRow("SELECT COALESCE(user_id,0) FROM works WHERE id=? AND published=1", id).Scan(&owner)
			} else {
				err = db.QueryRow("SELECT id FROM users WHERE id=? AND suspended=0", id).Scan(&owner)
			}
			if err != nil {
				http.NotFound(w, r)
				return
			}
			if owner == uid {
				http.Error(w, "You cannot subscribe to yourself", 400)
				return
			}
			_, err = db.Exec("INSERT OR IGNORE INTO subscriptions(user_id,kind,target_id) VALUES(?,?,?)", uid, kind, id)
		} else {
			_, err = db.Exec("DELETE FROM subscriptions WHERE user_id=? AND kind=? AND target_id=?", uid, kind, id)
		}
		if err != nil {
			http.Error(w, "Could not update subscription", 500)
			return
		}
		http.Redirect(w, r, "/subscriptions", 303)
		return
	}
	rows, err := db.Query(`SELECT s.kind,s.target_id,CASE WHEN s.kind='story' THEN w.title ELSE u.username END, CASE WHEN s.kind='story' THEN '/work/'||w.id ELSE '/people/'||u.username END FROM subscriptions s LEFT JOIN works w ON s.kind='story' AND w.id=s.target_id AND w.published=1 LEFT JOIN users u ON s.kind='author' AND u.id=s.target_id AND u.suspended=0 WHERE s.user_id=? AND (w.id IS NOT NULL OR u.id IS NOT NULL) ORDER BY s.kind,3`, uid)
	if err != nil {
		http.Error(w, "Could not load subscriptions", 500)
		return
	}
	var list []Subscription
	for rows.Next() {
		var v Subscription
		if rows.Scan(&v.Kind, &v.TargetID, &v.Label, &v.URL) == nil {
			list = append(list, v)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		http.Error(w, "Could not load subscriptions", 500)
		return
	}
	render(w, r, "subscriptions", Page{Title: "Subscriptions", Subscriptions: list})
}
func notificationsPage(w http.ResponseWriter, r *http.Request) {
	uid, _ := current(r)
	if uid == 0 {
		http.Redirect(w, r, "/login", 303)
		return
	}
	if r.Method == "POST" {
		if _, err := db.Exec("UPDATE notifications SET read=1 WHERE user_id=? AND read=0", uid); err != nil {
			http.Error(w, "Could not update notifications", 500)
			return
		}
		http.Redirect(w, r, "/notifications", 303)
		return
	}
	rows, err := db.Query("SELECT id,work_id,message,created,read FROM notifications WHERE user_id=? ORDER BY id DESC LIMIT 100", uid)
	if err != nil {
		http.Error(w, "Could not load notifications", 500)
		return
	}
	var list []Notification
	for rows.Next() {
		var n Notification
		var story sql.NullInt64
		var read int
		if rows.Scan(&n.ID, &story, &n.Message, &n.Created, &read) == nil {
			n.StoryID = story.Int64
			n.Read = read != 0
			list = append(list, n)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		http.Error(w, "Could not load notifications", 500)
		return
	}
	render(w, r, "notifications", Page{Title: "Notifications", Notifications: list})
}
func notifySubscribers(tx *sql.Tx, uid, storyID int, message string) error {
	_, err := tx.Exec(`INSERT INTO notifications(user_id,work_id,message,created) SELECT DISTINCT user_id,?,?,? FROM subscriptions WHERE user_id<>? AND ((kind='story' AND target_id=?) OR (kind='author' AND target_id=?))`, storyID, message, time.Now().Format("2006-01-02"), uid, storyID, uid)
	return err
}
func publishOwnedStory(uid int, author string, work Work, draftID, draftRevision int) (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if draftID > 0 {
		result, err := tx.Exec("DELETE FROM drafts WHERE id=? AND user_id=? AND revision=?", draftID, uid, draftRevision)
		if err != nil {
			return 0, err
		}
		n, err := result.RowsAffected()
		if err != nil || n != 1 {
			return 0, fmt.Errorf("draft changed or no longer exists")
		}
	}
	res, err := tx.Exec(`INSERT INTO works(user_id,title,author,fandom,rating,warnings,tags,summary,content,status,language,words,updated) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, uid, work.Title, author, work.Fandom, work.Rating, work.Warnings, work.Tags, work.Summary, work.Content, work.Status, "English", len(strings.Fields(work.Content)), time.Now().Format("2006-01-02"))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err = notifySubscribers(tx, uid, int(id), fmt.Sprintf("%s published a new story.", author)); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}
