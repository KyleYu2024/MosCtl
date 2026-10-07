package diagnostics

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

func TestClassificationAndLegacyMigration(t *testing.T) {
	c := &Collector{}
	c.Consume(summary("REJECTED", 1, "example.com."))
	c.Consume(`time	INFO	main	MOSCTL_STATS_ALL	{"uqid":1,"qname":"example.com.","qtype":65,"rcode":3}`)
	c.Consume(summary("LOCAL", 2, "example.com."))
	c.Consume(`time	INFO	main	MOSCTL_STATS_ALL	{"uqid":2,"qname":"example.com.","qtype":1,"error":"timeout"}`)
	d := c.Snapshot()
	if d.Local != 1 || d.Other != 1 || d.Categories["rejected"] != 1 || d.Results["rejected"] != 1 || d.Results["error"] != 1 || d.Types["HTTPS"] != 1 {
		t.Fatalf("wrong classification: %+v", d)
	}
	c.Consume(summary("LOCAL", 3, "example.com."))
	c.Consume(`time	INFO	main	MOSCTL_STATS_ALL	{"uqid":3,"qname":"example.com.","qtype":65,"rcode":0}`)
	if c.Snapshot().Categories["rejected"] != 1 || c.Snapshot().Results["success"] != 1 {
		t.Fatal("HTTPS upstream request misclassified as rejection")
	}
	d.Categories["local"] = 999
	if c.Snapshot().Categories["local"] != 2 {
		t.Fatal("snapshot maps alias collector")
	}
	old := StatsData{Day: time.Now().Format("2006-01-02"), Local: 3, Remote: 2, Other: 4, Hours: make([]HourCount, 24)}
	data, _ := json.Marshal(old)
	path := filepath.Join(t.TempDir(), "stats.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	restored := &Collector{}
	ctx, cancel := context.WithCancel(context.Background())
	done := restored.Start(ctx, path)
	defer func() { cancel(); <-done }()
	got := restored.Snapshot()
	if got.Categories["other"] != 4 || got.Categories["rejected"] != 0 || got.Results["historical"] != 9 {
		t.Fatalf("history fabricated: %+v", got)
	}
}
