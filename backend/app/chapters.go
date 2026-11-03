package everlore

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func addChapter(w http.ResponseWriter, r *http.Request) {
	uid, _ := current(r)
	if uid == 0 {
		http.Redirect(w, r, "/login", 303)
		return
	}
	id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/chapters/add/"))
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	works := queryWorks(" AND id=? AND user_id=?", id, uid)
	if len(works) == 0 {
		http.NotFound(w, r)
		return
	}
	p := Page{Title: "Add a chapter", Work: works[0]}
	if r.Method == "POST" {
		title, content := strings.TrimSpace(r.FormValue("title")), strings.TrimSpace(r.FormValue("content"))
		p.ChapterTitle = title
		p.ChapterContent = content
		if title == "" || len(title) > 300 || content == "" {
			p.Error = "Enter a title and chapter text."
		} else {
			tx, e := db.Begin()
			if e != nil {
				http.Error(w, "Could not save chapter", 500)
				return
			}
			defer tx.Rollback()
			var number int
			e = tx.QueryRow("SELECT COALESCE(MAX(number),1)+1 FROM chapters WHERE work_id=?", id).Scan(&number)
			if e == nil {
				_, e = tx.Exec("INSERT INTO chapters(work_id,number,title,content) VALUES(?,?,?,?)", id, number, title, content)
			}
			if e == nil {
				_, e = tx.Exec("UPDATE works SET words=words+?,updated=? WHERE id=? AND user_id=?", len(strings.Fields(content)), time.Now().Format("2006-01-02"), id, uid)
			}
			if e == nil {
				e = notifySubscribers(tx, uid, id, "A story you follow has a new chapter.")
			}
			if e == nil {
				e = tx.Commit()
			}
			if e == nil {
				http.Redirect(w, r, fmt.Sprintf("/work/%d?chapter=%d", id, number), 303)
				return
			}
			p.Error = "Could not save the chapter. Please try again."
		}
	}
	render(w, r, "addchapter", p)
}

// Recalculate all chapter words when the first chapter changes, within one transaction.
func updateWorkWithChapterWords(uid, id int, title, fandom, summary, tags, content, warnings string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query("SELECT content FROM chapters WHERE work_id=?", id)
	if err != nil {
		return err
	}
	total := len(strings.Fields(content))
	for rows.Next() {
		var chapter string
		if err = rows.Scan(&chapter); err != nil {
			rows.Close()
			return err
		}
		total += len(strings.Fields(chapter))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE works SET title=?,fandom=?,summary=?,tags=?,content=?,warnings=?,words=?,updated=? WHERE id=? AND user_id=?`, title, fandom, summary, tags, content, warnings, total, time.Now().Format("2006-01-02"), id, uid)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("story not found")
	}
	return tx.Commit()
}

// manageChapters lets only the owner edit, reorder, or remove subsequent chapters.
// The first chapter remains part of the story editor.
func manageChapters(w http.ResponseWriter, r *http.Request) {
	uid, _ := current(r)
	if uid == 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/chapters/manage/"))
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	works := queryWorks(" AND id=? AND user_id=?", id, uid)
	if len(works) == 0 {
		http.NotFound(w, r)
		return
	}
	p := Page{Title: "Manage chapters", Work: works[0]}
	if r.Method == http.MethodPost {
		number, e := strconv.Atoi(r.FormValue("number"))
		action := r.FormValue("action")
		if e != nil || number < 2 {
			http.Error(w, "Invalid chapter", 400)
			return
		}
		title, content := strings.TrimSpace(r.FormValue("title")), strings.TrimSpace(r.FormValue("content"))
		p.ChapterTitle, p.ChapterContent = title, content
		if action == "save" && (title == "" || len(title) > 300 || content == "") {
			p.Error = "Enter a chapter title and text."
		} else {
			e = changeChapter(uid, id, number, action, title, content)
			if e == nil {
				http.Redirect(w, r, fmt.Sprintf("/chapters/manage/%d", id), http.StatusSeeOther)
				return
			}
			p.Error = "Could not update the chapter. Please reload and try again."
		}
	}
	rows, e := db.Query("SELECT number,title,content FROM chapters WHERE work_id=? ORDER BY number", id)
	if e != nil {
		http.Error(w, "Could not load chapters", 500)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var c Chapter
		if e = rows.Scan(&c.Number, &c.Title, &c.Content); e != nil {
			http.Error(w, "Could not load chapters", 500)
			return
		}
		if p.Error != "" && r.FormValue("action") == "save" && r.FormValue("number") == strconv.Itoa(c.Number) {
			c.Title, c.Content = p.ChapterTitle, p.ChapterContent
		}
		p.ChapterList = append(p.ChapterList, c)
	}
	if rows.Err() != nil {
		http.Error(w, "Could not load chapters", 500)
		return
	}
	render(w, r, "managechapters", p)
}
func changeChapter(uid, id, number int, action, title, content string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var oldContent string
	err = tx.QueryRow(`SELECT c.content FROM chapters c JOIN works w ON w.id=c.work_id WHERE c.work_id=? AND c.number=? AND w.user_id=?`, id, number, uid).Scan(&oldContent)
	if err != nil {
		return err
	}
	delta := 0
	switch action {
	case "save":
		delta = len(strings.Fields(content)) - len(strings.Fields(oldContent))
		_, err = tx.Exec("UPDATE chapters SET title=?,content=? WHERE work_id=? AND number=?", title, content, id, number)
	case "delete":
		delta = -len(strings.Fields(oldContent))
		_, err = tx.Exec("DELETE FROM chapters WHERE work_id=? AND number=?", id, number)
		if err != nil {
			return err
		}
		rows, e := tx.Query("SELECT number FROM chapters WHERE work_id=? AND number>? ORDER BY number", id, number)
		if e != nil {
			return e
		}
		var nums []int
		for rows.Next() {
			var n int
			if e = rows.Scan(&n); e != nil {
				rows.Close()
				return e
			}
			nums = append(nums, n)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, n := range nums {
			if _, err = tx.Exec("UPDATE chapters SET number=? WHERE work_id=? AND number=?", n-1, id, n); err != nil {
				return err
			}
		}
	case "up", "down":
		other := number - 1
		if action == "down" {
			other = number + 1
		}
		if other < 2 {
			return fmt.Errorf("first chapter cannot be moved")
		}
		var exists int
		if err = tx.QueryRow("SELECT 1 FROM chapters WHERE work_id=? AND number=?", id, other).Scan(&exists); err != nil {
			return err
		}
		var temporary int
		if err = tx.QueryRow("SELECT COALESCE(MAX(number),1)+1 FROM chapters WHERE work_id=?", id).Scan(&temporary); err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE chapters SET number=? WHERE work_id=? AND number=?", temporary, id, number); err == nil {
			_, err = tx.Exec("UPDATE chapters SET number=? WHERE work_id=? AND number=?", number, id, other)
		}
		if err == nil {
			_, err = tx.Exec("UPDATE chapters SET number=? WHERE work_id=? AND number=?", other, id, temporary)
		}
	default:
		return fmt.Errorf("invalid action")
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE works SET words=words+?,updated=? WHERE id=? AND user_id=?", delta, time.Now().Format("2006-01-02"), id, uid); err != nil {
		return err
	}
	return tx.Commit()
}
