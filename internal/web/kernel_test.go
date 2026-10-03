package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKernelEndpointsRequireLogin(t *testing.T) {
	s, _ := newTestServer(t)
	for _, endpoint := range []struct{ method, path string }{{"GET", "/api/settings/kernel"}, {"POST", "/api/settings/kernel/check"}, {"POST", "/api/settings/kernel/update"}, {"POST", "/api/settings/kernel/rollback"}} {
		r := httptest.NewRequest(endpoint.method, endpoint.path, nil)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("%s: %d", endpoint.path, w.Code)
		}
	}
}
func TestKernelActionRejectsForeignOrigin(t *testing.T) {
	s, _ := newTestServer(t)
	h := s.Handler()
	cookie := loginCookie(t, h)
	r := httptest.NewRequest("POST", "/api/settings/kernel/update", nil)
	r.Header.Set("Origin", "http://evil.test")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status: %d", w.Code)
	}
}
func TestKernelStatusAndUnsupportedAction(t *testing.T) {
	s, _ := newTestServer(t)
	h := s.Handler()
	cookie := loginCookie(t, h)
	r := httptest.NewRequest("GET", "/api/settings/kernel", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
	r = httptest.NewRequest("POST", "/api/settings/kernel/delete", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("invalid action: %d", w.Code)
	}
}
