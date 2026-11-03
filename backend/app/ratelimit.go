package everlore

import (
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// Vercel supplies a sanitized client IP; local servers do not trust forwarded headers.
func clientIP(r *http.Request) string {
	if os.Getenv("VERCEL") != "" {
		forwarded := strings.TrimSpace(r.Header.Get("X-Vercel-Forwarded-For"))
		if ip := net.ParseIP(forwarded); ip != nil {
			return ip.String()
		}
	}
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		host = r.RemoteAddr
	}
	return host
}
func allowAuth(r *http.Request) bool {
	key := clientIP(r)
	now := time.Now().Unix()
	tx, e := db.Begin()
	if e != nil {
		return false
	}
	defer tx.Rollback()
	if rand.IntN(64) == 0 {
		if _, e = tx.Exec("DELETE FROM rate_limits WHERE created<?", now-600); e != nil {
			return false
		}
	}
	var n int
	if e = tx.QueryRow("SELECT count(*) FROM rate_limits WHERE key=? AND created>?", key, now-600).Scan(&n); e != nil || n >= 20 {
		return false
	}
	if _, e = tx.Exec("INSERT INTO rate_limits(key,created) VALUES(?,?)", key, now); e != nil {
		return false
	}
	return tx.Commit() == nil
}
