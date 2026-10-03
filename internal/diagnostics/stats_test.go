package diagnostics

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

func summary(label string, id int, domain string) string {
	return fmt.Sprintf("time\tINFO\tmain\tMOSCTL_STATS_%s\t{\"uqid\":%d,\"qname\":%q}", label, id, domain)
}
func TestStatsUseFinalRouteAndSort(t *testing.T) {
	c := &Collector{}
	c.Consume(summary("LOCAL", 1, "a.example."))
	c.Consume(summary("REMOTE", 1, "a.example."))
	c.Consume(summary("ALL", 1, "a.example."))
	c.Consume(summary("LOCAL", 2, "b.example."))
	c.Consume(summary("ALL", 2, "b.example."))
	c.Consume(summary("LOCAL", 3, "b.example."))
	c.Consume(summary("ALL", 3, "b.example."))
	c.Consume(summary("HOSTS", 4, "home.example."))
	c.Consume(summary("ALL", 4, "home.example."))
	d := c.Snapshot()
	if d.Local != 2 || d.Remote != 1 || d.Other != 1 {
		t.Fatalf("wrong totals %+v", d)
	}
	if d.Domains[0].Domain != "b.example" || d.Domains[0].Count != 2 {
		t.Fatal("ranking not sorted")
	}
	if c.Consume("ordinary log") {
		t.Fatal("normal logs swallowed")
	}
}
func TestStatsPersistAcrossRestart(t *testing.T) {
	c := &Collector{}
	c.Consume(summary("REMOTE", 1, "example.com."))
	c.Consume(summary("ALL", 1, "example.com."))
	path := filepath.Join(t.TempDir(), "stats.json")
	c.Flush(path)
	other := &Collector{}
	ctx, cancel := context.WithCancel(context.Background())
	done := other.Start(ctx, path)
	defer func() { cancel(); <-done }()
	if other.Snapshot().Remote != 1 {
		t.Fatal("persisted data lost")
	}
}
