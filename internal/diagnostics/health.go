package diagnostics

import (
	"context"
	"net"
	"sync"
	"time"
)

type HealthData struct {
	Running   bool   `json:"running"`
	PID       int    `json:"pid"`
	DNSOK     bool   `json:"dns_ok"`
	CheckedAt string `json:"checked_at"`
	Error     string `json:"error,omitempty"`
}

var health struct {
	sync.Mutex
	data HealthData
}

func SetProcess(pid int) {
	health.Lock()
	defer health.Unlock()
	health.data = HealthData{Running: pid > 0, PID: pid}
}
func HealthSnapshot() HealthData { health.Lock(); defer health.Unlock(); return health.data }

// SampleHealth is called by the supervisor, never once per browser request.
func SampleHealth(ctx context.Context, pid int) {
	resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, "127.0.0.1:53")
	}}
	message := ""
	for _, domain := range []string{"www.baidu.com", "www.google.com"} {
		queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, err := resolver.LookupHost(queryCtx, domain)
		cancel()
		if err != nil {
			message = err.Error()
			break
		}
	}
	if ctx.Err() != nil {
		return
	}
	health.Lock()
	defer health.Unlock()
	if health.data.PID != pid {
		return
	}
	health.data.DNSOK = message == ""
	health.data.Error = message
	health.data.CheckedAt = time.Now().Format(time.RFC3339)
}
