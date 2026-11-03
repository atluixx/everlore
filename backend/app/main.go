package everlore

import (
	"context"
	"crypto/rand"
	"database/sql"

	"encoding/hex"
	"everlore/frontend"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

var db *sql.DB

var templates = template.Must(template.New("").Funcs(template.FuncMap{"friendlyDate": friendlyDate, "hasWarning": hasWarning, "hasCurrentWarning": hasCurrentWarning, "displayFandom": displayFandom}).ParseFS(frontend.Assets, "templates/*.html"))

type Work struct {
	ID, OwnerID                                                                                int
	Title, Author, Fandom, Rating, Warnings, Tags, Summary, Updated, Status, Language, Content string
	Words, Kudos, Chapters                                                                     int
}
type Comment struct {
	ID                    int
	Author, Body, Created string
	CanDelete             bool
}
type Chapter struct {
	Number         int
	Title, Content string
}
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
	Count                                    int
	CSRF                                     string
	ProfileName, Bio, DisplayName            string
	Pseudonyms                               []Pseudonym
	Bookmarked                               bool
	Filters                                  url.Values
	PageNumber                               int
	PreviousURL, NextURL                     string
	GuestName, GuestEmail                    string
	Claimable                                []Work
	CurrentChapter, PrevChapter, NextChapter int
	ChapterTitle, ChapterContent             string
	IsAdmin                                  bool
	Reports                                  []ModerationReport
	Subscriptions                            []Subscription
	Notifications                            []Notification
	ProfileID                                int
	Subscribed                               bool
	CanSubscribe                             bool
	ChapterList                              []Chapter
	Drafts                                   []Draft
	DraftID, DraftRevision                   int
}

// displayFandom keeps the original stored fandom compatible while presenting story terminology.
func displayFandom(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "Original Work") || strings.EqualFold(strings.TrimSpace(value), "Original Stories") {
		return "Original Stories"
	}
	return value
}
func storedFandom(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "Original Stories") {
		return "Original Work"
	}
	return value
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
func execBatch(sqlText string) error {
	for _, statement := range strings.Split(sqlText, ";") {
		if strings.TrimSpace(statement) != "" {
			if _, err := db.Exec(statement); err != nil {
				return err
			}
		}
	}
	return nil
}
func NewHandler() http.Handler {
	var err error
	if remote := os.Getenv("TURSO_DATABASE_URL"); remote != "" {
		parsed, e := url.Parse(remote)
		must(e)
		params := parsed.Query()
		params.Set("authToken", os.Getenv("TURSO_AUTH_TOKEN"))
		parsed.RawQuery = params.Encode()
		db, err = sql.Open("libsql", parsed.String())
	} else {
		if os.Getenv("VERCEL") != "" {
			log.Fatal("TURSO_DATABASE_URL is required on Vercel")
		}
		db, err = sql.Open("sqlite", "everlore.db")
	}
	must(err)
	db.SetMaxOpenConns(1)
	if os.Getenv("TURSO_DATABASE_URL") == "" {
		must(execBatch(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;`))
	}
	// A persistent schema marker prevents every serverless cold start from
	// repeating the full sequence of schema and data migrations.
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (id INTEGER PRIMARY KEY CHECK(id=1), version INTEGER NOT NULL)`)
	must(err)
	var schemaVersion int
	err = db.QueryRow(`SELECT version FROM schema_migrations WHERE id=1`).Scan(&schemaVersion)
	if err != nil && err != sql.ErrNoRows {
		must(err)
	}
	if schemaVersion < 1 {
		err = execBatch(`CREATE TABLE IF NOT EXISTS users(id INTEGER PRIMARY KEY, username TEXT UNIQUE NOT NULL, password TEXT NOT NULL); CREATE TABLE IF NOT EXISTS sessions(token TEXT PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id), expires INTEGER NOT NULL); CREATE TABLE IF NOT EXISTS works(id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id), title TEXT NOT NULL, author TEXT NOT NULL, fandom TEXT NOT NULL, rating TEXT NOT NULL, warnings TEXT NOT NULL, tags TEXT NOT NULL, summary TEXT NOT NULL, content TEXT NOT NULL, language TEXT NOT NULL DEFAULT 'English', status TEXT NOT NULL DEFAULT 'Complete', words INTEGER NOT NULL, updated TEXT NOT NULL, published INTEGER NOT NULL DEFAULT 1); CREATE TABLE IF NOT EXISTS bookmarks(user_id INTEGER REFERENCES users(id), work_id INTEGER REFERENCES works(id), PRIMARY KEY(user_id,work_id)); CREATE TABLE IF NOT EXISTS kudos(user_id INTEGER REFERENCES users(id), work_id INTEGER REFERENCES works(id), PRIMARY KEY(user_id,work_id)); CREATE TABLE IF NOT EXISTS comments(id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id), work_id INTEGER REFERENCES works(id), body TEXT NOT NULL, created TEXT NOT NULL);`)
		must(err)
		migrate()
		migrateWarnings()
		apiSchema()
		_, err = db.Exec(`INSERT INTO schema_migrations(id,version) VALUES(1,1) ON CONFLICT(id) DO UPDATE SET version=excluded.version`)
		must(err)
		schemaVersion = 1
	}
	if schemaVersion < 2 {
		must(migrateGuestChapters())
		_, err = db.Exec(`INSERT INTO schema_migrations(id,version) VALUES(1,2) ON CONFLICT(id) DO UPDATE SET version=excluded.version`)
		must(err)
		schemaVersion = 2
	}
	if schemaVersion < 3 {
		must(migrateCommunity())
		_, err = db.Exec(`INSERT INTO schema_migrations(id,version) VALUES(1,3) ON CONFLICT(id) DO UPDATE SET version=excluded.version`)
		must(err)
		schemaVersion = 3
	}
	if schemaVersion < 4 {
		must(migrateDrafts())
		_, err = db.Exec(`INSERT INTO schema_migrations(id,version) VALUES(1,4) ON CONFLICT(id) DO UPDATE SET version=excluded.version`)
		must(err)
	}
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
	mux := http.NewServeMux()
	staticFS, err := fs.Sub(frontend.Assets, "static")
	must(err)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("/", home)
	mux.HandleFunc("/browse", browse)
	mux.HandleFunc("/work/", work)
	mux.HandleFunc("/login", login)
	mux.HandleFunc("/recovery", func(w http.ResponseWriter, r *http.Request) {
		render(w, r, "recovery", Page{Title: "Password recovery"})
	})
	mux.HandleFunc("/register", register)
	mux.HandleFunc("/logout", logout)
	mux.HandleFunc("/publish", publish)
	mux.HandleFunc("/drafts/", drafts)
	mux.HandleFunc("/guest/verify", guestVerify)
	mux.HandleFunc("/api/guest/resend", resendGuestConfirmation)
	mux.HandleFunc("/guest/recover", func(w http.ResponseWriter, r *http.Request) {
		render(w, r, "guestrecover", Page{Title: "Resend story confirmation"})
	})
	mux.HandleFunc("/verification", func(w http.ResponseWriter, r *http.Request) {
		render(w, r, "resendverification", Page{Title: "Resend verification email"})
	})
	mux.HandleFunc("/guest/claim/", claimGuestWork)
	mux.HandleFunc("/chapters/add/", addChapter)
	mux.HandleFunc("/chapters/manage/", manageChapters)
	mux.HandleFunc("/dashboard", dashboard)
	mux.HandleFunc("/subscriptions", subscriptionPage)
	mux.HandleFunc("/notifications", notificationsPage)
	mux.HandleFunc("/about", about)
	mux.HandleFunc("/report/", reportPage)
	mux.HandleFunc("/moderation", moderationPage)
	mux.HandleFunc("/profile", profile)
	mux.HandleFunc("/people/", publicProfile)
	mux.HandleFunc("/edit/", editWork)
	mux.HandleFunc("/api/csrf", apiCSRF)
	mux.HandleFunc("/api/account/register", accountRegister)
	mux.HandleFunc("/api/account/verify", accountVerify)
	mux.HandleFunc("/api/account/verification", func(w http.ResponseWriter, r *http.Request) { accountRequestToken(w, r, "verify") })
	mux.HandleFunc("/api/account/recovery", func(w http.ResponseWriter, r *http.Request) { accountRequestToken(w, r, "reset") })
	mux.HandleFunc("/api/account/reset", accountReset)
	mux.HandleFunc("/api/account/delete", accountDelete)
	mux.HandleFunc("/api/pseudonyms", apiPseudonyms)
	mux.HandleFunc("/api/reports", reportWork)
	mux.HandleFunc("/api/works/delete", deleteWork)
	mux.HandleFunc("/api/comments/delete", deleteComment)
	mux.HandleFunc("/api/admin/audit", adminAudit)
	mux.HandleFunc("/api/admin/reports", adminReports)
	mux.HandleFunc("/api/admin/actions", adminAction)
	return security(mux)
}

func seed() {
	var n int
	db.QueryRow("SELECT count(*) FROM works").Scan(&n)
	if n > 0 {
		return
	}
	samples := []Work{
		{Title: "The Cartography of Small Things", Author: "mothandmaple", Fandom: "Original Work", Rating: "General Audiences", Warnings: "No archive warnings apply", Tags: "Found Family, Quiet Magic, Letters", Summary: "A mapmaker returns to the town she once disappeared from, and finds that someone has been carefully keeping track of all the places she loved.", Content: "On the morning the map arrived, the town was still folded into the fog.\n\nMara stood at the window with a cup of tea cooling in her hands. Across the street, the old bookshop had opened its door, just as it had every morning she could remember. Some things, she thought, knew how to wait.\n\nInside the envelope was a map of a place she had spent years trying to forget. In the corner, in a hand she recognized at once, someone had written: You can always find your way back.", Status: "Complete", Words: 4820},
		{Title: "Where the Light Gets In", Author: "inkandembers", Fandom: "The Locked Tomb Series", Rating: "Teen And Up Audiences", Warnings: "No archive warnings apply", Tags: "Character Study, Grief and Healing, Slow Burn", Summary: "There are a hundred ways to haunt a house. There are almost as many ways to come home.", Content: "The house remembered them, even when they could not remember themselves.\n\nIn the afternoon light, dust turned slowly in the air, and every room held its breath.", Status: "In Progress", Words: 12640},
		{Title: "A Constellation of Ordinary Days", Author: "papercranes", Fandom: "Good Omens", Rating: "Teen And Up Audiences", Warnings: "Author chose not to use archive warnings", Tags: "Aziraphale/Crowley, Bookshops, Gentle Romance", Summary: "An angel, a demon, and the rather extraordinary business of learning how to spend an ordinary afternoon.", Content: "The bookshop was quiet in the particular way it only ever was after rain. Crowley considered this a personal affront to the laws of London traffic.", Status: "Complete", Words: 8350},
		{Title: "Saltwater, After Everything", Author: "northbound", Fandom: "Our Flag Means Death", Rating: "Mature", Warnings: "Major character death", Tags: "Edward Teach/Stede Bonnet, Second Chances, The Sea", Summary: "The sea gives back what it can. The rest, they will have to make for themselves.", Content: "The sea was kind that morning, which made everything a little more difficult to explain.", Status: "In Progress", Words: 21940},
		{Title: "The Language of Wildflowers", Author: "larkspurwrites", Fandom: "Original Work", Rating: "General Audiences", Warnings: "No archive warnings apply", Tags: "Poetry, Nature, Hopeful Ending", Summary: "A small collection of poems about growing around the things we cannot change.", Content: "Somewhere the wildflowers\nare learning the names\nof every road home.", Status: "Complete", Words: 2100},
		{Title: "A Study in Second Chances", Author: "astrolabe", Fandom: "Sherlock Holmes & Related Fandoms", Rating: "Teen And Up Audiences", Warnings: "No archive warnings apply", Tags: "Friendship, Mystery, London", Summary: "A curious letter arrives at Baker Street, addressed to a man who has been dead for seventeen years.", Content: "The letter arrived on a Tuesday, tucked between the morning paper and a bill for coal.", Status: "Complete", Words: 6700},
	}
	for _, w := range samples {
		db.Exec(`INSERT INTO works(title,author,fandom,rating,warnings,tags,summary,content,status,words,updated) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, w.Title, w.Author, w.Fandom, w.Rating, w.Warnings, w.Tags, w.Summary, w.Content, w.Status, w.Words, time.Now().Add(-time.Duration(len(w.Title))*time.Hour).Format("2006-01-02"))
	}
}

type requestIdentityKey struct{}
type requestIdentity struct {
	id   int
	name string
}

func current(r *http.Request) (int, string) {
	if cached, ok := r.Context().Value(requestIdentityKey{}).(requestIdentity); ok {
		return cached.id, cached.name
	}
	c, e := r.Cookie("session")
	if e != nil {
		return 0, ""
	}
	var id int
	var name string
	db.QueryRow(`SELECT users.id,username FROM sessions JOIN users ON users.id=sessions.user_id WHERE token=? AND expires>? AND users.suspended=0`, sessionHash(c.Value), time.Now().Unix()).Scan(&id, &name)
	*r = *r.WithContext(context.WithValue(r.Context(), requestIdentityKey{}, requestIdentity{id: id, name: name}))
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
func queryWorks(where string, args ...any) []Work { return queryWorksSelect(false, where, args...) }
func queryWorksWithContent(where string, args ...any) []Work {
	return queryWorksSelect(true, where, args...)
}
func queryWorksSelect(includeContent bool, where string, args ...any) []Work {
	contentColumn := "''"
	if includeContent {
		contentColumn = "content"
	}
	rows, e := db.Query(`SELECT id,COALESCE(user_id,0),title,author,fandom,rating,warnings,tags,summary,updated,status,language,words,`+contentColumn+`,(SELECT count(*) FROM kudos WHERE work_id=works.id), 1+(SELECT count(*) FROM chapters WHERE work_id=works.id) FROM works WHERE published=1 `+where, args...)
	if e != nil {
		log.Print(e)
		return nil
	}
	defer rows.Close()
	var out []Work
	for rows.Next() {
		var x Work
		rows.Scan(&x.ID, &x.OwnerID, &x.Title, &x.Author, &x.Fandom, &x.Rating, &x.Warnings, &x.Tags, &x.Summary, &x.Updated, &x.Status, &x.Language, &x.Words, &x.Content, &x.Kudos, &x.Chapters)
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
	add(storedFandom(q.Get("fandom")), "fandom LIKE ?")
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
		order = "words DESC, id DESC"
	case "title":
		order = "title ASC, id ASC"
	case "kudos":
		order = "(SELECT count(*) FROM kudos WHERE work_id=works.id) DESC, id DESC"
	}
	const pageSize = 20
	page := 1
	if requested, e := strconv.Atoi(q.Get("page")); e == nil && requested > 0 && requested <= int(^uint(0)>>1)/pageSize {
		page = requested
	}
	args = append(args, pageSize+1, (page-1)*pageSize)
	works := queryWorks(where+" ORDER BY "+order+" LIMIT ? OFFSET ?", args...)
	hasNext := len(works) > pageSize
	if hasNext {
		works = works[:pageSize]
	}
	pageURL := func(n int) string {
		values := r.URL.Query()
		if n == 1 {
			values.Del("page")
		} else {
			values.Set("page", strconv.Itoa(n))
		}
		encoded := values.Encode()
		if encoded == "" {
			return "/browse"
		}
		return "/browse?" + encoded
	}
	previous, next := "", ""
	if page > 1 {
		previous = pageURL(page - 1)
	}
	if hasNext {
		next = pageURL(page + 1)
	}
	render(w, r, "browse", Page{Title: "Browse stories", Works: works, Count: len(works), Query: q.Get("q"), Sort: sort, Filters: q, PageNumber: page, PreviousURL: previous, NextURL: next})
}
func work(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/work/"))
	if e != nil {
		http.NotFound(w, r)
		return
	}
	works := queryWorksWithContent(" AND id=?", id)
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
	chapter := 1
	if raw := r.URL.Query().Get("chapter"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			http.NotFound(w, r)
			return
		}
		chapter = n
	}
	chapterTitle := works[0].Title
	if chapter > 1 {
		if err := db.QueryRow("SELECT title,content FROM chapters WHERE work_id=? AND number=?", id, chapter).Scan(&chapterTitle, &works[0].Content); err != nil {
			http.NotFound(w, r)
			return
		}
	}
	prev, next := 0, 0
	if chapter > 1 {
		prev = chapter - 1
	}
	if chapter < works[0].Chapters {
		next = chapter + 1
	}
	rows, err := db.Query(`SELECT comments.id,comments.user_id,username,body,created FROM comments JOIN users ON users.id=comments.user_id WHERE work_id=? ORDER BY comments.id`, id)
	var comments []Comment
	if err == nil {
		for rows.Next() {
			var c Comment
			var commenter int
			rows.Scan(&c.ID, &commenter, &c.Author, &c.Body, &c.Created)
			c.CanDelete = uid != 0 && (uid == commenter || uid == works[0].OwnerID)
			comments = append(comments, c)
		}
		rows.Close()
	}
	var bookmarked int
	if uid != 0 {
		db.QueryRow("SELECT count(*) FROM bookmarks WHERE user_id=? AND work_id=?", uid, id).Scan(&bookmarked)
	}
	render(w, r, "work", Page{Title: works[0].Title, Work: works[0], Comments: comments, Bookmarked: bookmarked > 0, Subscribed: subscribed(uid, "story", id), CanSubscribe: uid != 0 && uid != works[0].OwnerID, CurrentChapter: chapter, PrevChapter: prev, NextChapter: next, ChapterTitle: chapterTitle})
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
	p := Page{Title: "Post a story"}
	if r.Method == "POST" {
		p.DraftID, _ = strconv.Atoi(r.FormValue("draft_id"))
		p.DraftRevision, _ = strconv.Atoi(r.FormValue("draft_revision"))
	}
	if uid != 0 {
		p.Pseudonyms = pseudonyms(uid)
	}
	if r.Method == "GET" && uid != 0 && r.URL.Query().Get("draft") != "" {
		if !loadDraft(w, r, uid, &p) {
			return
		}
	}
	if r.Method == "POST" {
		title := strings.TrimSpace(r.FormValue("title"))
		content := strings.TrimSpace(r.FormValue("content"))
		fandom := storedFandom(strings.TrimSpace(r.FormValue("fandom")))
		rating := r.FormValue("rating")
		if uid == 0 {
			if !allowAuth(r) {
				http.Error(w, "Too many attempts. Please try again later.", 429)
				return
			}
			name = strings.TrimSpace(r.FormValue("guest_name"))
			p.GuestName = name
			p.GuestEmail = strings.TrimSpace(r.FormValue("guest_email"))
		}
		pseud := r.FormValue("pseudonym")
		if uid != 0 && pseud != "" {
			var exists int
			db.QueryRow("SELECT count(*) FROM pseudonyms WHERE user_id=? AND name=?", uid, pseud).Scan(&exists)
			if exists != 1 {
				http.Error(w, "Invalid pseudonym", 400)
				return
			}
			name = pseud
		}
		warningValues := r.Form["warnings"]
		warnings := strings.Join(warningValues, ", ")
		p.Work = Work{Title: title, Content: content, Fandom: fandom, Rating: rating, Warnings: warnings, Tags: r.FormValue("tags"), Summary: r.FormValue("summary"), Status: r.FormValue("status"), Author: pseud}
		if title == "" || len(title) > 300 || content == "" || fandom == "" || len(fandom) > 300 || !allowedRating[rating] || !validWarnings(warningValues) || (r.FormValue("status") != "Complete" && r.FormValue("status") != "In Progress") {
			p.Error = "Complete the required fields and select valid warnings. Choose “None apply” on its own."
		} else if uid == 0 {
			if err := submitGuestWork(p.GuestEmail, name, p.Work); err != nil {
				p.Error = err.Error()
			} else {
				p.Message = "Check your email to confirm and publish your story. You can claim it later with a verified account using the same email."
				p.Work = Work{}
				p.GuestName = ""
				p.GuestEmail = ""
			}
		} else {
			id, e := publishOwnedStory(uid, name, p.Work, p.DraftID, p.DraftRevision)
			if e == nil {
				http.Redirect(w, r, fmt.Sprintf("/work/%d", id), 303)
				return
			}
			p.Error = "Could not publish your story. If this draft was changed in another tab, copy your text and reload it before trying again."
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
	render(w, r, "dashboard", Page{Title: "Your dashboard", Works: works, Bookmarks: bookmarks, Claimable: claimableWorks(uid), IsAdmin: isAdmin(uid), Drafts: listDrafts(uid)})
}
func about(w http.ResponseWriter, r *http.Request) {
	render(w, r, "about", Page{Title: "About Everlore"})
}

var allowedRating = map[string]bool{"General Audiences": true, "Teen And Up Audiences": true, "Mature": true, "Explicit": true, "Not Rated": true}
var warningOptions = []string{"Sexual content", "Non-con", "Dub-con", "Incest", "Underage sex", "Graphic violence", "None apply"}
var legacyWarnings = map[string]string{"No Archive Warnings Apply": "No archive warnings apply", "Creator Chose Not To Use Archive Warnings": "Author chose not to use archive warnings", "Graphic Depictions of Violence": "Graphic descriptions of violence", "Major Character Death": "Major character death", "Rape/Non-Con": "Rape/non-con", "Underage": "Underage sex"}

func hasWarning(selected, warning string) bool {
	for _, value := range strings.Split(selected, ", ") {
		if value == warning {
			return true
		}
	}
	return false
}
func hasCurrentWarning(selected, warning string) bool {
	return validWarnings(strings.Split(selected, ", ")) && hasWarning(selected, warning)
}
func validWarnings(values []string) bool {
	if len(values) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, v := range values {
		valid := false
		for _, option := range warningOptions {
			if v == option {
				valid = true
				break
			}
		}
		if !valid || seen[v] {
			return false
		}
		seen[v] = true
	}
	return len(values) == 1 || !seen["None apply"]
}
func migrateWarnings() {
	for old, newValue := range legacyWarnings {
		_, err := db.Exec("UPDATE works SET warnings=? WHERE warnings=?", newValue, old)
		must(err)
	}
}
