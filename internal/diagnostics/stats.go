package diagnostics

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/miekg/dns"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type DomainCount struct {
	Domain  string            `json:"domain"`
	Route   string            `json:"route"`
	Count   uint64            `json:"count"`
	Errors  uint64            `json:"errors,omitempty"`
	Types   map[string]uint64 `json:"types,omitempty"`
	Results map[string]uint64 `json:"results,omitempty"`
}
type HourCount struct {
	Time   string `json:"time,omitempty"`
	Hour   int    `json:"hour"`
	Local  uint64 `json:"local"`
	Remote uint64 `json:"remote"`
	Other  uint64 `json:"other"`
}
type StatsData struct {
	SchemaVersion int               `json:"schema_version"`
	UpdatedAt     string            `json:"updated_at"`
	Categories    map[string]uint64 `json:"categories"`
	Types         map[string]uint64 `json:"types"`
	Results       map[string]uint64 `json:"results"`
	RecentHours   []HourCount       `json:"recent_hours"`
	Day           string            `json:"day"`
	StartedAt     string            `json:"started_at"`
	Local         uint64            `json:"local"`
	Remote        uint64            `json:"remote"`
	Other         uint64            `json:"other"`
	Domains       []DomainCount     `json:"domains"`
	Hours         []HourCount       `json:"hours"`
	Overflow      uint64            `json:"unranked"`
}
type Collector struct {
	mu      sync.Mutex
	data    StatsData
	domains map[string]*DomainCount
	pending map[uint64]string
	enabled bool
	traces  []Trace
}

var QueryStats = &Collector{}

func (c *Collector) resetDay(now time.Time) {
	if c.data.Day == now.Format("2006-01-02") && c.domains != nil {
		return
	}
	recent := c.data.RecentHours
	c.data = StatsData{SchemaVersion: 2, Categories: make(map[string]uint64), Types: make(map[string]uint64), Results: make(map[string]uint64), RecentHours: recent, Day: now.Format("2006-01-02"), StartedAt: now.Format(time.RFC3339), Hours: make([]HourCount, 24)}
	for i := range c.data.Hours {
		c.data.Hours[i].Hour = i
	}
	c.domains = make(map[string]*DomainCount)
}

func (c *Collector) Consume(line string) bool {
	route := ""
	for _, name := range []string{"LOCAL", "REMOTE", "HOSTS", "REJECTED", "ALL"} {
		if strings.Contains(line, "\tMOSCTL_STATS_"+name+"\t") {
			route = name
			break
		}
	}
	if route == "" {
		return false
	}
	index := strings.IndexByte(line, '{')
	if index < 0 {
		return true
	}
	var q struct {
		ID      uint64 `json:"uqid"`
		Domain  string `json:"qname"`
		Type    uint16 `json:"qtype"`
		Rcode   *int   `json:"rcode"`
		Error   string `json:"error"`
		Client  string `json:"client"`
		Elapsed string `json:"elapsed"`
	}
	if json.Unmarshal([]byte(line[index:]), &q) != nil {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending == nil {
		c.pending = make(map[uint64]string)
	}
	if route != "ALL" {
		if len(c.pending) > 10000 {
			c.pending = make(map[uint64]string)
		}
		c.pending[q.ID] = route
		return true
	}
	actual := c.pending[q.ID]
	delete(c.pending, q.ID)
	now := time.Now()
	c.resetDay(now)
	name := strings.ToLower(strings.TrimSuffix(q.Domain, "."))
	if name == "" {
		return true
	}
	c.data.RecentHours = rollingHours(c.data.RecentHours, now)
	recent := &c.data.RecentHours[len(c.data.RecentHours)-1]
	bucket := &c.data.Hours[now.Hour()]
	category := "unknown"
	switch actual {
	case "LOCAL":
		category = "local"
		c.data.Local++
		bucket.Local++
		recent.Local++
	case "REMOTE":
		category = "remote"
		c.data.Remote++
		bucket.Remote++
		recent.Remote++
	default:
		if actual == "HOSTS" {
			category = "hosts"
		}
		if actual == "REJECTED" {
			category = "rejected"
		}
		c.data.Other++
		bucket.Other++
		recent.Other++
	}
	c.data.Categories[category]++
	c.data.UpdatedAt = now.Format(time.RFC3339)
	typeName := fmt.Sprint(q.Type)
	if q.Type == 0 {
		typeName = "未知"
	} else if n, ok := dns.TypeToString[q.Type]; ok {
		typeName = n
	}
	c.data.Types[typeName]++
	result := "success"
	if actual == "REJECTED" {
		result = "rejected"
	} else if q.Error != "" || q.Rcode == nil {
		result = "error"
	} else if *q.Rcode != 0 {
		result = "dns_error"
	}
	c.data.Results[result]++
	c.traces = append(c.traces, Trace{Domain: name, Type: q.Type, Route: category, Result: result, Client: q.Client, At: now})
	if len(c.traces) > 256 {
		c.traces = c.traces[len(c.traces)-256:]
	}
	key := category + ":" + name
	item := c.domains[key]
	if item == nil {
		if len(c.domains) >= 10000 {
			c.data.Overflow++
			return true
		}
		item = &DomainCount{Domain: name, Route: category}
		c.domains[key] = item
	}
	if len(item.Types) == 0 {
		item.Types = make(map[string]uint64)
		if item.Count > 0 {
			item.Types["历史未知"] = item.Count
		}
	}
	if len(item.Results) == 0 {
		item.Results = make(map[string]uint64)
		if item.Count > 0 {
			item.Results["historical"] = item.Count
		}
	}
	item.Types[typeName]++
	item.Results[result]++
	item.Count++
	if result == "error" {
		item.Errors++
	}
	return true
}

func (c *Collector) Snapshot() StatsData {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resetDay(time.Now())
	c.data.RecentHours = rollingHours(c.data.RecentHours, time.Now())
	data := c.data
	data.Categories = cloneCounts(c.data.Categories)
	data.Types = cloneCounts(c.data.Types)
	data.Results = cloneCounts(c.data.Results)
	data.RecentHours = append([]HourCount(nil), c.data.RecentHours...)
	data.Hours = append([]HourCount(nil), c.data.Hours...)
	data.Domains = make([]DomainCount, 0, len(c.domains))
	for _, item := range c.domains {
		copy := *item
		copy.Types = cloneCounts(item.Types)
		copy.Results = cloneCounts(item.Results)
		data.Domains = append(data.Domains, copy)
	}
	sort.Slice(data.Domains, func(i, j int) bool {
		if data.Domains[i].Count == data.Domains[j].Count {
			return data.Domains[i].Domain < data.Domains[j].Domain
		}
		return data.Domains[i].Count > data.Domains[j].Count
	})
	return data
}
func (c *Collector) SetEnabled(enabled bool) {
	c.mu.Lock()
	c.enabled = enabled
	c.pending = make(map[uint64]string)
	c.mu.Unlock()
}
func (c *Collector) Enabled() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.enabled }
func (c *Collector) ResetPending() { c.mu.Lock(); c.pending = make(map[uint64]string); c.mu.Unlock() }

func (c *Collector) Start(ctx context.Context, path string) <-chan struct{} {
	done := make(chan struct{})
	if data, err := os.ReadFile(path); err == nil {
		var saved StatsData
		if json.Unmarshal(data, &saved) == nil && len(saved.Hours) == 24 {
			c.mu.Lock()
			if len(saved.RecentHours) == 0 {
				for _, h := range saved.Hours {
					stamp, err := time.ParseInLocation("2006-01-02", saved.Day, time.Local)
					if err == nil {
						h.Time = stamp.Add(time.Duration(h.Hour) * time.Hour).Format(time.RFC3339)
						saved.RecentHours = append(saved.RecentHours, h)
					}
				}
			}
			if saved.Categories == nil {
				saved.Categories = map[string]uint64{"local": saved.Local, "remote": saved.Remote, "other": saved.Other}
			}
			if saved.Types == nil {
				saved.Types = map[string]uint64{"历史未知": saved.Local + saved.Remote + saved.Other}
			}
			if saved.Results == nil {
				saved.Results = map[string]uint64{"historical": saved.Local + saved.Remote + saved.Other}
			}
			saved.SchemaVersion = 2
			c.data = saved
			c.domains = make(map[string]*DomainCount)
			for i, item := range saved.Domains {
				if i >= 10000 {
					break
				}
				copy := item
				c.domains[item.Route+":"+item.Domain] = &copy
			}
			c.mu.Unlock()
		}
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				c.save(path)
				return
			case <-ticker.C:
				c.save(path)
			}
		}
	}()
	return done
}
func (c *Collector) save(path string) {
	data, err := json.Marshal(c.Snapshot())
	if err != nil {
		return
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".query-stats-*")
	if err != nil {
		return
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return
	}
	if file.Close() != nil {
		return
	}
	_ = os.Rename(file.Name(), path)
}
func (c *Collector) Flush(path string) { c.save(path) }

// Keep the current partial hour and the preceding 23 hourly buckets.
func rollingHours(previous []HourCount, now time.Time) []HourCount {
	end := now.Truncate(time.Hour)
	values := make(map[string]HourCount)
	for _, h := range previous {
		values[h.Time] = h
	}
	result := make([]HourCount, 24)
	for i := range result {
		stamp := end.Add(time.Duration(i-23) * time.Hour)
		key := stamp.Format(time.RFC3339)
		result[i] = values[key]
		result[i].Time = key
		result[i].Hour = stamp.Hour()
	}
	return result
}

func cloneCounts(src map[string]uint64) map[string]uint64 {
	result := make(map[string]uint64, len(src))
	for k, v := range src {
		result[k] = v
	}
	return result
}

type Trace struct {
	Domain                string
	Type                  uint16
	Route, Result, Client string
	At                    time.Time
}

func (c *Collector) LocalTrace(domain string, typ uint16, since time.Time) (Trace, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.traces) - 1; i >= 0; i-- {
		t := c.traces[i]
		if t.Domain == domain && t.Type == typ && !t.At.Before(since) && (t.Client == "127.0.0.1" || t.Client == "::ffff:127.0.0.1" || t.Client == "::1") {
			return t, true
		}
	}
	return Trace{}, false
}
