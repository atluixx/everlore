package main

import (
	"database/sql"
	"golang.org/x/crypto/bcrypt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestMultiUserIsolation(t *testing.T) {
	var err error
	db, err = sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA foreign_keys=ON; CREATE TABLE users(id INTEGER PRIMARY KEY, username TEXT UNIQUE NOT NULL,password TEXT NOT NULL); CREATE TABLE sessions(token TEXT PRIMARY KEY,user_id INTEGER NOT NULL REFERENCES users(id),expires INTEGER NOT NULL); CREATE TABLE works(id INTEGER PRIMARY KEY,user_id INTEGER REFERENCES users(id),title TEXT NOT NULL,author TEXT NOT NULL,fandom TEXT NOT NULL,rating TEXT NOT NULL,warnings TEXT NOT NULL,tags TEXT NOT NULL,summary TEXT NOT NULL,content TEXT NOT NULL,language TEXT NOT NULL DEFAULT 'English',status TEXT NOT NULL DEFAULT 'Complete',words INTEGER NOT NULL,updated TEXT NOT NULL,published INTEGER NOT NULL DEFAULT 1); CREATE TABLE bookmarks(user_id INTEGER REFERENCES users(id),work_id INTEGER REFERENCES works(id),PRIMARY KEY(user_id,work_id)); CREATE TABLE kudos(user_id INTEGER REFERENCES users(id),work_id INTEGER REFERENCES works(id),PRIMARY KEY(user_id,work_id)); CREATE TABLE comments(id INTEGER PRIMARY KEY,user_id INTEGER REFERENCES users(id),work_id INTEGER REFERENCES works(id),body TEXT NOT NULL,created TEXT NOT NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	migrate()
	apiSchema()
	mux := http.NewServeMux()
	mux.HandleFunc("/register", register)
	mux.HandleFunc("/login", login)
	mux.HandleFunc("/publish", publish)
	mux.HandleFunc("/edit/", editWork)
	mux.HandleFunc("/work/", work)
	mux.HandleFunc("/dashboard", dashboard)
	mux.HandleFunc("/profile", profile)
	mux.HandleFunc("/api/csrf", apiCSRF)
	mux.HandleFunc("/api/account/register", accountRegister)
	mux.HandleFunc("/api/account/verify", accountVerify)
	mux.HandleFunc("/api/account/recovery", func(w http.ResponseWriter, r *http.Request) { accountRequestToken(w, r, "reset") })
	mux.HandleFunc("/api/account/reset", accountReset)
	mux.HandleFunc("/api/reports", reportWork)
	mux.HandleFunc("/api/admin/actions", adminAction)
	server := httptest.NewServer(security(mux))
	defer server.Close()
	client := func() *http.Client {
		jar, _ := cookiejar.New(nil)
		return &http.Client{Jar: jar, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	}
	a, b := client(), client()
	post := func(c *http.Client, path string, form url.Values) *http.Response {
		t.Helper()
		u, _ := url.Parse(server.URL)
		for _, cookie := range c.Jar.Cookies(u) {
			if cookie.Name == "csrf" {
				form.Set("csrf", cookie.Value)
			}
		}
		res, e := c.PostForm(server.URL+path, form)
		if e != nil {
			t.Fatal(e)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		return res
	}
	get := func(c *http.Client, path string) *http.Response {
		t.Helper()
		res, e := c.Get(server.URL + path)
		if e != nil {
			t.Fatal(e)
		}
		return res
	}
	for _, v := range []struct {
		c    *http.Client
		name string
	}{{a, "alice"}, {b, "bobby"}} {
		hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		if _, err := db.Exec("INSERT INTO users(username,password,email_verified) VALUES(?,?,1)", v.name, string(hash)); err != nil {
			t.Fatal(err)
		}
		res := get(v.c, "/login")
		res.Body.Close()
		res = post(v.c, "/login", url.Values{"username": {v.name}, "password": {"password123"}})
		if res.StatusCode != 303 {
			t.Fatalf("login %s: %d", v.name, res.StatusCode)
		}
	}
	// Invalid metadata must not discard an author's draft text.
	csrfURL, _ := url.Parse(server.URL)
	form := url.Values{"title": {"A title"}, "fandom": {"Original Work"}, "rating": {"invalid"}, "warnings": {"No Archive Warnings Apply"}, "content": {"A draft worth keeping"}, "status": {"Complete"}}
	for _, cookie := range a.Jar.Cookies(csrfURL) {
		if cookie.Name == "csrf" {
			form.Set("csrf", cookie.Value)
		}
	}
	invalid, err := a.PostForm(server.URL+"/publish", form)
	if err != nil {
		t.Fatal(err)
	}
	invalidBody, _ := io.ReadAll(invalid.Body)
	invalid.Body.Close()
	if !strings.Contains(string(invalidBody), "A draft worth keeping") || !strings.Contains(string(invalidBody), "A title") {
		t.Fatal("validation lost entered work")
	}
	res := post(a, "/publish", url.Values{"title": {"Alice's story"}, "fandom": {"Original Work"}, "rating": {"General Audiences"}, "warnings": {"No Archive Warnings Apply"}, "content": {"Once upon a time"}, "status": {"Complete"}})
	if res.StatusCode != 303 {
		t.Fatalf("publish: %d", res.StatusCode)
	}
	res = get(b, "/edit/1")
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Fatalf("another user could access editor: %d", res.StatusCode)
	}
	res = post(b, "/edit/1", url.Values{"title": {"Stolen"}, "content": {"Changed"}, "fandom": {"Original Work"}})
	if res.StatusCode != 404 {
		t.Fatalf("another user could edit: %d", res.StatusCode)
	}
	res = post(b, "/work/1", url.Values{"action": {"bookmark"}})
	if res.StatusCode != 303 {
		t.Fatal(res.StatusCode)
	}
	res = get(b, "/dashboard")
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(body), "Alice&#39;s story") {
		t.Fatal("bookmark missing from owner's dashboard")
	}
	res = get(a, "/dashboard")
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(body), "You have no bookmarks yet") {
		t.Fatal("bookmark leaked to another account")
	}
	bare, _ := http.PostForm(server.URL+"/login", url.Values{"username": {"alice"}, "password": {"password123"}})
	bare.Body.Close()
	if bare.StatusCode != 403 {
		t.Fatalf("missing CSRF accepted: %d", bare.StatusCode)
	}
	var mailedToken string
	originalMailer := sendMail
	defer func() { sendMail = originalMailer }()
	sendMail = func(email, purpose, token string) error { mailedToken = token; return nil }
	c := client()
	res = get(c, "/api/csrf")
	res.Body.Close()
	res = post(c, "/api/account/register", url.Values{"username": {"charlie"}, "email": {"charlie@example.com"}, "password": {"password123"}})
	if res.StatusCode != 201 || mailedToken == "" {
		t.Fatalf("registration: %d", res.StatusCode)
	}
	res = post(c, "/login", url.Values{"username": {"charlie"}, "password": {"password123"}})
	if res.StatusCode == 303 {
		t.Fatal("unverified account logged in")
	}
	verification := mailedToken
	res = post(c, "/api/account/verify", url.Values{"token": {verification}})
	if res.StatusCode != 200 {
		t.Fatalf("verify: %d", res.StatusCode)
	}
	res = post(c, "/api/account/verify", url.Values{"token": {verification}})
	if res.StatusCode != 400 {
		t.Fatal("verification token reused")
	}
	res = post(c, "/login", url.Values{"username": {"charlie"}, "password": {"password123"}})
	if res.StatusCode != 303 {
		t.Fatal("verified login failed")
	}
	res = post(c, "/api/account/recovery", url.Values{"email": {"charlie@example.com"}})
	if res.StatusCode != 200 || mailedToken == verification {
		t.Fatal("recovery email missing")
	}
	res = post(c, "/api/account/reset", url.Values{"token": {mailedToken}, "password": {"newpassword123"}})
	if res.StatusCode != 200 {
		t.Fatalf("reset: %d", res.StatusCode)
	}
	res = get(c, "/dashboard")
	res.Body.Close()
	if res.StatusCode != 303 {
		t.Fatal("password reset did not revoke session")
	}
	res = post(b, "/api/reports", url.Values{"work_id": {"1"}, "reason": {"Please review this work"}})
	if res.StatusCode != 201 {
		t.Fatal("report creation failed")
	}
	res = post(b, "/api/admin/actions", url.Values{"id": {"1"}, "action": {"hide_work"}, "reason": {"Policy review"}})
	if res.StatusCode != 403 {
		t.Fatal("non-admin moderation accepted")
	}
	if _, err = db.Exec("UPDATE users SET role='admin' WHERE username='alice'"); err != nil {
		t.Fatal(err)
	}
	res = post(a, "/api/admin/actions", url.Values{"id": {"1"}, "action": {"hide_work"}, "reason": {"Policy review"}})
	if res.StatusCode != 200 {
		t.Fatalf("moderation failed: %d", res.StatusCode)
	}
	res = get(b, "/work/1")
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Fatal("hidden work remains public")
	}

}
