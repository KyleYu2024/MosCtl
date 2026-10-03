package web

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/KyleYu2024/mosctl/internal/diagnostics"
)

func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"content": diagnostics.Logs.Snapshot(), "limit_bytes": 256 * 1024})
}
func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"enabled": diagnostics.QueryStats.Enabled(), "data": diagnostics.QueryStats.Snapshot()})
}

func (s *Server) testDNS(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "请求来源无效")
		return
	}
	domains := map[string]string{"baidu": "www.baidu.com", "google": "www.google.com"}
	domain, ok := domains[r.PathValue("target")]
	if r.PathValue("target") == "custom" {
		var req struct {
			Domain string `json:"domain"`
		}
		if decodeJSON(r, &req, 4096) != nil {
			writeError(w, 400, "请输入有效域名")
			return
		}
		var err error
		domain, err = testDomain(req.Domain)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		ok = true
	}
	if !ok {
		writeError(w, http.StatusNotFound, "测试目标不存在")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, "127.0.0.1:53")
	}}
	start := time.Now()
	ips, err := resolver.LookupHost(ctx, domain)
	result := map[string]any{"domain": domain, "elapsed_ms": time.Since(start).Milliseconds(), "addresses": ips, "ok": err == nil}
	result["rule_hint"] = s.ruleHint(domain)
	result["resolver"] = "127.0.0.1:53"
	if err != nil {
		result["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, result)
}

func testDomain(input string) (string, error) {
	input = strings.TrimSpace(input)
	if strings.Contains(input, "://") {
		parsed, err := url.Parse(input)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
			return "", fmt.Errorf("请输入域名或 HTTP/HTTPS 地址")
		}
		input = parsed.Hostname()
	}
	input = strings.ToLower(strings.TrimSuffix(input, "."))
	if net.ParseIP(input) != nil {
		return "", fmt.Errorf("DNS 分流测试需要域名，不能输入 IP")
	}
	if len(input) == 0 || len(input) > 253 {
		return "", fmt.Errorf("域名长度无效")
	}
	for _, label := range strings.Split(input, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("域名格式无效")
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-') {
				return "", fmt.Errorf("请输入有效英文域名（国际域名请使用 Punycode）")
			}
		}
	}
	return input, nil
}

// This is an explanation of configured rule matches, not a per-query trace.
func (s *Server) ruleHint(domain string) string {
	hosts, err := os.ReadFile(filepath.Join(s.ruleDir, "hosts.txt"))
	if err == nil {
		for _, line := range strings.Split(string(hosts), "\n") {
			fields := strings.Fields(strings.SplitN(line, "#", 2)[0])
			if len(fields) > 1 && matchesDomain(domain, fields[0]) {
				return "Hosts 规则匹配"
			}
		}
	}
	for _, rule := range []struct{ file, label string }{
		{"geosite_apple.txt", "Apple 分流规则"}, {"force-cn.txt", "国内规则"}, {"geosite_cn.txt", "国内规则"}, {"force-nocn.txt", "国外规则"}, {"geosite_no_cn.txt", "国外规则"},
	} {
		file, err := os.Open(filepath.Join(s.ruleDir, rule.file))
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 4096), 65536)
		match := false
		for scanner.Scan() {
			if matchesDomain(domain, strings.TrimSpace(scanner.Text())) {
				match = true
				break
			}
		}
		file.Close()
		if match {
			return rule.label + " · " + rule.file + ""
		}
	}
	return "未匹配域名规则；默认模板走国外上游（实际路径可能受缓存、自定义配置和客户端 IP 规则影响）"
}

func matchesDomain(domain, rule string) bool {
	if rule == "" || strings.HasPrefix(rule, "#") {
		return false
	}
	switch {
	case strings.HasPrefix(rule, "full:"):
		return domain == strings.ToLower(strings.TrimPrefix(rule, "full:"))
	case strings.HasPrefix(rule, "keyword:"):
		return strings.Contains(domain, strings.ToLower(strings.TrimPrefix(rule, "keyword:")))
	case strings.HasPrefix(rule, "regexp:"):
		pattern, err := regexp.Compile(strings.TrimPrefix(rule, "regexp:"))
		return err == nil && pattern.MatchString(domain)
	default:
		rule = strings.ToLower(strings.TrimPrefix(rule, "domain:"))
		return domain == rule || strings.HasSuffix(domain, "."+rule)
	}
}
