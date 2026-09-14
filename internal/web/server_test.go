package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestServer(t *testing.T) (*Server, *int) {
	t.Helper()
	t.Setenv("USERNAME", "admin")
	t.Setenv("PASSWORD", "admin123")
	restarts := 0
	srv, err := New(Options{RuleDir: t.TempDir(), Restart: func() error { restarts++; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	return srv, &restarts
}

func loginCookie(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	body := bytes.NewBufferString(`{"username":"admin","password":"admin123"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/login", body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", rec.Code, rec.Body.String())
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "mosctl_session" {
			return cookie
		}
	}
	t.Fatal("session cookie missing")
	return nil
}

func TestSaveAndApplyRule(t *testing.T) {
	srv, restarts := newTestServer(t)
	handler := srv.Handler()
	cookie := loginCookie(t, handler)

	body, _ := json.Marshal(map[string]any{"content": "example.com\r\n\n# note\n", "apply": true})
	req := httptest.NewRequest(http.MethodPut, "/api/rules/force-nocn", bytes.NewReader(body))
	req.Host = "example.test"
	req.Header.Set("Origin", "http://example.test")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("save status = %d, body = %s", rec.Code, rec.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(srv.ruleDir, "force-nocn.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "example.com\n\n# note\n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	if *restarts != 1 {
		t.Fatalf("restarts = %d, want 1", *restarts)
	}
}

func TestRulesRequireAuthentication(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/rules", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestReadRuleNormalizesWindowsLineEndings(t *testing.T) {
	srv, _ := newTestServer(t)
	if err := os.WriteFile(filepath.Join(srv.ruleDir, "hosts.txt"), []byte("a.test 10.0.0.1\r\nb.test 10.0.0.2"), 0644); err != nil {
		t.Fatal(err)
	}
	handler := srv.Handler()
	cookie := loginCookie(t, handler)
	req := httptest.NewRequest(http.MethodGet, "/api/rules/hosts", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Content != "a.test 10.0.0.1\nb.test 10.0.0.2" {
		t.Fatalf("content = %q", response.Content)
	}
}

func TestLogoutIconAsset(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, tc := range []struct {
		path        string
		contentType string
	}{
		{"/assets/logout.svg", "image/svg+xml"},
		{"/manifest.webmanifest", "application/manifest+json"},
		{"/sw.js", "application/javascript; charset=utf-8"},
		{"/assets/icon-192.png", "image/png"},
		{"/assets/icon-512.png", "image/png"},
		{"/assets/icon-maskable-512.png", "image/png"},
		{"/assets/apple-touch-icon.png", "image/png"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s status = %d, want 200", tc.path, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != tc.contentType {
			t.Errorf("%s content type = %q", tc.path, got)
		}
	}
}

func TestAppShowsVersion(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "v0.5.5") || strings.Contains(body, "{{VERSION}}") {
		t.Fatalf("rendered page does not contain the resolved version")
	}
	if strings.Contains(body, "MosDNS Rule Control") {
		t.Fatalf("removed subtitle is still present")
	}
	if !strings.Contains(body, `apple-mobile-web-app-status-bar-style" content="black"`) || !strings.Contains(body, "safe-area-inset-top") {
		t.Fatalf("mobile safe-area metadata or CSS is missing")
	}
}

func TestRejectsUnknownRuleAndCrossOriginWrite(t *testing.T) {
	srv, _ := newTestServer(t)
	handler := srv.Handler()
	cookie := loginCookie(t, handler)

	for _, tc := range []struct {
		path   string
		origin string
		want   int
	}{
		{"/api/rules/not-real", "http://example.test", http.StatusNotFound},
		{"/api/rules/force-cn", "https://evil.test", http.StatusForbidden},
	} {
		req := httptest.NewRequest(http.MethodPut, tc.path, bytes.NewBufferString(`{"content":"a.com","apply":false}`))
		req.Host = "example.test"
		req.Header.Set("Origin", tc.origin)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s status = %d, want %d", tc.path, rec.Code, tc.want)
		}
	}
}

func TestCredentialsAreRequired(t *testing.T) {
	t.Setenv("USERNAME", "")
	t.Setenv("PASSWORD", "")
	if _, err := New(Options{}); err == nil {
		t.Fatal("expected missing credentials error")
	}
}
