package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingStage struct {
	fail   string
	closed bool
}

func (f *failingStage) Chmod(os.FileMode) error {
	if f.fail == "chmod" {
		return errors.New("chmod failed")
	}
	return nil
}
func (f *failingStage) Write(p []byte) (int, error) {
	if f.fail == "write" {
		return 0, errors.New("disk full")
	}
	if f.fail == "short" {
		return len(p) - 1, nil
	}
	return len(p), nil
}
func (f *failingStage) Sync() error {
	if f.fail == "sync" {
		return errors.New("sync failed")
	}
	return nil
}
func (f *failingStage) Close() error {
	f.closed = true
	if f.fail == "close" {
		return errors.New("close failed")
	}
	return nil
}

func TestStagingPropagatesEveryFailure(t *testing.T) {
	for _, failure := range []string{"chmod", "write", "short", "sync", "close"} {
		t.Run(failure, func(t *testing.T) {
			f := &failingStage{fail: failure}
			err := writeStaged(f, []byte("new data"), 0600)
			if err == nil || !f.closed {
				t.Fatalf("failure swallowed: %v, closed=%v", err, f.closed)
			}
			if failure == "short" && !errors.Is(err, io.ErrShortWrite) {
				t.Fatal(err)
			}
		})
	}
}
func TestManagedWatcherAndInterruptedApplication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.txt")
	if err := os.WriteFile(path, []byte("old"), 0640); err != nil {
		t.Fatal(err)
	}
	backup, err := BackupFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = backup.Journal(path); err != nil {
		t.Fatal(err)
	}
	if err = WriteManaged(path, []byte("new"), 0640); err != nil {
		t.Fatal(err)
	}
	if !ManagedContents(path) {
		t.Fatal("managed event not suppressed")
	}
	if err = os.WriteFile(path, []byte("external edit"), 0640); err != nil {
		t.Fatal(err)
	}
	if ManagedContents(path) {
		t.Fatal("external edit suppressed")
	}
	if err = RecoverFile(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	if string(data) != "old" || info.Mode().Perm() != 0640 {
		t.Fatal("old content or permissions lost")
	}
	if err = RecoverFile(path); err != nil {
		t.Fatal(err)
	}
}

func TestBatchFailureRestoresAllFiles(t *testing.T) {
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")}
	files := make(map[string][]byte)
	for _, path := range paths {
		os.WriteFile(path, []byte("old"), 0644)
		files[path] = []byte("new")
	}
	checks := 0
	err := ApplyFiles(context.Background(), files, func(context.Context) error {
		checks++
		if checks == 1 {
			return errors.New("bad config")
		}
		for _, path := range paths {
			data, _ := os.ReadFile(path)
			if string(data) != "old" {
				t.Fatal("batch partially restored")
			}
		}
		return nil
	})
	if err == nil || checks != 2 {
		t.Fatalf("err=%v checks=%d", err, checks)
	}
}
func TestInterruptedCleanupKeepsCommittedBatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	os.WriteFile(path, []byte("old"), 0644)
	backup, _ := BackupFile(path)
	backup.Transaction = "transaction-id"
	backup.CommitPath = filepath.Join(dir, "commit-marker")
	if err := backup.Journal(path); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("new"), 0644)
	os.WriteFile(backup.CommitPath, []byte(backup.Transaction), 0600)
	if err := RecoverFile(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new" {
		t.Fatal("committed file rolled back after interrupted cleanup")
	}
}

func TestDownloadedErrorPageDoesNotReplaceRules(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html>" + strings.Repeat("gateway error", 20) + "</html>"))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "geosite_cn.txt")
	os.WriteFile(path, []byte("original.example\n"), 0644)
	if _, err := DownloadFile(server.URL, path); err == nil {
		t.Fatal("error page accepted as rules")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "original.example\n" {
		t.Fatal("original rules replaced")
	}
}

func TestMalformedRecoveryRecordDoesNotRemoveOriginal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("original"), 0600)
	os.WriteFile(path+".mosctl-pending", []byte("{}"), 0600)
	if err := RecoverFile(path); err == nil {
		t.Fatal("invalid journal accepted")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "original" {
		t.Fatal("original removed")
	}
}
