package kernel

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fakeVersion(_ context.Context, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	v := strings.TrimPrefix(string(data), "\x7fELF")
	if _, ok := versionNumbers(v); !ok {
		return "", errors.New("bad version")
	}
	return v, nil
}
func fakeBinary(version string) []byte {
	return append([]byte{0x7f, 'E', 'L', 'F'}, []byte(version)...)
}
func archive(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, data := range entries {
		f, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func fixture(t *testing.T) (*Manager, []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mosdns")
	if err := os.WriteFile(path, fakeBinary("v5.3.3"), 0755); err != nil {
		t.Fatal(err)
	}
	data := archive(t, map[string][]byte{"mosdns": fakeBinary("v5.3.4-0-gb732318"), "README.md": []byte("readme")})
	sum := sha256.Sum256(data)
	name := "mosdns-linux-" + runtime.GOARCH + ".zip"
	r := release{Tag: "v5.3.4", Assets: []asset{{Name: name, URL: "https://github.com/IrineSistiana/mosdns/releases/download/v5.3.4/" + name, Digest: "sha256:" + hex.EncodeToString(sum[:])}}}
	metadata, _ := json.Marshal(r)
	client := &http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
		body := data
		if req.URL.String() == releaseAPI {
			body = metadata
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	})}
	return New(Options{Binary: path, Supported: true, Client: client, Version: fakeVersion, Restart: func(context.Context) error { return nil }, Health: func(context.Context) error { return nil }}), data
}
func wait(t *testing.T, m *Manager) Status {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s := m.Snapshot()
		if !s.Busy {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("operation timed out")
	return Status{}
}
func start(t *testing.T, m *Manager, action string) Status {
	t.Helper()
	if err := m.Start(context.Background(), action); err != nil {
		t.Fatal(err)
	}
	return wait(t, m)
}
func TestUpdateAndRollback(t *testing.T) {
	m, _ := fixture(t)
	s := start(t, m, "check")
	if !s.UpdateAvailable || s.Latest != "v5.3.4" {
		t.Fatalf("check: %+v", s)
	}
	s = start(t, m, "update")
	if s.Error != "" || s.Current != "v5.3.4-0-gb732318" || s.Backup != "v5.3.3" || s.UpdateAvailable {
		t.Fatalf("update: %+v", s)
	}
	s = start(t, m, "rollback")
	if s.Error != "" || s.Current != "v5.3.3" || !s.UpdateAvailable || s.Backup != "v5.3.4-0-gb732318" {
		t.Fatalf("rollback: %+v", s)
	}
	if _, err := os.Stat(m.opts.Binary + ".mosctl-pending"); !os.IsNotExist(err) {
		t.Fatal("pending transaction left behind")
	}
}
func TestFailedUpdateRestoresOldBinary(t *testing.T) {
	for _, failure := range []string{"restart", "health"} {
		t.Run(failure, func(t *testing.T) {
			m, _ := fixture(t)
			start(t, m, "check")
			var attempts atomic.Int32
			failOnce := func(context.Context) error {
				if attempts.Add(1) == 1 {
					return fmt.Errorf("%s failed", failure)
				}
				return nil
			}
			if failure == "restart" {
				m.opts.Restart = failOnce
			} else {
				m.opts.Health = failOnce
			}
			s := start(t, m, "update")
			if s.Current != "v5.3.3" || !strings.Contains(s.Error, "已恢复旧内核") {
				t.Fatalf("recovery: %+v", s)
			}
			if attempts.Load() != 2 {
				t.Fatalf("attempts %d", attempts.Load())
			}
		})
	}
}
func TestChecksumFailureDoesNotReplaceBinary(t *testing.T) {
	m, data := fixture(t)
	start(t, m, "check")
	m.opts.Client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		corrupt := append([]byte(nil), data...)
		corrupt[0] ^= 1
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(corrupt))}, nil
	})
	s := start(t, m, "update")
	if s.Current != "v5.3.3" || !strings.Contains(s.Error, "SHA-256") {
		t.Fatalf("checksum: %+v", s)
	}
	if _, err := os.Stat(m.opts.Binary + ".mosctl-pending"); !os.IsNotExist(err) {
		t.Fatal("backup made for unverified download")
	}
}
func TestRecoverInterruptedUpdate(t *testing.T) {
	m, _ := fixture(t)
	if err := atomicCopy(m.opts.Binary, m.opts.Binary+".mosctl-pending"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.opts.Binary, fakeBinary("v5.3.4"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := Recover(m.opts.Binary); err != nil {
		t.Fatal(err)
	}
	v, _ := fakeVersion(context.Background(), m.opts.Binary)
	if v != "v5.3.3" {
		t.Fatalf("recovered %s", v)
	}
	if err := Recover(m.opts.Binary); err != nil {
		t.Fatal(err)
	}
}
func TestConcurrentOperationsRejected(t *testing.T) {
	m, _ := fixture(t)
	start(t, m, "check")
	entered, release := make(chan struct{}), make(chan struct{})
	m.opts.Restart = func(context.Context) error { close(entered); <-release; return nil }
	if err := m.Start(context.Background(), "update"); err != nil {
		t.Fatal(err)
	}
	<-entered
	if err := m.Start(context.Background(), "check"); err == nil {
		t.Fatal("concurrent operation allowed")
	}
	close(release)
	wait(t, m)
}
func TestReleaseValidation(t *testing.T) {
	name := "mosdns-linux-amd64.zip"
	a := asset{Name: name, URL: "https://github.com/IrineSistiana/mosdns/releases/download/v5.3.4/" + name, Digest: "sha256:" + strings.Repeat("a", 64)}
	if !trustedAsset(a, "v5.3.4") {
		t.Fatal("valid asset rejected")
	}
	for _, mutate := range []func(*asset){func(a *asset) { a.URL = "https://example.com/" + name }, func(a *asset) { a.Digest = "" }, func(a *asset) { a.URL = strings.Replace(a.URL, "https:", "http:", 1) }, func(a *asset) { a.URL += "?x=y" }} {
		bad := a
		mutate(&bad)
		if trustedAsset(bad, "v5.3.4") {
			t.Fatalf("invalid asset accepted: %+v", bad)
		}
	}
	if newer("v5.3.4", "v5.3.4-0-gb732318") || !newer("v5.3.10", "v5.3.4") || newer("v5.3.4", "v5.4.0") {
		t.Fatal("version comparison incorrect")
	}
}
func TestArchiveDoesNotExtractPaths(t *testing.T) {
	for _, name := range []string{"../mosdns", "/mosdns", "subdir/mosdns"} {
		data := archive(t, map[string][]byte{name: fakeBinary("v5.3.4")})
		if _, err := extractBinary(data); err == nil {
			t.Fatalf("unsafe path accepted: %s", name)
		}
	}
}
