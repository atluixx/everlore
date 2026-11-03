package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

var db *sql.DB
var templates = template.Must(template.New("").Funcs(template.FuncMap{"friendlyDate": friendlyDate}).ParseGlob("templates/*.html"))

type Work struct {
	ID                                                                                         int
	Title, Author, Fandom, Rating, Warnings, Tags, Summary, Updated, Status, Language, Content string
	Words, Kudos, Chapters                                                                     int
}
type Comment struct{ Author, Body, Created string }
type Pseudonym struct {
	ID   int
	Name string
}
type Page struct {
	Title, User, Query, Error, Message, Sort string
	Works                                    []Work
	Bookmarks                                []Work
	Comments                                 []Comment
	Work                                     Work
	Tags                                     []string
	Count                                    int
	CSRF                                     string
	ProfileName, Bio, DisplayName            string
	Pseudonyms                               []Pseudonym
	Bookmarked                               bool
	Editing                                  bool
	Filters                                  url.Values
}

// friendlyDate formats stored ISO dates without altering their sortable database representation.
func friendlyDate(value string) string {
	date, err := time.Parse("2006-01-02", value)
	if err != nil {
		return value
	}
	day := date.Day()
	suffix := "th"
	if day%100 < 11 || day%100 > 13 {
		switch day % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%s %d%s, %d", date.Month(), day, suffix, date.Year())
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
func main() {
	var err error
	db, err = sql.Open("sqlite", "everlore.db")
	must(err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`)
	must(err)
	_, err = db.Exec(`PRAGMA foreign_keys=ON; CREATE TABLE IF NOT EXISTS users(id INTEGER PRIMARY KEY, username TEXT UNIQUE NOT NULL, password TEXT NOT NULL); CREATE TABLE IF NOT EXISTS sessions(token TEXT PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id), expires INTEGER NOT NULL); CREATE TABLE IF NOT EXISTS works(id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id), title TEXT NOT NULL, author TEXT NOT NULL, fandom TEXT NOT NULL, rating TEXT NOT NULL, warnings TEXT NOT NULL, tags TEXT NOT NULL, summary TEXT NOT NULL, content TEXT NOT NULL, language TEXT NOT NULL DEFAULT 'English', status TEXT NOT NULL DEFAULT 'Complete', words INTEGER NOT NULL, updated TEXT NOT NULL, published INTEGER NOT NULL DEFAULT 1); CREATE TABLE IF NOT EXISTS bookmarks(user_id INTEGER REFERENCES users(id), work_id INTEGER REFERENCES works(id), PRIMARY KEY(user_id,work_id)); CREATE TABLE IF NOT EXISTS kudos(user_id INTEGER REFERENCES users(id), work_id INTEGER REFERENCES works(id), PRIMARY KEY(user_id,work_id)); CREATE TABLE IF NOT EXISTS comments(id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id), work_id INTEGER REFERENCES works(id), body TEXT NOT NULL, created TEXT NOT NULL);`)
	must(err)
	migrate()
	apiSchema()
	if os.Getenv("EVERLORE_SEED") == "1" {
		seed()
	}
	if admin := os.Getenv("EVERLORE_BOOTSTRAP_ADMIN"); admin != "" {
		result, err := db.Exec("UPDATE users SET role='admin' WHERE username=? AND email_verified=1", admin)
		must(err)
		n, _ := result.RowsAffected()
		if n != 1 {
			log.Fatal("bootstrap admin must be an existing verified account")
		}
	}
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.HandleFunc("/", home)
	http.HandleFunc("/browse", browse)
	http.HandleFunc("/work/", work)
	http.HandleFunc("/login", login)
	http.HandleFunc("/recovery", func(w http.ResponseWriter, r *http.Request) {
		render(w, r, "recovery", Page{Title: "Password recovery"})
	})
	http.HandleFunc("/register", register)
	http.HandleFunc("/logout", logout)
	http.HandleFunc("/publish", publish)
	http.HandleFunc("/dashboard", dashboard)
	http.HandleFunc("/about", about)
	http.HandleFunc("/profile", profile)
	http.HandleFunc("/people/", publicProfile)
	http.HandleFunc("/edit/", editWork)
	http.HandleFunc("/api/csrf", apiCSRF)
	http.HandleFunc("/api/account/register", accountRegister)
	http.HandleFunc("/api/account/verify", accountVerify)
	http.HandleFunc("/api/account/verification", func(w http.ResponseWriter, r *http.Request) { accountRequestToken(w, r, "verify") })
	http.HandleFunc("/api/account/recovery", func(w http.ResponseWriter, r *http.Request) { accountRequestToken(w, r, "reset") })
	http.HandleFunc("/api/account/reset", accountReset)
	http.HandleFunc("/api/account/delete", accountDelete)
	http.HandleFunc("/api/pseudonyms", apiPseudonyms)
	http.HandleFunc("/api/reports", reportWork)
	http.HandleFunc("/api/works/delete", deleteWork)
	http.HandleFunc("/api/comments/delete", deleteComment)
	http.HandleFunc("/api/admin/audit", adminAudit)
	http.HandleFunc("/api/admin/reports", adminReports)
	http.HandleFunc("/api/admin/actions", adminAction)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Everlore listening on http://localhost:%s", port)
	server := &http.Server{Addr: ":" + port, Handler: security(http.DefaultServeMux), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	must(server.ListenAndServe())
}
func seed() {
	var n int
	db.QueryRow("SELECT count(*) FROM works").Scan(&n)
	if n > 0 {
		return
	}
	samples := []Work{
		{Title: "The Cartography of Small Things", Author: "mothandmaple", Fandom: "Original Work", Rating: "General Audiences", Warnings: "No Archive Warnings Apply", Tags: "Found Family, Quiet Magic, Letters", Summary: "A mapmaker returns to the town she once disappeared from, and finds that someone has been carefully keeping track of all the places she loved.", Content: "On the morning the map arrived, the town was still folded into the fog.\n\nMara stood at the window with a cup of tea cooling in her hands. Across the street, the old bookshop had opened its door, just as it had every morning she could remember. Some things, she thought, knew how to wait.\n\nInside the envelope was a map of a place she had spent years trying to forget. In the corner, in a hand she recognized at once, someone had written: You can always find your way back.", Status: "Complete", Words: 4820},
		{Title: "Where the Light Gets In", Author: "inkandembers", Fandom: "The Locked Tomb Series", Rating: "Teen And Up Audiences", Warnings: "No Archive Warnings Apply", Tags: "Character Study, Grief and Healing, Slow Burn", Summary: "There are a hundred ways to haunt a house. There are almost as many ways to come home.", Content: "The house remembered them, even when they could not remember themselves.\n\nIn the afternoon light, dust turned slowly in the air, and every room held its breath.", Status: "In Progress", Words: 12640},
		{Title: "A Constellation of Ordinary Days", Author: "papercranes", Fandom: "Good Omens", Rating: "Teen And Up Audiences", Warnings: "Creator Chose Not To Use Archive Warnings", Tags: "Aziraphale/Crowley, Bookshops, Gentle Romance", Summary: "An angel, a demon, and the rather extraordinary business of learning how to spend an ordinary afternoon.", Content: "The bookshop was quiet in the particular way it only ever was after rain. Crowley considered this a personal affront to the laws of London traffic.", Status: "Complete", Words: 8350},
		{Title: "Saltwater, After Everything", Author: "northbound", Fandom: "Our Flag Means Death", Rating: "Mature", Warnings: "Major Character Death", Tags: "Edward Teach/Stede Bonnet, Second Chances, The Sea", Summary: "The sea gives back what it can. The rest, they will have to make for themselves.", Content: "The sea was kind that morning, which made everything a little more difficult to explain.", Status: "In Progress", Words: 21940},
		{Title: "The Language of Wildflowers", Author: "larkspurwrites", Fandom: "Original Work", Rating: "General Audiences", Warnings: "No Archive Warnings Apply", Tags: "Poetry, Nature, Hopeful Ending", Summary: "A small collection of poems about growing around the things we cannot change.", Content: "Somewhere the wildflowers\nare learning the names\nof every road home.", Status: "Complete", Words: 2100},
		{Title: "A Study in Second Chances", Author: "astrolabe", Fandom: "Sherlock Holmes & Related Fandoms", Rating: "Teen And Up Audiences", Warnings: "No Archive Warnings Apply", Tags: "Friendship, Mystery, London", Summary: "A curious letter arrives at Baker Street, addressed to a man who has been dead for seventeen years.", Content: "The letter arrived on a Tuesday, tucked between the morning paper and a bill for coal.", Status: "Complete", Words: 6700},
	}
	for _, w := range samples {
		db.Exec(`INSERT INTO works(title,author,fandom,rating,warnings,tags,summary,content,status,words,updated) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, w.Title, w.Author, w.Fandom, w.Rating, w.Warnings, w.Tags, w.Summary, w.Content, w.Status, w.Words, time.Now().Add(-time.Duration(len(w.Title))*time.Hour).Format("2006-01-02"))
	}
}
func current(r *http.Request) (int, string) {
	c, e := r.Cookie("session")
	if e != nil {
		return 0, ""
	}
	var id int
	var name string
	db.QueryRow(`SELECT users.id,username FROM sessions JOIN users ON users.id=sessions.user_id WHERE token=? AND expires>? AND users.suspended=0`, sessionHash(c.Value), time.Now().Unix()).Scan(&id, &name)
	return id, name
}
func render(w http.ResponseWriter, r *http.Request, name string, p Page) {
	_, p.User = current(r)
	p.CSRF = csrfToken(w, r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, name, p); err != nil {
		log.Print(err)
	}
}
func queryWorks(where string, args ...any) []Work {
	rows, e := db.Query(`SELECT id,title,author,fandom,rating,warnings,tags,summary,updated,status,language,words,content,(SELECT count(*) FROM kudos WHERE work_id=works.id) FROM works WHERE published=1 `+where, args...)
	if e != nil {
		log.Print(e)
		return nil
	}
	defer rows.Close()
	var out []Work
	for rows.Next() {
		var x Work
		rows.Scan(&x.ID, &x.Title, &x.Author, &x.Fandom, &x.Rating, &x.Warnings, &x.Tags, &x.Summary, &x.Updated, &x.Status, &x.Language, &x.Words, &x.Content, &x.Kudos)
		x.Chapters = 1
		out = append(out, x)
	}
	return out
}
func home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	works := queryWorks("ORDER BY updated DESC, id DESC LIMIT 4")
	render(w, r, "home", Page{Title: "Home", Works: works})
}
func browse(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	where := ""
	args := []any{}
	add := func(v, clause string) {
		if v != "" {
			where += " AND " + clause
			args = append(args, "%"+v+"%")
		}
	}
	add(q.Get("q"), "(title LIKE ? OR author LIKE ? OR fandom LIKE ? OR tags LIKE ? OR summary LIKE ?)")
	if q.Get("q") != "" {
		v := "%" + q.Get("q") + "%"
		args = append(args, v, v, v, v)
	}
	add(q.Get("fandom"), "fandom LIKE ?")
	add(q.Get("include"), "tags LIKE ?")
	if q.Get("exclude") != "" {
		where += " AND tags NOT LIKE ?"
		args = append(args, "%"+q.Get("exclude")+"%")
	}
	if q.Get("rating") != "" {
		where += " AND rating=?"
		args = append(args, q.Get("rating"))
	}
	if q.Get("status") != "" {
		where += " AND status=?"
		args = append(args, q.Get("status"))
	}
	sort := q.Get("sort")
	order := "updated DESC, id DESC"
	switch sort {
	case "words":
		order = "words DESC"
	case "title":
		order = "title ASC"
	case "kudos":
		order = "(SELECT count(*) FROM kudos WHERE work_id=works.id) DESC"
	}
	works := queryWorks(where+" ORDER BY "+order, args...)
	render(w, r, "browse", Page{Title: "Browse works", Works: works, Count: len(works), Query: q.Get("q"), Sort: sort, Filters: q})
}
func work(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/work/"))
	if e != nil {
		http.NotFound(w, r)
		return
	}
	works := queryWorks(" AND id=?", id)
	if len(works) == 0 {
		http.NotFound(w, r)
		return
	}
	uid, _ := current(r)
	if r.Method == "POST" {
		if uid == 0 {
			http.Redirect(w, r, "/login", 303)
			return
		}
		switch r.FormValue("action") {
		case "bookmark":
			db.Exec("INSERT OR IGNORE INTO bookmarks VALUES(?,?)", uid, id)
		case "unbookmark":
			db.Exec("DELETE FROM bookmarks WHERE user_id=? AND work_id=?", uid, id)
		case "kudos":
			db.Exec("INSERT OR IGNORE INTO kudos VALUES(?,?)", uid, id)
		case "comment":
			body := strings.TrimSpace(r.FormValue("comment"))
			if body != "" && len(body) <= 5000 {
				db.Exec("INSERT INTO comments(user_id,work_id,body,created) VALUES(?,?,?,?)", uid, id, body, time.Now().Format("2006-01-02"))
			}
		}
		http.Redirect(w, r, fmt.Sprintf("/work/%d", id), 303)
		return
	}
	rows, err := db.Query(`SELECT username, body, created FROM comments JOIN users ON users.id=comments.user_id WHERE work_id=? ORDER BY comments.id`, id)
	var comments []Comment
	if err == nil {
		for rows.Next() {
			var c Comment
			rows.Scan(&c.Author, &c.Body, &c.Created)
			comments = append(comments, c)
		}
		rows.Close()
	}
	var bookmarked int
	if uid != 0 {
		db.QueryRow("SELECT count(*) FROM bookmarks WHERE user_id=? AND work_id=?", uid, id).Scan(&bookmarked)
	}
	render(w, r, "work", Page{Title: works[0].Title, Work: works[0], Comments: comments, Bookmarked: bookmarked > 0})
}
func auth(w http.ResponseWriter, r *http.Request, registering bool) {
	title := "Log in"
	if registering {
		title = "Create account"
	}
	p := Page{Title: title}
	if r.Method == "POST" {
		if !allowAuth(r) {
			http.Error(w, "Too many attempts. Please try again later.", http.StatusTooManyRequests)
			return
		}
		name := strings.TrimSpace(r.FormValue("username"))
		pass := r.FormValue("password")
		if !validUsername.MatchString(name) || len(pass) < 8 || len(pass) > 72 {
			p.Error = "Use 3–30 letters, numbers, underscores or hyphens for your username and 8–72 bytes for your password."
		} else if registering {
			p.Error = "Registration requires email verification. Use the account API."
		} else {
			var id int
			var hash string
			e := db.QueryRow("SELECT id,password FROM users WHERE username=? COLLATE NOCASE AND suspended=0 AND (email IS NULL OR email_verified=1)", name).Scan(&id, &hash)
			if e != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(pass)) != nil {
				p.Error = "Incorrect username or password."
			} else {
				b := make([]byte, 32)
				rand.Read(b)
				token := hex.EncodeToString(b)
				if _, err := db.Exec("INSERT INTO sessions VALUES(?,?,?)", sessionHash(token), id, time.Now().Add(30*24*time.Hour).Unix()); err != nil {
					http.Error(w, "Could not start session", 500)
					return
				}
				http.SetCookie(w, &http.Cookie{Name: "session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 2592000, Secure: secureCookie(r)})
				http.Redirect(w, r, "/dashboard", 303)
				return
			}
		}
	}
	if r.URL.Query().Get("created") != "" {
		p.Message = "Account created. You can now log in."
	}
	render(w, r, "auth", p)
}
func login(w http.ResponseWriter, r *http.Request)    { auth(w, r, false) }
func register(w http.ResponseWriter, r *http.Request) { auth(w, r, true) }
func logout(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		if c, e := r.Cookie("session"); e == nil {
			db.Exec("DELETE FROM sessions WHERE token=?", sessionHash(c.Value))
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Path: "/", MaxAge: -1})
	}
	http.Redirect(w, r, "/", 303)
}
func publish(w http.ResponseWriter, r *http.Request) {
	uid, name := current(r)
	if uid == 0 {
		http.Redirect(w, r, "/login", 303)
		return
	}
	p := Page{Title: "Post a work", Pseudonyms: pseudonyms(uid)}
	if r.Method == "POST" {
		title := strings.TrimSpace(r.FormValue("title"))
		content := strings.TrimSpace(r.FormValue("content"))
		fandom := strings.TrimSpace(r.FormValue("fandom"))
		rating := r.FormValue("rating")
		pseud := r.FormValue("pseudonym")
		if pseud != "" {
			var exists int
			db.QueryRow("SELECT count(*) FROM pseudonyms WHERE user_id=? AND name=?", uid, pseud).Scan(&exists)
			if exists != 1 {
				http.Error(w, "Invalid pseudonym", 400)
				return
			}
			name = pseud
		}
		warnings := r.FormValue("warnings")
		p.Work = Work{Title: title, Content: content, Fandom: fandom, Rating: rating, Warnings: warnings, Tags: r.FormValue("tags"), Summary: r.FormValue("summary"), Status: r.FormValue("status"), Author: pseud}
		if title == "" || len(title) > 300 || content == "" || fandom == "" || len(fandom) > 300 || !allowedRating[rating] || !allowedWarning[warnings] || (r.FormValue("status") != "Complete" && r.FormValue("status") != "In Progress") {
			p.Error = "Please complete the title, fandom, rating, warning, and work text."
		} else {
			res, e := db.Exec(`INSERT INTO works(user_id,title,author,fandom,rating,warnings,tags,summary,content,status,language,words,updated) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, uid, title, name, fandom, rating, warnings, r.FormValue("tags"), r.FormValue("summary"), content, r.FormValue("status"), "English", len(strings.Fields(content)), time.Now().Format("2006-01-02"))
			if e == nil {
				id, _ := res.LastInsertId()
				http.Redirect(w, r, fmt.Sprintf("/work/%d", id), 303)
				return
			}
			p.Error = "Could not save your work. Please try again."
		}
	}
	render(w, r, "publish", p)
}
func dashboard(w http.ResponseWriter, r *http.Request) {
	uid, _ := current(r)
	if uid == 0 {
		http.Redirect(w, r, "/login", 303)
		return
	}
	works := queryWorks(" AND user_id=? ORDER BY id DESC", uid)
	bookmarks := queryWorks(" AND id IN (SELECT work_id FROM bookmarks WHERE user_id=?) ORDER BY updated DESC", uid)
	render(w, r, "dashboard", Page{Title: "Your dashboard", Works: works, Bookmarks: bookmarks})
}
func about(w http.ResponseWriter, r *http.Request) {
	render(w, r, "about", Page{Title: "About Everlore"})
}

var allowedRating = map[string]bool{"General Audiences": true, "Teen And Up Audiences": true, "Mature": true, "Explicit": true, "Not Rated": true}
var allowedWarning = map[string]bool{"No Archive Warnings Apply": true, "Creator Chose Not To Use Archive Warnings": true, "Graphic Depictions of Violence": true, "Major Character Death": true, "Rape/Non-Con": true, "Underage": true}
