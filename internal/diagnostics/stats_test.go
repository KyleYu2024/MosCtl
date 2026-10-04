package diagnostics

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
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

func TestRollingHoursCrossMidnight(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 4, 0, 15, 0, 0, loc)
	previous := []HourCount{{Time: now.Add(-time.Hour).Truncate(time.Hour).Format(time.RFC3339), Local: 7}, {Time: now.Add(-24 * time.Hour).Truncate(time.Hour).Format(time.RFC3339), Local: 99}}
	hours := rollingHours(previous, now)
	if len(hours) != 24 || hours[22].Hour != 23 || hours[22].Local != 7 || hours[23].Hour != 0 || hours[0].Local != 0 {
		t.Fatalf("incorrect window: %+v", hours)
	}
	c := &Collector{data: StatsData{RecentHours: hours}}
	c.resetDay(now)
	c.data.Local = 10
	c.resetDay(now.Add(24 * time.Hour))
	if c.data.Local != 0 || c.data.RecentHours[22].Local != 7 {
		t.Fatal("daily reset lost rolling history or retained daily totals")
	}
}
