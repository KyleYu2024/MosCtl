package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/KyleYu2024/mosctl/internal/config"
	"github.com/KyleYu2024/mosctl/internal/kernel"
	"github.com/KyleYu2024/mosctl/internal/service"
)

func newKernelManager() *kernel.Manager {
	_, dockerErr := os.Stat("/.dockerenv")
	_, containerErr := os.Stat("/run/.containerenv")
	supported := runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") && service.IsManagedMode() && os.Geteuid() == 0 && os.Getenv(service.EnvMode) != "docker" && os.Getenv("container") != "podman" && os.IsNotExist(dockerErr) && os.IsNotExist(containerErr)
	return kernel.New(kernel.Options{Binary: config.MosDNSBin, Supported: supported, Restart: service.RestartAndWait, Health: kernelDNSHealth})
}
func kernelDNSHealth(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, "127.0.0.1:53")
	}}
	consecutive := 0
	var lastErr error
	for ctx.Err() == nil {
		healthy := true
		for _, domain := range []string{"www.baidu.com", "www.google.com"} {
			queryCtx, stop := context.WithTimeout(ctx, 3*time.Second)
			ips, err := resolver.LookupHost(queryCtx, domain)
			stop()
			if err != nil || len(ips) == 0 {
				healthy = false
				lastErr = err
				break
			}
		}
		if healthy {
			consecutive++
			if consecutive >= 2 {
				return nil
			}
		} else {
			consecutive = 0
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
		}
	}
	return fmt.Errorf("DNS 解析检查未通过: %v", lastErr)
}
func (s *Server) getKernel(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.kernel.Snapshot())
}
func (s *Server) kernelAction(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeError(w, 403, "请求来源无效")
		return
	}
	action := r.PathValue("action")
	if action != "check" && action != "update" && action != "rollback" {
		writeError(w, 400, "操作无效")
		return
	}
	if err := s.kernel.Start(s.kernelContext, action); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, s.kernel.Snapshot())
}
