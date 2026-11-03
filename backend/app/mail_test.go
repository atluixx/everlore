package everlore

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendResendMail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("invalid request")
		}
		var body struct {
			From    string   `json:"from"`
			To      []string `json:"to"`
			Subject string   `json:"subject"`
			Text    string   `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.From != "sender@example.com" || len(body.To) != 1 || body.To[0] != "reader@example.com" || body.Text != "body" {
			t.Error("invalid email payload")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	if err := sendResendMail(server.Client(), server.URL, "test-key", "sender@example.com", "reader@example.com", "subject", "body"); err != nil {
		t.Fatal(err)
	}
}
func TestSendResendMailRejectsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "sensitive provider detail", http.StatusUnauthorized)
	}))
	defer server.Close()
	err := sendResendMail(server.Client(), server.URL, "test-key", "sender@example.com", "reader@example.com", "subject", "body")
	if err == nil || err.Error() != "email provider rejected request (HTTP 401)" {
		t.Fatalf("unexpected error: %v", err)
	}
}
