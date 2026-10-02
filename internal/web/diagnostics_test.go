package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiagnosticsRequireLogin(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, route := range []struct{ method, path string }{{"GET", "/api/logs"}, {"POST", "/api/test/baidu"}} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(route.method, route.path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s status=%d", route.path, rec.Code)
		}
	}
}

func TestDNSRejectsOtherTargetsAndOrigins(t *testing.T) {
	srv, _ := newTestServer(t)
	handler := srv.Handler()
	cookie := loginCookie(t, handler)
	for _, tc := range []struct {
		path, origin string
		status       int
	}{{"/api/test/unknown", "", 404}, {"/api/test/google", "https://other.test", 403}} {
		req := httptest.NewRequest("POST", tc.path, nil)
		req.AddCookie(cookie)
		req.Header.Set("Origin", tc.origin)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("%s status=%d", tc.path, rec.Code)
		}
	}
}

func TestCustomDomainValidation(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{"https://WWW.Example.com/path?q=1", "www.example.com"}, {"example.com.", "example.com"}} {
		got, err := testDomain(tc.input)
		if err != nil || got != tc.want {
			t.Fatalf("%q: got %q err=%v", tc.input, got, err)
		}
	}
	for _, input := range []string{"127.0.0.1", "http://127.0.0.1/", "example.com:8080", "a..com", "file:///etc/passwd", "https://user:pass@example.com"} {
		if _, err := testDomain(input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}

func TestDomainRuleMatching(t *testing.T) {
	for _, tc := range []struct {
		domain, rule string
		want         bool
	}{
		{"a.example.com", "example.com", true}, {"notexample.com", "example.com", false}, {"a.example.com", "full:example.com", false}, {"a.example.com", "domain:example.com", true}, {"a.example.com", "regexp:[", false},
	} {
		if got := matchesDomain(tc.domain, tc.rule); got != tc.want {
			t.Fatalf("%s %s = %v", tc.domain, tc.rule, got)
		}
	}
}
