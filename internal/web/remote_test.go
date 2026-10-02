package web

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoteSavePreservesOtherUpstreams(t *testing.T) {
	t.Setenv("USERNAME", "admin")
	t.Setenv("PASSWORD", "admin123")
	dir := t.TempDir()
	restarts := 0
	configPath := filepath.Join(dir, "config.yaml")
	config := `plugins:
  - tag: forward_local
    args:
      upstreams:
        - addr: udp://223.5.5.5
        - addr: udp://223.6.6.6
  - tag: forward_remote
    args:
      upstreams:
        - addr: udp://8.8.8.8 # TAG_REMOTE
`
	if err := os.WriteFile(configPath, []byte(config), 0640); err != nil {
		t.Fatal(err)
	}
	srv, err := New(Options{RuleDir: filepath.Join(dir, "rules"), Restart: func() error { restarts++; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	handler := srv.Handler()
	cookie := loginCookie(t, handler)
	req := httptest.NewRequest("PUT", "/api/settings/remote", bytes.NewBufferString(`{"remote":"192.0.2.53:53"}`))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d %s", rec.Code, rec.Body.String())
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	_, addr, err := remoteNode(data)
	if err != nil || addr.Value != "udp://192.0.2.53:53" {
		t.Fatalf("unexpected remote %v %v", addr, err)
	}
	if !bytes.Contains(data, []byte("223.6.6.6")) || !bytes.Contains(data, []byte("TAG_REMOTE")) || restarts != 1 {
		t.Fatal("unrelated configuration lost or restart missing")
	}
	if !bytes.Contains(data, []byte("WEB_MANAGED")) {
		t.Fatal("Web configuration ownership marker missing")
	}
	info, _ := os.Stat(configPath)
	if info.Mode().Perm() != 0640 {
		t.Fatal("permissions changed")
	}
	readReq := httptest.NewRequest("GET", "/api/settings/remote", nil)
	readReq.AddCookie(cookie)
	readRec := httptest.NewRecorder()
	handler.ServeHTTP(readRec, readReq)
	var response struct{ Remote string }
	if err = json.Unmarshal(readRec.Body.Bytes(), &response); err != nil || response.Remote != "udp://192.0.2.53:53" {
		t.Fatal("read-back failed")
	}
}

func TestRemoteValidation(t *testing.T) {
	for _, input := range []string{"udp://1.1.1.1:53", "192.0.2.53:53", "https://dns.example/dns-query", "tls://dns.example:853"} {
		if _, err := validateRemote(input); err != nil {
			t.Fatal(input, err)
		}
	}
	for _, input := range []string{"", "file:///etc/passwd", "udp://host:99999", "udp://host/path", "https://user:pass@host/dns-query"} {
		if _, err := validateRemote(input); err == nil {
			t.Fatal("accepted", input)
		}
	}
}

func TestPageRoutes(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, path := range []string{"/rules", "/logs", "/tests", "/settings"} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 200 {
			t.Fatal(path, rec.Code)
		}
	}
}
