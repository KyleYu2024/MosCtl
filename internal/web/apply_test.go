package web

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvalidRulesAndConflicts(t *testing.T) {
	for _, tc := range []struct{ id, text string }{{"user-iot", "10.0.0.999"}, {"user-iot", "not-an-ip"}, {"hosts", "example.com bad-ip"}, {"force-cn", "regexp:["}, {"force-cn", "https://example.com"}, {"force-nocn", "127.0.0.1"}} {
		if err := validateRule(tc.id, "# note\n"+tc.text); err == nil || !strings.Contains(err.Error(), "第 2 行") {
			t.Fatalf("accepted %s: %v", tc.text, err)
		}
	}
	for _, tc := range []struct{ id, text string }{{"user-iot", "10.0.0.1\n2001:db8::/32"}, {"hosts", "example.com 10.0.0.1 2001:db8::1"}, {"force-cn", "full:example.com\ndomain:example.org\nregexp:^a\\.example\\.com$\nkeyword:cdn"}} {
		if err := validateRule(tc.id, tc.text); err != nil {
			t.Fatal(err)
		}
	}
	s, _ := newTestServer(t)
	if err := os.WriteFile(filepath.Join(s.ruleDir, "force-cn.txt"), []byte("example.com\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.checkRuleConflict("force-nocn", "EXAMPLE.COM."); err == nil {
		t.Fatal("conflict accepted")
	}
}
func TestFailedApplicationRestoresAndChecksOriginal(t *testing.T) {
	s, _ := newTestServer(t)
	path := filepath.Join(s.ruleDir, "force-cn.txt")
	if err := os.WriteFile(path, []byte("old.example\n"), 0640); err != nil {
		t.Fatal(err)
	}
	checks := 0
	s.apply = func(context.Context) error {
		checks++
		data, _ := os.ReadFile(path)
		if checks == 1 {
			if string(data) != "new.example\n" {
				t.Fatal("new file not staged")
			}
			return errors.New("DNS startup failed")
		}
		if string(data) != "old.example\n" {
			t.Fatal("recovery did not restore original")
		}
		return nil
	}
	if err := s.applyFile(context.Background(), path, []byte("new.example\n"), true); err == nil || !strings.Contains(err.Error(), "已恢复原文件") {
		t.Fatalf("got %v", err)
	}
	if checks != 2 {
		t.Fatalf("checks=%d", checks)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0640 {
		t.Fatal("permissions changed")
	}
	if _, err := os.Stat(path + ".mosctl-pending"); !os.IsNotExist(err) {
		t.Fatal("journal left after successful recovery")
	}
}
