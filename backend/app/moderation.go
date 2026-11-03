package everlore

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
)

type ModerationReport struct {
	ID             int
	StoryID        int64
	Reason, Status string
}

func isAdmin(uid int) bool {
	if uid == 0 {
		return false
	}
	var role string
	return db.QueryRow("SELECT role FROM users WHERE id=? AND suspended=0", uid).Scan(&role) == nil && role == "admin"
}
func reportPage(w http.ResponseWriter, r *http.Request) {
	uid, _ := current(r)
	if uid == 0 {
		http.Redirect(w, r, "/login", 303)
		return
	}
	id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/report/"))
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	works := queryWorks(" AND id=?", id)
	if len(works) == 0 {
		http.NotFound(w, r)
		return
	}
	render(w, r, "reportstory", Page{Title: "Report story", Work: works[0]})
}
func moderationPage(w http.ResponseWriter, r *http.Request) {
	uid, _ := current(r)
	if uid == 0 {
		http.Redirect(w, r, "/login", 303)
		return
	}
	if !isAdmin(uid) {
		http.Error(w, "Administrator required", 403)
		return
	}
	rows, err := db.Query("SELECT id,work_id,reason,status FROM reports ORDER BY id DESC LIMIT 100")
	if err != nil {
		http.Error(w, "Could not load reports", 500)
		return
	}
	var reports []ModerationReport
	for rows.Next() {
		var report ModerationReport
		var story sql.NullInt64
		if err = rows.Scan(&report.ID, &story, &report.Reason, &report.Status); err != nil {
			rows.Close()
			http.Error(w, "Could not load reports", 500)
			return
		}
		report.StoryID = story.Int64
		reports = append(reports, report)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		http.Error(w, "Could not load reports", 500)
		return
	}
	render(w, r, "moderation", Page{Title: "Moderation", Reports: reports})
}
