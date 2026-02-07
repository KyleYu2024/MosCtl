package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/KyleYu2024/mosctl/internal/service"
)

const (
	BaseURL    = "https://raw.githubusercontent.com/KyleYu2024/MosCtl/main/templates"
	ConfigPath = "/etc/mosdns/config.yaml"
	RuleDir    = "/etc/mosdns/rules"
	MosDNSBin  = "/usr/local/bin/mosdns"
)

// SyncConfig 执行完整的同步流程
func SyncConfig() error {
	fmt.Println("🔄 开始同步云端配置...")

	// 1. 准备临时文件列表
	filesToSync := map[string]string{
		"config.yaml":      ConfigPath,
		"cloud_direct.txt": filepath.Join(RuleDir, "cloud_direct.txt"),
		"cloud_proxy.txt":  filepath.Join(RuleDir, "cloud_proxy.txt"),
	}

	// 2. 下载所有文件到临时位置 (.tmp)
	tempFiles := make([]string, 0)
	for remoteFile, localPath := range filesToSync {
		url := fmt.Sprintf("%s/%s", BaseURL, remoteFile)
		tempPath := localPath + ".tmp"

		fmt.Printf("⬇️  正在下载: %s ...\n", remoteFile)
		if err := service.DownloadFile(url, tempPath); err != nil {
			return fmt.Errorf("下载失败 %s: %v", remoteFile, err)
		}
		tempFiles = append(tempFiles, tempPath)
	}

	// 3. 处理 config.yaml 变量保留
	if err := preserveConfigVariables(ConfigPath + ".tmp"); err != nil {
		fmt.Printf("⚠️  无法保留旧配置变量: %v\n", err)
	}

	// 4. 原子替换 (Atomic Replace)
	fmt.Println("⚡️ 正在应用更新...")
	for _, tmpPath := range tempFiles {
		finalPath := tmpPath[:len(tmpPath)-4] // 去掉 .tmp
		if err := os.Rename(tmpPath, finalPath); err != nil {
			cleanup(tempFiles)
			return fmt.Errorf("无法应用文件 %s: %v", finalPath, err)
		}
	}

	// 5. 重载服务
	fmt.Println("🔄 重载 MosDNS 服务...")
	if err := service.RestartService(); err != nil {
		return fmt.Errorf("配置更新成功但服务重载失败: %v", err)
	}

	fmt.Println("✅ 同步完成！所有系统运行正常。")
	return nil
}

func preserveConfigVariables(newConfigTmp string) error {
	if _, err := os.Stat(ConfigPath); os.IsNotExist(err) {
		return nil // 没有旧配置，跳过
	}

	oldContent, err := os.ReadFile(ConfigPath)
	if err != nil {
		return err
	}

	newContent, err := os.ReadFile(newConfigTmp)
	if err != nil {
		return err
	}

	// 提取旧值
	ttlRegex := regexp.MustCompile(`lazy_cache_ttl:\s*(\d+)`)
	localDnsRegex := regexp.MustCompile(`addr:\s*"([^"]+)"\s*#\s*TAG_LOCAL`)
	remoteDnsRegex := regexp.MustCompile(`addr:\s*"([^"]+)"\s*#\s*TAG_REMOTE`)

	ttlMatch := ttlRegex.FindStringSubmatch(string(oldContent))
	localMatch := localDnsRegex.FindStringSubmatch(string(oldContent))
	remoteMatch := remoteDnsRegex.FindStringSubmatch(string(oldContent))

	updatedContent := string(newContent)

	if len(ttlMatch) > 1 {
		re := regexp.MustCompile(`lazy_cache_ttl:\s*\d+`)
		updatedContent = re.ReplaceAllString(updatedContent, "lazy_cache_ttl: "+ttlMatch[1])
	}
	if len(localMatch) > 1 {
		re := regexp.MustCompile(`addr:\s*"[^"]+"\s*#\s*TAG_LOCAL`)
		updatedContent = re.ReplaceAllString(updatedContent, fmt.Sprintf(`addr: "%s" # TAG_LOCAL`, localMatch[1]))
	}
	if len(remoteMatch) > 1 {
		re := regexp.MustCompile(`addr:\s*"[^"]+"\s*#\s*TAG_REMOTE`)
		updatedContent = re.ReplaceAllString(updatedContent, fmt.Sprintf(`addr: "%s" # TAG_REMOTE`, remoteMatch[1]))
	}

	return os.WriteFile(newConfigTmp, []byte(updatedContent), 0644)
}

func SetUpstream(isLocal bool, addr string) error {
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

// cleanup ...
func cleanup(files []string) {
	for _, f := range files {
		os.Remove(f)
	}
}
