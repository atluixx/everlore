package everlore

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Draft struct {
	ID, Revision   int
	Title, Updated string
}

func migrateDrafts() error {
	return execBatch(`CREATE TABLE IF NOT EXISTS drafts(id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id), data TEXT NOT NULL, title TEXT NOT NULL, revision INTEGER NOT NULL DEFAULT 1, updated TEXT NOT NULL); CREATE INDEX IF NOT EXISTS drafts_user_idx ON drafts(user_id,updated DESC);`)
}
func listDrafts(uid int) []Draft {
	rows, err := db.Query("SELECT id,revision,title,updated FROM drafts WHERE user_id=? ORDER BY updated DESC,id DESC", uid)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var drafts []Draft
	for rows.Next() {
		var d Draft
		if rows.Scan(&d.ID, &d.Revision, &d.Title, &d.Updated) == nil {
			drafts = append(drafts, d)
		}
	}
	return drafts
}
func loadDraft(w http.ResponseWriter, r *http.Request, uid int, p *Page) bool {
	id, err := strconv.Atoi(r.URL.Query().Get("draft"))
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return false
	}
	var raw string
	if db.QueryRow("SELECT data,revision FROM drafts WHERE id=? AND user_id=?", id, uid).Scan(&raw, &p.DraftRevision) != nil {
		http.NotFound(w, r)
		return false
	}
	var values url.Values
	if json.Unmarshal([]byte(raw), &values) != nil {
		http.Error(w, "Could not load draft", 500)
		return false
	}
	p.DraftID = id
	p.Work = Work{Title: values.Get("title"), Content: values.Get("content"), Fandom: values.Get("fandom"), Rating: values.Get("rating"), Warnings: strings.Join(values["warnings"], ", "), Tags: values.Get("tags"), Summary: values.Get("summary"), Status: values.Get("status"), Author: values.Get("pseudonym")}
	return true
}
func drafts(w http.ResponseWriter, r *http.Request) {
	uid, _ := current(r)
	if uid == 0 {
		http.Error(w, "Log in to manage drafts", http.StatusUnauthorized)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.Atoi(r.FormValue("draft_id"))
	revision, _ := strconv.Atoi(r.FormValue("draft_revision"))
	if r.URL.Path == "/drafts/delete" {
		result, err := db.Exec("DELETE FROM drafts WHERE id=? AND user_id=? AND revision=?", id, uid, revision)
		if err != nil {
			http.Error(w, "Could not delete draft", 500)
			return
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			http.Error(w, "Draft changed or no longer exists; reload before deleting", 409)
			return
		}
		http.Redirect(w, r, "/dashboard", 303)
		return
	}
	if r.URL.Path != "/drafts/save" {
		http.NotFound(w, r)
		return
	}
	values := url.Values{}
	for _, key := range []string{"title", "content", "fandom", "rating", "warnings", "tags", "summary", "status", "pseudonym"} {
		values[key] = r.Form[key]
	}
	data, err := json.Marshal(values)
	if err != nil || len(data) > 2*1024*1024 {
		http.Error(w, "Draft is too large", 400)
		return
	}
	title := strings.TrimSpace(values.Get("title"))
	if len(title) > 300 {
		http.Error(w, "Title is too long", 400)
		return
	}
	if title == "" {
		title = "Untitled draft"
	}
	updated := time.Now().UTC().Format(time.RFC3339)
	if id == 0 {
		result, err := db.Exec("INSERT INTO drafts(user_id,data,title,updated) VALUES(?,?,?,?)", uid, string(data), title, updated)
		if err != nil {
			http.Error(w, "Could not save draft", 500)
			return
		}
		newID, err := result.LastInsertId()
		if err != nil {
			http.Error(w, "Could not save draft", 500)
			return
		}
		id = int(newID)
	} else {
		result, err := db.Exec("UPDATE drafts SET data=?,title=?,updated=?,revision=revision+1 WHERE id=? AND user_id=? AND revision=?", string(data), title, updated, id, uid, revision)
		if err != nil {
			http.Error(w, "Could not save draft", 500)
			return
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			http.Error(w, "Draft changed or no longer exists. Copy your text, then reload the draft.", 409)
			return
		}
	}
	http.Redirect(w, r, "/publish?draft="+strconv.Itoa(id), 303)
}
