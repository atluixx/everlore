package everlore

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"mime/multipart"
	"net"
	"net/http"
	"net/smtp"
	"net/textproto"
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
	textBody, htmlBody := emailContent(purpose, link)
	if key := os.Getenv("RESEND_API_KEY"); key != "" {
		sender := os.Getenv("EVERLORE_EMAIL_FROM")
		if sender == "" || strings.ContainsAny(sender, "\r\n") {
			return errors.New("EVERLORE_EMAIL_FROM is required")
		}
		return sendResendMail(&http.Client{Timeout: 15 * time.Second}, "https://api.resend.com/emails", key, sender, email, subject, textBody, htmlBody)
	}
	if addr == "" || from == "" || strings.ContainsAny(from, "\r\n") {
		return errors.New("mail service is not configured")
	}
	host, _, e := net.SplitHostPort(addr)
	if e != nil {
		return e
	}
	var message bytes.Buffer
	mw := multipart.NewWriter(&message)
	for _, part := range []struct{ contentType, body string }{{"text/plain; charset=UTF-8", textBody}, {"text/html; charset=UTF-8", htmlBody}} {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Type", part.contentType)
		header.Set("Content-Transfer-Encoding", "8bit")
		writer, err := mw.CreatePart(header)
		if err != nil {
			return err
		}
		if _, err = writer.Write([]byte(part.body)); err != nil {
			return err
		}
	}
	if err := mw.Close(); err != nil {
		return err
	}
	body := "From: " + from + "\r\nTo: " + email + "\r\nSubject: " + subject + "\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=\"" + mw.Boundary() + "\"\r\n\r\n" + message.String()
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

func emailContent(purpose, link string) (string, string) {
	heading, intro, action, expiry := "Welcome to Everlore", "Thanks for joining our community fiction archive. Confirm your email to start sharing stories.", "Verify email", "This link expires in 24 hours."
	switch purpose {
	case "reset":
		heading, intro, action, expiry = "Reset your password", "Use the link below to choose a new password for your account.", "Reset password", "This link expires in one hour."
	case "guest":
		heading, intro, action, expiry = "Confirm your story", "Your story is ready to join the archive. Confirm your email to publish it.", "Confirm story", "This link expires in 24 hours."
	}
	plain := heading + "\n\n" + intro + "\n\n" + action + ": " + link + "\n\n" + expiry + " If you didn't request this email, you can ignore it.\n\nEverlore — Every story deserves a place in the archive."
	markup := `<!doctype html><html lang="en"><body style="margin:0;background:#eef2f4;color:#242627;font-family:Arial,Helvetica,sans-serif"><table role="presentation" width="100%" cellspacing="0" cellpadding="0"><tr><td align="center" style="padding:32px 16px"><table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="max-width:560px;background:#fff;border:1px solid #b7c3cc;border-top:5px solid #285989"><tr><td style="padding:32px"><p style="margin:0 0 28px;color:#285989;font-family:Georgia,serif;font-size:25px;font-weight:bold">Everlore</p><h1 style="margin:0 0 18px;color:#1c3f65;font-family:Georgia,serif;font-size:27px;font-weight:normal">` + html.EscapeString(heading) + `</h1><p style="line-height:1.6">` + html.EscapeString(intro) + `</p><p style="margin:28px 0"><a href="` + html.EscapeString(link) + `" style="display:inline-block;padding:12px 20px;background:#285989;color:#fff;text-decoration:none;font-weight:bold">` + html.EscapeString(action) + `</a></p><p style="color:#515d65;font-size:13px;line-height:1.6">` + html.EscapeString(expiry) + ` If you didn't request this email, you can ignore it.</p><p style="color:#515d65;font-size:13px;line-height:1.6;word-break:break-all">If the button doesn't work, copy this link:<br><a href="` + html.EscapeString(link) + `" style="color:#285989">` + html.EscapeString(link) + `</a></p><hr style="border:0;border-top:1px solid #d3dce1;margin:28px 0 18px"><p style="margin:0;color:#515d65;font-family:Georgia,serif;font-size:14px">Every story deserves a place in the archive.</p></td></tr></table></td></tr></table></body></html>`
	return plain, markup
}

func sendResendMail(client *http.Client, endpoint, key, from, to, subject, body, htmlBody string) error {
	payload, err := json.Marshal(map[string]any{"from": from, "to": []string{to}, "subject": subject, "text": body, "html": htmlBody})
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
