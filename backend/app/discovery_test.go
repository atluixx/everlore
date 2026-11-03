package everlore

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowsePaginationAndListingContent(t *testing.T) {
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); os.Chdir(old) })
	t.Setenv("TURSO_DATABASE_URL", "")
	t.Setenv("VERCEL", "")
	t.Setenv("EVERLORE_SEED", "0")
	t.Setenv("EVERLORE_BOOTSTRAP_ADMIN", "")
	handler := NewHandler()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 45; i++ {
		_, err = tx.Exec(`INSERT INTO works(title,author,fandom,rating,warnings,tags,summary,content,status,words,updated) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, fmt.Sprintf("Work %02d", i), "author", "Example", "General Audiences", "None apply", "Friendship", "Summary", "unique story body", "Complete", 3, "2026-01-01")
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path                     string
		want, absent, navigation string
	}{
		{"/browse?fandom=Example", "Work 45", "Work 25", "page=2"},
		{"/browse?fandom=Example&page=2", "Work 25", "Work 45", "page=3"},
		{"/browse?fandom=Example&page=3", "Work 05", "Work 25", "Previous"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		body := response.Body.String()
		if response.Code != 200 || !strings.Contains(body, test.want) || strings.Contains(body, test.absent) || !strings.Contains(body, test.navigation) || strings.Contains(body, "unique story body") {
			t.Errorf("unexpected results for %s: status %d", test.path, response.Code)
		}
		if !strings.Contains(body, "fandom=Example") {
			t.Errorf("filter missing from pagination for %s", test.path)
		}
	}
	listing := queryWorks(" AND id=?", 1)
	detail := queryWorksWithContent(" AND id=?", 1)
	if len(listing) != 1 || listing[0].Content != "" || len(detail) != 1 || detail[0].Content != "unique story body" {
		t.Error("listing and detail content selection differ from expected")
	}
	// The database file should remain in the temporary test directory.
	if _, err = os.Stat(filepath.Join(dir, "everlore.db")); err != nil {
		t.Fatal(err)
	}
}
func TestDiscoveryIndexes(t *testing.T) {
	// A separate in-memory database ensures the schema index definitions are valid.
	oldDB := db
	var err error
	db, err = sqlOpenTest()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); db = oldDB })
	if _, err = db.Exec(`CREATE TABLE works(id INTEGER PRIMARY KEY,user_id INTEGER,updated TEXT,published INTEGER); CREATE TABLE users(id INTEGER PRIMARY KEY,username TEXT); CREATE TABLE sessions(token TEXT,expires INTEGER); CREATE TABLE kudos(user_id INTEGER,work_id INTEGER); CREATE TABLE comments(id INTEGER PRIMARY KEY,work_id INTEGER);`); err != nil {
		t.Fatal(err)
	}
	migrate()
	for _, query := range []string{"EXPLAIN QUERY PLAN SELECT count(*) FROM kudos WHERE work_id=1", "EXPLAIN QUERY PLAN SELECT id FROM works WHERE published=1 ORDER BY updated DESC, id DESC LIMIT 20"} {
		rows, err := db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		var plan string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan += detail
		}
		rows.Close()
		if !strings.Contains(plan, "INDEX") {
			t.Errorf("expected index use for %q: %s", query, plan)
		}
	}
}

func sqlOpenTest() (*sql.DB, error) { return sql.Open("sqlite", ":memory:") }

func TestOriginalStoriesFandomCompatibility(t *testing.T) {
	if got := displayFandom("Original Work"); got != "Original Stories" {
		t.Fatalf("display fandom = %q", got)
	}
	if got := storedFandom("Original Stories"); got != "Original Work" {
		t.Fatalf("stored fandom = %q", got)
	}
	if got := storedFandom("Good Omens"); got != "Good Omens" {
		t.Fatalf("unrelated fandom changed: %q", got)
	}
}
