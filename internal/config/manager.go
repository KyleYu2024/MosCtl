package config

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/KyleYu2024/mosctl/internal/service"
)

const (
	ConfigPath = "/etc/mosdns/config.yaml"
	RuleDir    = "/etc/mosdns/rules"
	MosDNSBin  = "/usr/local/bin/mosdns"
)


func SetUpstream(isLocal bool, addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return fmt.Errorf("地址不能为空")
	}

	// 自动补全协议：如果不包含 :// 且不是以 / 开头（针对 Unix Domain Socket）
	if !strings.Contains(addr, "://") && !strings.HasPrefix(addr, "/") {
		addr = "udp://" + addr
	}

	tag := "# TAG_REMOTE"
	if isLocal {
		tag = "# TAG_LOCAL"
	}

	content, err := os.ReadFile(ConfigPath)
	if err != nil {
		return err
	}

	re := regexp.MustCompile(`addr:\s*"[^"]+"\s*` + tag)
	if !re.Match(content) {
		return fmt.Errorf("找不到标记 %s", tag)
	}

	updatedContent := re.ReplaceAllString(string(content), fmt.Sprintf(`addr: "%s" %s`, addr, tag))
	if err := os.WriteFile(ConfigPath, []byte(updatedContent), 0644); err != nil {
		return err
	}

	return service.RestartService()
}

func SetCacheTTL(ttl string) error {
	content, err := os.ReadFile(ConfigPath)
	if err != nil {
		return err
	}

	re := regexp.MustCompile(`lazy_cache_ttl:\s*\d+`)
	updatedContent := re.ReplaceAllString(string(content), "lazy_cache_ttl: "+ttl)
	if err := os.WriteFile(ConfigPath, []byte(updatedContent), 0644); err != nil {
		return err
	}

	return service.RestartService()
}

func FlushCache() error {
	fmt.Println("🧹 正在清空 DNS 缓存...")
	os.Remove("/etc/mosdns/cache.dump")
	return service.RestartService()
}

// GetCurrentUpstreams 返回 (国内DNS, 国外DNS)
func GetCurrentUpstreams() (string, string) {
	content, err := os.ReadFile(ConfigPath)
	if err != nil {
		return "未知", "未知"
	}
	localRegex := regexp.MustCompile(`addr:\s*"([^"]+)"\s*#\s*TAG_LOCAL`)
	remoteRegex := regexp.MustCompile(`addr:\s*"([^"]+)"\s*#\s*TAG_REMOTE`)

	localMatch := localRegex.FindStringSubmatch(string(content))
	remoteMatch := remoteRegex.FindStringSubmatch(string(content))

	local := "未知"
	remote := "未知"
	if len(localMatch) > 1 {
		local = localMatch[1]
	}
	if len(remoteMatch) > 1 {
		remote = remoteMatch[1]
	}
	return local, remote
}

// GetCurrentTTL 返回当前缓存 TTL
func GetCurrentTTL() string {
	content, err := os.ReadFile(ConfigPath)
	if err != nil {
		return "未知"
	}
	re := regexp.MustCompile(`lazy_cache_ttl:\s*(\d+)`)
	match := re.FindStringSubmatch(string(content))
	if len(match) > 1 {
		return match[1]
	}
	return "未知"
}

// RunTest 运行 DNS 解析测试
func RunTest() {
	fmt.Println("\n🩺 正在进行 DNS 解析诊断...")
	
	testDomain := func(domain, label string) {
		fmt.Printf("  Testing %s (%s) ... ", label, domain)
		
		// 简单起见，使用 nslookup 命令，因为用户习惯看到它的输出
		// 也可以使用 Go 的 net.Resolver
		cmd := exec.Command("nslookup", domain, "127.0.0.1")
		start := time.Now()
		output, err := cmd.CombinedOutput()
		duration := time.Since(start)

		if err == nil {
			fmt.Printf("✅ Pass (%v)\n", duration.Round(time.Millisecond))
			// 提取 IP
			lines := strings.Split(string(output), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "Address:") && !strings.Contains(line, "#53") && !strings.Contains(line, "127.0.0.1") {
					fmt.Printf("     -> %s\n", strings.TrimSpace(strings.TrimPrefix(line, "Address:")))
					break
				}
			}
		} else {
			fmt.Printf("❌ Failed\n")
		}
	}

	testDomain("www.baidu.com", "🇨🇳 国内")
	testDomain("www.google.com", "🌍 国外")
	fmt.Println()
}

// GetLogLevel 获取当前日志级别
func GetLogLevel() string {
	content, err := os.ReadFile(ConfigPath)
	if err != nil {
		return "未知"
	}
	re := regexp.MustCompile(`level:\s*(\w+)`)
	match := re.FindStringSubmatch(string(content))
	if len(match) > 1 {
		return match[1]
	}
	return "未知"
}

// SetLogLevel 设置日志级别
func SetLogLevel(level string) error {
	content, err := os.ReadFile(ConfigPath)
	if err != nil {
		return err
	}

	re := regexp.MustCompile(`level:\s*\w+`)
	updatedContent := re.ReplaceAllString(string(content), "level: "+level)
	if err := os.WriteFile(ConfigPath, []byte(updatedContent), 0644); err != nil {
		return err
	}

	return service.RestartService()
}

// ClearLogs 清空日志文件
func ClearLogs() error {
	logFile := "/var/log/mosdns.log"
	return os.Truncate(logFile, 0)
}

// GetLogSize 获取日志文件大小
func GetLogSize() string {
	logFile := "/var/log/mosdns.log"
	info, err := os.Stat(logFile)
	if err != nil {
		return "0 B"
	}
	size := info.Size()
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}


