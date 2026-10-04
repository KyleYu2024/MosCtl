package diagnostics

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type DomainCount struct {
	Domain string `json:"domain"`
	Route  string `json:"route"`
	Count  uint64 `json:"count"`
}
type HourCount struct {
	Time   string `json:"time,omitempty"`
	Hour   int    `json:"hour"`
	Local  uint64 `json:"local"`
	Remote uint64 `json:"remote"`
	Other  uint64 `json:"other"`
}
type StatsData struct {
	RecentHours []HourCount   `json:"recent_hours"`
	Day         string        `json:"day"`
	StartedAt   string        `json:"started_at"`
	Local       uint64        `json:"local"`
	Remote      uint64        `json:"remote"`
	Other       uint64        `json:"other"`
	Domains     []DomainCount `json:"domains"`
	Hours       []HourCount   `json:"hours"`
	Overflow    uint64        `json:"unranked"`
}
type Collector struct {
	mu      sync.Mutex
	data    StatsData
	domains map[string]*DomainCount
	pending map[uint64]string
	enabled bool
}

var QueryStats = &Collector{}

func (c *Collector) resetDay(now time.Time) {
	if c.data.Day == now.Format("2006-01-02") && c.domains != nil {
		return
	}
	recent := c.data.RecentHours
	c.data = StatsData{RecentHours: recent, Day: now.Format("2006-01-02"), StartedAt: now.Format(time.RFC3339), Hours: make([]HourCount, 24)}
	for i := range c.data.Hours {
		c.data.Hours[i].Hour = i
	}
	c.domains = make(map[string]*DomainCount)
}

func (c *Collector) Consume(line string) bool {
	route := ""
	for _, name := range []string{"LOCAL", "REMOTE", "HOSTS", "ALL"} {
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
		ID     uint64 `json:"uqid"`
		Domain string `json:"qname"`
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
	category := "other"
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
		c.data.Other++
		bucket.Other++
		recent.Other++
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
	item.Count++
	return true
}

func (c *Collector) Snapshot() StatsData {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resetDay(time.Now())
	c.data.RecentHours = rollingHours(c.data.RecentHours, time.Now())
	data := c.data
	data.RecentHours = append([]HourCount(nil), c.data.RecentHours...)
	data.Hours = append([]HourCount(nil), c.data.Hours...)
	data.Domains = make([]DomainCount, 0, len(c.domains))
	for _, item := range c.domains {
		data.Domains = append(data.Domains, *item)
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
