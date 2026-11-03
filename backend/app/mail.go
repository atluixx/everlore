package everlore

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"strings"
	"time"
)

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return hex.EncodeToString(b), nil
}
func sendAccountMail(email, purpose, token string) error {
	base := strings.TrimRight(os.Getenv("EVERLORE_BASE_URL"), "/")
	addr := os.Getenv("EVERLORE_SMTP_ADDR")
	from := os.Getenv("EVERLORE_SMTP_FROM")
	if base == "" {
		return errors.New("mail service is not configured")
	}
	u, e := url.Parse(base)
	if e != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("EVERLORE_BASE_URL must be an HTTPS URL")
	}
	subject := "Verify your Everlore account"
	path := "/api/account/verify"
	if purpose == "guest" {
		subject = "Confirm your Everlore story"
		path = "/guest/verify"
	}
	if purpose == "reset" {
		subject = "Reset your Everlore password"
		path = "/api/account/reset"
	}
	link := base + path + "?token=" + url.QueryEscape(token)
	if key := os.Getenv("RESEND_API_KEY"); key != "" {
		sender := os.Getenv("EVERLORE_EMAIL_FROM")
		if sender == "" || strings.ContainsAny(sender, "\r\n") {
			return errors.New("EVERLORE_EMAIL_FROM is required")
		}
		return sendResendMail(&http.Client{Timeout: 15 * time.Second}, "https://api.resend.com/emails", key, sender, email, subject, subject+":\n"+link+"\n\nThis link expires automatically.\n")
	}
	if addr == "" || from == "" || strings.ContainsAny(from, "\r\n") {
		return errors.New("mail service is not configured")
	}
	host, _, e := net.SplitHostPort(addr)
	if e != nil {
		return e
	}
	body := "From: " + from + "\r\nTo: " + email + "\r\nSubject: " + subject + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + subject + ":\r\n" + link + "\r\n\r\nThis link expires automatically.\r\n"
	// smtp.SendMail requires STARTTLS when offered; require it explicitly so credentials and tokens never travel in plaintext.
	conn, e := net.DialTimeout("tcp", addr, 10*time.Second)
	if e != nil {
		return e
	}
	conn.SetDeadline(time.Now().Add(30 * time.Second))
	c, e := smtp.NewClient(conn, host)
	if e != nil {
		return e
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return errors.New("SMTP server does not support STARTTLS")
	}
	if e = c.StartTLS(nil); e != nil {
		return e
	}
	if user := os.Getenv("EVERLORE_SMTP_USER"); user != "" {
		if e = c.Auth(smtp.PlainAuth("", user, os.Getenv("EVERLORE_SMTP_PASSWORD"), host)); e != nil {
			return e
		}
	}
	if e = c.Mail(from); e != nil {
		return e
	}
	if e = c.Rcpt(email); e != nil {
		return e
	}
	wc, e := c.Data()
	if e != nil {
		return e
	}
	if _, e = wc.Write([]byte(body)); e != nil {
		wc.Close()
		return e
	}
	if e = wc.Close(); e != nil {
		return e
	}
	return c.Quit()
}

func sendResendMail(client *http.Client, endpoint, key, from, to, subject, body string) error {
	payload, err := json.Marshal(map[string]any{"from": from, "to": []string{to}, "subject": subject, "text": body})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("email provider request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("email provider rejected request (HTTP %d)", resp.StatusCode)
	}
	return nil
}

var sendMail = sendAccountMail

func issueToken(uid int, email, purpose string) error {
	token, e := randomToken()
	if e != nil {
		return e
	}
	expiry := time.Now().Add(time.Hour)
	if purpose == "verify" {
		expiry = time.Now().Add(24 * time.Hour)
	}
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("DELETE FROM account_tokens WHERE user_id=? AND purpose=?", uid, purpose); e != nil {
		return e
	}
	if _, e = tx.Exec("INSERT INTO account_tokens VALUES(?,?,?,?)", sessionHash(token), uid, purpose, expiry.Unix()); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	return sendMail(email, purpose, token)
}
