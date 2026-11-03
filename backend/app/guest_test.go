package everlore

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestGuestConfirmationClaimAndChapters(t *testing.T) {
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); os.Chdir(old) })
	for _, key := range []string{"TURSO_DATABASE_URL", "VERCEL", "EVERLORE_BOOTSTRAP_ADMIN"} {
		t.Setenv(key, "")
	}
	t.Setenv("EVERLORE_SEED", "0")
	handler := NewHandler()
	server := httptest.NewServer(handler)
	defer server.Close()
	oldMailer := sendMail
	t.Cleanup(func() { sendMail = oldMailer })
	var confirmation string
	sendMail = func(email, purpose, token string) error {
		if email != "guest@example.com" || purpose != "guest" {
			t.Errorf("unexpected mail recipient or purpose")
		}
		confirmation = token
		return nil
	}
	newClient := func() *http.Client {
		jar, _ := cookiejar.New(nil)
		return &http.Client{Jar: jar, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	}
	guest, owner, other := newClient(), newClient(), newClient()
	get := func(client *http.Client, path string) (int, string) {
		t.Helper()
		r, e := client.Get(server.URL + path)
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r.StatusCode, string(b)
	}
	post := func(client *http.Client, path string, values url.Values) (int, string) {
		t.Helper()
		u, _ := url.Parse(server.URL)
		for _, c := range client.Jar.Cookies(u) {
			if c.Name == "csrf" {
				values.Set("csrf", c.Value)
			}
		}
		r, e := client.PostForm(server.URL+path, values)
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r.StatusCode, string(b)
	}
	if code, _ := get(guest, "/publish"); code != 200 {
		t.Fatalf("guest publish form: %d", code)
	}
	values := url.Values{"guest_name": {"Writer"}, "guest_email": {"guest@example.com"}, "title": {"Guest story"}, "fandom": {"Original Work"}, "rating": {"General Audiences"}, "warnings": {"None apply"}, "content": {"First chapter text"}, "status": {"In Progress"}}
	if code, body := post(guest, "/publish", values); code != 200 || !strings.Contains(body, "Check your email") || confirmation == "" {
		t.Fatalf("guest submit: %d %s", code, body)
	}
	if code, _ := get(guest, "/work/1"); code != 404 {
		t.Fatalf("unconfirmed work visible: %d", code)
	}
	if code, _ := get(guest, "/chapters/add/1"); code != 303 {
		t.Fatalf("guest chapter access: %d", code)
	}
	if code, _ := post(guest, "/guest/verify", url.Values{"token": {confirmation}}); code != 303 {
		t.Fatalf("confirm: %d", code)
	}
	if code, body := get(guest, "/work/1"); code != 200 || !strings.Contains(body, "First chapter text") || strings.Contains(body, "guest@example.com") {
		t.Fatalf("confirmed work: %d", code)
	}
	if code, _ := post(guest, "/guest/verify", url.Values{"token": {confirmation}}); code != 400 {
		t.Fatalf("token reuse: %d", code)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	for _, u := range []struct {
		name, email string
		client      *http.Client
	}{{"owner", "guest@example.com", owner}, {"other", "other@example.com", other}} {
		if _, err = db.Exec("INSERT INTO users(username,password,email,email_verified) VALUES(?,?,?,1)", u.name, string(hash), u.email); err != nil {
			t.Fatal(err)
		}
		get(u.client, "/login")
		if code, _ := post(u.client, "/login", url.Values{"username": {u.name}, "password": {"password123"}}); code != 303 {
			t.Fatalf("login %s: %d", u.name, code)
		}
	}
	if code, _ := post(other, "/guest/claim/1", url.Values{}); code != 404 {
		t.Fatalf("another user claimed: %d", code)
	}
	if _, body := get(owner, "/dashboard"); !strings.Contains(body, "Claim your guest stories") {
		t.Fatal("eligible guest work missing")
	}
	if code, _ := post(owner, "/guest/claim/1", url.Values{}); code != 303 {
		t.Fatalf("owner claim: %d", code)
	}
	if code, _ := post(other, "/subscriptions", url.Values{"kind": {"story"}, "id": {"1"}, "action": {"subscribe"}}); code != 303 {
		t.Fatal("story subscription failed")
	}
	if code, _ := post(other, "/subscriptions", url.Values{"kind": {"author"}, "id": {"1"}, "action": {"subscribe"}}); code != 303 {
		t.Fatal("author subscription failed")
	}
	if code, _ := post(other, "/chapters/add/1", url.Values{"title": {"Other"}, "content": {"Not theirs"}}); code != 404 {
		t.Fatalf("another user added chapter: %d", code)
	}
	if code, _ := post(owner, "/chapters/add/1", url.Values{"title": {"Second chapter"}, "content": {"Second chapter text"}}); code != 303 {
		t.Fatalf("add chapter: %d", code)
	}
	var noticeCount int
	if err := db.QueryRow("SELECT count(*) FROM notifications WHERE user_id=2").Scan(&noticeCount); err != nil || noticeCount != 1 {
		t.Fatalf("chapter should create one notification despite two subscriptions: %d %v", noticeCount, err)
	}
	if _, body := get(other, "/notifications"); !strings.Contains(body, "new chapter") {
		t.Fatal("subscriber notification missing")
	}
	if _, body := get(owner, "/notifications"); strings.Contains(body, "new chapter") {
		t.Fatal("notification leaked to author")
	}
	if code, _ := post(owner, "/edit/1", url.Values{"title": {"Guest story"}, "fandom": {"Original Work"}, "content": {"New first chapter"}}); code != 303 {
		t.Fatalf("edit first chapter: %d", code)
	}
	var words int
	if err := db.QueryRow("SELECT words FROM works WHERE id=1").Scan(&words); err != nil || words != 6 {
		t.Fatalf("total chapter word count: %d, %v", words, err)
	}
	if code, _ := post(owner, "/chapters/add/1", url.Values{"title": {"Third"}, "content": {"Third text"}}); code != 303 {
		t.Fatal("third chapter failed")
	}
	if err := changeChapter(2, 1, 3, "up", "", ""); err == nil {
		t.Fatal("non-owner reordered chapters")
	}
	if err := changeChapter(1, 1, 3, "up", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := changeChapter(1, 1, 2, "save", "Revised", "new content here"); err != nil {
		t.Fatal(err)
	}
	if err := changeChapter(1, 1, 2, "delete", "", ""); err != nil {
		t.Fatal(err)
	}
	if code, body := get(guest, "/work/1?chapter=2"); code != 200 || !strings.Contains(body, "Second chapter text") || !strings.Contains(body, "Previous chapter") {
		t.Fatal(fmt.Sprintf("second chapter: %d", code))
	}
	if code, body := get(guest, "/work/1"); code != 200 || !strings.Contains(body, "New first chapter") || !strings.Contains(body, "Next chapter") {
		t.Fatal(fmt.Sprintf("first chapter: %d", code))
	}
}

func TestGuestMailFailureDoesNotPublish(t *testing.T) {
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); os.Chdir(old) })
	for _, key := range []string{"TURSO_DATABASE_URL", "VERCEL", "EVERLORE_BOOTSTRAP_ADMIN"} {
		t.Setenv(key, "")
	}
	t.Setenv("EVERLORE_SEED", "0")
	NewHandler()
	previous := sendMail
	t.Cleanup(func() { sendMail = previous })
	sendMail = func(_, _, _ string) error { return fmt.Errorf("mail unavailable") }
	work := Work{Title: "Unsent", Fandom: "Original Work", Rating: "General Audiences", Warnings: "None apply", Content: "A chapter", Status: "Complete"}
	if err = submitGuestWork("guest@example.com", "Writer", work); err == nil {
		t.Fatal("mail failure accepted")
	}
	var works, claims int
	if err = db.QueryRow("SELECT count(*) FROM works").Scan(&works); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT count(*) FROM guest_claims").Scan(&claims); err != nil {
		t.Fatal(err)
	}
	if works != 0 || claims != 0 {
		t.Fatalf("unpublished draft was not cleaned up: %d works %d claims", works, claims)
	}
}
