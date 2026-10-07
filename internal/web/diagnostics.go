package web

import (
	"bufio"
	"context"
	"fmt"
	"github.com/miekg/dns"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/KyleYu2024/mosctl/internal/diagnostics"
	"github.com/KyleYu2024/mosctl/internal/service"
)

func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"content": diagnostics.Logs.Snapshot(), "limit_bytes": 256 * 1024})
}
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, diagnostics.HealthSnapshot())
}

type rankingRow struct {
	diagnostics.DomainCount
	Routes map[string]uint64 `json:"routes"`
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	route := r.URL.Query().Get("route")
	if route == "" {
		route = "all"
	}
	switch route {
	case "all", "local", "remote", "hosts", "rejected", "unknown", "other", "error":
	default:
		writeError(w, 400, "统计分类无效")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit != 50 {
		limit = 10
	}
	data := diagnostics.QueryStats.Snapshot()
	grouped := make(map[string]*rankingRow)
	for _, item := range data.Domains {
		if route != "all" && route != "error" && item.Route != route {
			continue
		}
		if route == "error" && item.Errors == 0 {
			continue
		}
		row := grouped[item.Domain]
		if row == nil {
			row = &rankingRow{DomainCount: diagnostics.DomainCount{Domain: item.Domain, Route: item.Route, Types: make(map[string]uint64), Results: make(map[string]uint64)}, Routes: make(map[string]uint64)}
			grouped[item.Domain] = row
		}
		row.Count += item.Count
		row.Errors += item.Errors
		row.Routes[item.Route] += item.Count
		if len(item.Types) == 0 {
			row.Types["历史未知"] += item.Count
		} else {
			for k, v := range item.Types {
				row.Types[k] += v
			}
		}
		if len(item.Results) == 0 {
			row.Results["historical"] += item.Count
		} else {
			for k, v := range item.Results {
				row.Results[k] += v
			}
		}
		if len(row.Routes) > 1 {
			row.Route = "mixed"
		}
	}
	rows := make([]rankingRow, 0, len(grouped))
	for _, row := range grouped {
		if route == "error" {
			row.Count = row.Errors
		}
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count == rows[j].Count {
			return rows[i].Domain < rows[j].Domain
		}
		return rows[i].Count > rows[j].Count
	})
	count := len(rows)
	if len(rows) > limit {
		rows = rows[:limit]
	}
	data.Domains = nil
	writeJSON(w, 200, map[string]any{"enabled": diagnostics.QueryStats.Enabled(), "data": data, "ranking": rows, "ranking_total": count, "fetched_at": time.Now().Format(time.RFC3339)})
}

func (s *Server) testDNS(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeError(w, 403, "请求来源无效")
		return
	}
	targets := map[string]string{"baidu": "www.baidu.com", "google": "www.google.com"}
	domain, ok := targets[r.PathValue("target")]
	typ := uint16(dns.TypeA)
	if r.PathValue("target") == "custom" {
		var req struct {
			Domain string `json:"domain"`
			Type   string `json:"type"`
		}
		if decodeJSON(r, &req, 4096) != nil {
			writeError(w, 400, "请输入有效域名")
			return
		}
		if req.Type != "" {
			var supported bool
			typ, supported = map[string]uint16{"A": dns.TypeA, "AAAA": dns.TypeAAAA, "HTTPS": dns.TypeHTTPS, "PTR": dns.TypePTR}[req.Type]
			if !supported {
				writeError(w, 400, "支持 A、AAAA、HTTPS、PTR")
				return
			}
		}
		var err error
		if typ == dns.TypePTR && net.ParseIP(strings.TrimSpace(req.Domain)) != nil {
			domain, err = dns.ReverseAddr(strings.TrimSpace(req.Domain))
			domain = strings.TrimSuffix(domain, ".")
		} else {
			domain, err = testDomain(req.Domain)
		}
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		ok = true
	}
	if !ok {
		writeError(w, 404, "测试目标不存在")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	query := new(dns.Msg)
	query.SetQuestion(dns.Fqdn(domain), typ)
	start := time.Now()
	client := &dns.Client{Net: "udp", Timeout: 5 * time.Second}
	reply, _, err := client.ExchangeContext(ctx, query, s.dnsAddress)
	if err == nil && reply.Truncated {
		client.Net = "tcp"
		reply, _, err = client.ExchangeContext(ctx, query, s.dnsAddress)
	}
	result := map[string]any{"domain": domain, "type": dns.TypeToString[typ], "elapsed_ms": time.Since(start).Milliseconds(), "ok": false, "resolver": s.dnsAddress, "rule_hint": s.ruleHint(domain)}
	if err != nil {
		result["error"] = err.Error()
	} else {
		result["rcode"] = dns.RcodeToString[reply.Rcode]
		result["ok"] = reply.Rcode == dns.RcodeSuccess
		records := make([]string, 0, len(reply.Answer))
		addresses := []string{}
		for _, rr := range reply.Answer {
			records = append(records, rr.String())
			switch rr := rr.(type) {
			case *dns.A:
				addresses = append(addresses, rr.A.String())
			case *dns.AAAA:
				addresses = append(addresses, rr.AAAA.String())
			}
		}
		result["records"] = records
		result["addresses"] = addresses
		if reply.Rcode != dns.RcodeSuccess {
			result["error"] = dns.RcodeToString[reply.Rcode]
		}
	}
	// Summary logs may still be moving through the pipe when the DNS packet arrives.
	for i := 0; i < 10; i++ {
		if trace, found := diagnostics.QueryStats.LocalTrace(domain, typ, start); found {
			result["actual_route"] = trace.Route
			result["actual_result"] = trace.Result
			break
		}
		select {
		case <-ctx.Done():
			i = 10
		case <-time.After(10 * time.Millisecond):
		}
	}
	writeJSON(w, 200, result)
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
		{"force-cn.txt", "自定义国内规则"}, {"force-nocn.txt", "自定义国外规则"}, {"geosite_apple.txt", "Apple 分流规则"}, {"geosite_cn.txt", "国内规则"}, {"geosite_no_cn.txt", "国外规则"},
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

func (s *Server) restartDNS(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "请求来源无效")
		return
	}
	if !service.OperationMu.TryLock() {
		writeError(w, http.StatusConflict, "正在重启，请稍候")
		return
	}
	defer service.OperationMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	s.logger.Print("🔄 用户请求重启 MosDNS")
	if err := service.RestartAndWait(ctx); err != nil {
		writeError(w, 500, "重启失败："+err.Error())
		return
	}
	resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, "127.0.0.1:53")
	}}
	for _, domain := range []string{"www.baidu.com", "www.google.com"} {
		var lastErr error
		for attempt := 0; attempt < 20; attempt++ {
			_, lastErr = resolver.LookupHost(ctx, domain)
			if lastErr == nil {
				break
			}
			select {
			case <-ctx.Done():
				writeError(w, 500, "重启后解析检查超时")
				return
			case <-time.After(250 * time.Millisecond):
			}
		}
		if lastErr != nil {
			writeError(w, 500, "重启后解析检查失败："+domain+"："+lastErr.Error())
			return
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": "MosDNS 已重启，国内、国外解析检查通过"})
}
