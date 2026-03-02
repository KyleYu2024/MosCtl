package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/KyleYu2024/mosctl/internal/service"
	"gopkg.in/yaml.v3"
)

const (
	ConfigPath     = "/etc/mosdns/config.yaml"
	RuleDir        = "/etc/mosdns/rules"
	MosDNSBin      = "/usr/local/bin/mosdns"
	StatsHistoryPath = "/etc/mosdns/stats_history.json"
)

// PersistentStats 存储历史累加数据
type PersistentStats struct {
	QueryTotal float64 `json:"query_total"`
	HitTotal   float64 `json:"hit_total"`
	MissTotal  float64 `json:"miss_total"`
	LazyTotal  float64 `json:"lazy_total"`
	UpdatedAt  int64   `json:"updated_at"`
}

// GetCacheStats 获取缓存统计信息 (包含历史累加)
func GetCacheStats() (string, error) {
	resp, err := http.Get("http://127.0.0.1:8080/metrics")
	if err != nil {
		return "", fmt.Errorf("无法连接到指标服务器: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取指标数据失败: %v", err)
	}

	metrics := string(body)
	q_curr, _ := strconv.ParseFloat(findMetric(metrics, "mosdns_cache_query_total"), 64)
	h_curr, _ := strconv.ParseFloat(findMetric(metrics, "mosdns_cache_hit_total"), 64)
	m_curr, _ := strconv.ParseFloat(findMetric(metrics, "mosdns_cache_miss_total"), 64)
	l_curr, _ := strconv.ParseFloat(findMetric(metrics, "mosdns_cache_lazy_hit_total"), 64)

	// 读取历史数据
	hist := loadHistory()

	// 最终显示值 = 当前活跃值 + 历史值
	q_total := q_curr + hist.QueryTotal
	h_total := h_curr + hist.HitTotal
	m_total := m_curr + hist.MissTotal
	l_total := l_curr + hist.LazyTotal

	if q_total == 0 && h_total == 0 && m_total == 0 {
		return "暂无缓存数据 (统计可能尚未开始)", nil
	}

	// 计算分母
	div := q_total
	if div == 0 {
		div = h_total + m_total
	}

	rate := 0.0
	if div > 0 {
		rate = (h_total / div) * 100
	}

	// 构造详细输出
	res := fmt.Sprintf("命中: %.0f", h_total)
	if l_total > 0 {
		res += fmt.Sprintf(" (含乐观命中: %.0f)", l_total)
	}
	
	if q_total > 0 {
		res += fmt.Sprintf(" | 总请求: %.0f", q_total)
	} else {
		res += fmt.Sprintf(" | 未命中: %.0f", m_total)
	}
	
	res += fmt.Sprintf(" | 命中率: %.2f%%", rate)
	return res, nil
}

// SaveCurrentStatsToHistory 在重启前保存当前活跃统计到历史记录
func SaveCurrentStatsToHistory() {
	resp, err := http.Get("http://127.0.0.1:8080/metrics")
	if err != nil {
		return // 可能是进程已经退出了，直接放弃
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	metrics := string(body)

	q, _ := strconv.ParseFloat(findMetric(metrics, "mosdns_cache_query_total"), 64)
	h, _ := strconv.ParseFloat(findMetric(metrics, "mosdns_cache_hit_total"), 64)
	m, _ := strconv.ParseFloat(findMetric(metrics, "mosdns_cache_miss_total"), 64)
	l, _ := strconv.ParseFloat(findMetric(metrics, "mosdns_cache_lazy_hit_total"), 64)

	if q == 0 && h == 0 && m == 0 {
		return // 没有产生任何新统计，无需保存
	}

	hist := loadHistory()
	hist.QueryTotal += q
	hist.HitTotal += h
	hist.MissTotal += m
	hist.LazyTotal += l
	hist.UpdatedAt = time.Now().Unix()

	saveHistory(hist)
}

func loadHistory() PersistentStats {
	var hist PersistentStats
	data, err := os.ReadFile(StatsHistoryPath)
	if err == nil {
		json.Unmarshal(data, &hist)
	}
	return hist
}

func saveHistory(hist PersistentStats) error {
	data, err := json.MarshalIndent(hist, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(StatsHistoryPath, data, 0644)
}

func ClearHistory() {
	os.Remove(StatsHistoryPath)
}

func findMetric(metrics, name string) string {
	// 匹配带标签的情况: name{...} value
	// 增加对科学计数法的支持: [0-9.]+(?:[eE][+-]?[0-9]+)?
	re := regexp.MustCompile(name + `\{.*?\}\s+([0-9.]+(?:[eE][+-]?[0-9]+)?)`)
	match := re.FindStringSubmatch(metrics)
	if len(match) > 1 {
		return match[1]
	}
	// 匹配不带标签的情况: name value
	re = regexp.MustCompile(name + `\s+([0-9.]+(?:[eE][+-]?[0-9]+)?)`)
	match = re.FindStringSubmatch(metrics)
	if len(match) > 1 {
		return match[1]
	}
	return "0"
}

// 修改配置文件的通用辅助函数 (不触发重启，且原子化写入)
func updateConfigSilent(fn func(root *yaml.Node) error) error {
	data, err := os.ReadFile(ConfigPath)
	if err != nil {
		return err
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return err
	}

	if err := fn(&root); err != nil {
		return err
	}

	updatedData, err := yaml.Marshal(&root)
	if err != nil {
		return err
	}

	// 核心优化：对比内容，如果一致则不写入，避免触发 fsnotify 重启
	if bytes.Equal(data, updatedData) {
		return nil
	}

	tmpFile := ConfigPath + ".tmp"
	if err := os.WriteFile(tmpFile, updatedData, 0644); err != nil {
		return err
	}
	
	// 原子替换
	return os.Rename(tmpFile, ConfigPath)
}

// EnsureMetricsServer 确保配置中包含指标监控服务器 (v5 API 模式，并清理旧版错误插件)
func EnsureMetricsServer() error {
	return updateConfigSilent(func(root *yaml.Node) error {
		if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
			return fmt.Errorf("配置文件内容无效")
		}
		
		doc := root.Content[0]
		
		// 1. 查找并删除 plugins 列表中的旧版错误插件 (prometheus_metrics, metrics_server)
		for i := 0; i < len(doc.Content); i += 2 {
			if doc.Content[i].Value == "plugins" {
				pluginsNode := doc.Content[i+1]
				if pluginsNode.Kind == yaml.SequenceNode {
					newPlugins := make([]*yaml.Node, 0, len(pluginsNode.Content))
					for _, p := range pluginsNode.Content {
						tagNode := findValueNode(p, "tag")
						if tagNode != nil && (tagNode.Value == "prometheus_metrics" || tagNode.Value == "metrics_server") {
							fmt.Printf("🧹 正在从 plugins 中清理旧的指标配置: %s\n", tagNode.Value)
							continue // 跳过，不添加到新列表中
						}
						newPlugins = append(newPlugins, p)
					}
					pluginsNode.Content = newPlugins
				}
				break
			}
		}

		// 2. 检查是否已经存在 api 顶级配置
		hasAPI := false
		for i := 0; i < len(doc.Content); i += 2 {
			if doc.Content[i].Value == "api" {
				hasAPI = true
				break
			}
		}

		if hasAPI {
			return nil
		}

		fmt.Println("📢 正在为现有配置启用 HTTP API (用于统计)...")

		// 3. 创建 api: { http: "127.0.0.1:8080" } 节点
		apiValue := &yaml.Node{
			Kind: yaml.MappingNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "http"},
				{Kind: yaml.ScalarNode, Value: "127.0.0.1:8080"},
			},
		}

		// 4. 将 api 插入到 doc.Content 的开头
		newContent := make([]*yaml.Node, 0, len(doc.Content)+2)
		newContent = append(newContent, &yaml.Node{Kind: yaml.ScalarNode, Value: "api"}, apiValue)
		newContent = append(newContent, doc.Content...)
		
		doc.Content = newContent
		return nil
	})
}

// 修改配置文件的通用辅助函数
func updateConfig(fn func(root *yaml.Node) error) error {
	data, err := os.ReadFile(ConfigPath)
	if err != nil {
		return err
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return err
	}

	if err := fn(&root); err != nil {
		return err
	}

	updatedData, err := yaml.Marshal(&root)
	if err != nil {
		return err
	}

	// 核心优化：如果内容一致，直接返回，不触发重启
	if bytes.Equal(data, updatedData) {
		return nil
	}

	tmpFile := ConfigPath + ".tmp"
	if err := os.WriteFile(tmpFile, updatedData, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmpFile, ConfigPath); err != nil {
		return err
	}

	return service.RestartService()
}

// 递归查找键名并返回其值的节点
func findValueNode(node *yaml.Node, keyName string) *yaml.Node {
	if node.Kind == yaml.DocumentNode {
		for _, content := range node.Content {
			if n := findValueNode(content, keyName); n != nil {
				return n
			}
		}
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			val := node.Content[i+1]
			if key.Value == keyName {
				return val
			}
			if n := findValueNode(val, keyName); n != nil {
				return n
			}
		}
	}
	if node.Kind == yaml.SequenceNode {
		for _, item := range node.Content {
			if n := findValueNode(item, keyName); n != nil {
				return n
			}
		}
	}
	return nil
}

func SetUpstream(isLocal bool, addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return fmt.Errorf("地址不能为空")
	}

	if !strings.Contains(addr, "://") && !strings.HasPrefix(addr, "/") {
		addr = "udp://" + addr
	}

	tag := "TAG_REMOTE"
	if isLocal {
		tag = "TAG_LOCAL"
	}

	return updateConfig(func(root *yaml.Node) error {
		// 查找包含指定注释标记的 addr 节点
		addrNode := findAddrNodeByComment(root, tag)
		if addrNode == nil {
			return fmt.Errorf("找不到带有标记 # %s 的上游配置", tag)
		}
		addrNode.Value = addr
		return nil
	})
}

func findAddrNodeByComment(node *yaml.Node, tag string) *yaml.Node {
	re := regexp.MustCompile(`(?i)#\s*` + tag)

	if node.Kind == yaml.DocumentNode {
		for _, content := range node.Content {
			if n := findAddrNodeByComment(content, tag); n != nil {
				return n
			}
		}
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			val := node.Content[i+1]
			if key.Value == "addr" && (re.MatchString(key.LineComment) || re.MatchString(val.LineComment)) {
				return val
			}
			if n := findAddrNodeByComment(val, tag); n != nil {
				return n
			}
		}
	}
	if node.Kind == yaml.SequenceNode {
		for _, item := range node.Content {
			if n := findAddrNodeByComment(item, tag); n != nil {
				return n
			}
		}
	}
	return nil
}

// EnsureDefaultTTL 确保缓存 TTL 为 86400 (24小时)
func EnsureDefaultTTL() error {
	return updateConfigSilent(func(root *yaml.Node) error {
		node := findValueNode(root, "lazy_cache_ttl")
		if node != nil {
			node.Value = "86400"
		}
		return nil
	})
}

func SetCacheTTL(ttl string) error {
	return updateConfig(func(root *yaml.Node) error {
		node := findValueNode(root, "lazy_cache_ttl")
		if node == nil {
			return fmt.Errorf("找不到 lazy_cache_ttl 配置项")
		}
		node.Value = ttl
		return nil
	})
}

func FlushCache() error {
	fmt.Println("🧹 正在清空 DNS 缓存...")
	os.Remove("/etc/mosdns/cache.dump")
	ClearHistory()
	return service.RestartService()
}

func GetCurrentUpstreams() (string, string) {
	data, err := os.ReadFile(ConfigPath)
	if err != nil {
		return "未知", "未知"
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return "未知", "未知"
	}

	local := "未知"
	remote := "未知"

	if n := findAddrNodeByComment(&root, "TAG_LOCAL"); n != nil {
		local = n.Value
	}
	if n := findAddrNodeByComment(&root, "TAG_REMOTE"); n != nil {
		remote = n.Value
	}

	return local, remote
}

func GetCurrentTTL() string {
	data, err := os.ReadFile(ConfigPath)
	if err != nil {
		return "未知"
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return "未知"
	}
	if n := findValueNode(&root, "lazy_cache_ttl"); n != nil {
		return n.Value
	}
	return "未知"
}

func GetLogLevel() string {
	data, err := os.ReadFile(ConfigPath)
	if err != nil {
		return "未知"
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return "未知"
	}
	
	// 在 log 块下寻找 level
	logNode := findValueNode(&root, "log")
	if logNode != nil {
		if levelNode := findValueNode(logNode, "level"); levelNode != nil {
			return levelNode.Value
		}
	}
	return "未知"
}

func SetLogLevel(level string) error {
	return updateConfig(func(root *yaml.Node) error {
		logNode := findValueNode(root, "log")
		if logNode == nil {
			return fmt.Errorf("找不到 log 配置块")
		}
		levelNode := findValueNode(logNode, "level")
		if levelNode == nil {
			return fmt.Errorf("找不到 level 配置项")
		}
		levelNode.Value = level
		return nil
	})
}

func RunTest() {
	fmt.Println("\n🩺 启动后连通性诊断...")
	
	testDomain := func(domain, label string) {
		fmt.Printf("  Testing %s (%s) ... ", label, domain)
		
		cmd := exec.Command("nslookup", domain, "127.0.0.1")
		start := time.Now()
		output, err := cmd.CombinedOutput()
		duration := time.Since(start)

		if err == nil {
			fmt.Printf("✅ Pass (%v)", duration.Round(time.Millisecond))
			lines := strings.Split(string(output), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "Address:") && !strings.Contains(line, "#53") && !strings.Contains(line, "127.0.0.1") {
					fmt.Printf(" -> %s\n", strings.TrimSpace(strings.TrimPrefix(line, "Address:")))
					break
				}
			}
		} else {
			fmt.Printf("❌ Failed\n")
		}
	}

	testDomain("www.baidu.com", "🇨🇳 国内")
	testDomain("www.google.com", "🌍 国外")
	fmt.Println("🎉 解析诊断完成。")
}

func RestartViaKill() error {
	return service.RestartService()
}

func ClearLogs() error {
	logFile := "/var/log/mosdns.log"
	return os.Truncate(logFile, 0)
}

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
