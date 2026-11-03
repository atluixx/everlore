package everlore

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func csrfRequest(origin string) *http.Request {
	form := url.Values{"csrf": {strings.Repeat("a", 64)}}
	req := httptest.NewRequest(http.MethodPost, "https://everlore.online/change", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	req.AddCookie(&http.Cookie{Name: "csrf", Value: strings.Repeat("a", 64)})
	return req
}

func TestSecurityRejectsCrossOriginPost(t *testing.T) {
	handler := security(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, tc := range []struct {
		name, origin string
		want         int
	}{
		{name: "cross origin", origin: "https://attacker.example", want: http.StatusForbidden},
		{name: "sibling subdomain", origin: "https://evil.everlore.online", want: http.StatusForbidden},
		{name: "same origin", origin: "https://everlore.online", want: http.StatusNoContent},
		{name: "non-browser client with valid token", want: http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, csrfRequest(tc.origin))
			if response.Code != tc.want {
				t.Fatalf("status = %d, want %d", response.Code, tc.want)
			}
		})
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler := security(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "https://everlore.online/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "opaque"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	for header, want := range map[string]string{
		"Cache-Control":             "no-store",
		"Content-Security-Policy":   "default-src 'self'",
		"Permissions-Policy":        "camera=()",
		"Strict-Transport-Security": "max-age=31536000",
	} {
		if value := response.Header().Get(header); !strings.Contains(value, want) {
			t.Errorf("%s = %q, expected it to contain %q", header, value, want)
		}
	}
}
