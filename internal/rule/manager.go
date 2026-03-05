package rule

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/KyleYu2024/mosctl/internal/service"
)

// RuleType 定义规则类型枚举
type RuleType int

const (
	TypeForceCN RuleType = iota
	TypeForceNoCN
	TypeIoT
)

// 对应 config.yaml 中的文件路径
const (
	PathForceCN   = "/etc/mosdns/rules/force-cn.txt"   // 强制国内
	PathForceNoCN = "/etc/mosdns/rules/force-nocn.txt" // 强制国外
	PathIoT       = "/etc/mosdns/rules/user_iot.txt"   // 智能家居
)

// AddRule 添加规则
func AddRule(content string, rType RuleType) error {
	var targetPath, listName string

	// 1. 基础校验与路径选择
	isIP := net.ParseIP(content) != nil
	_, _, errCIDR := net.ParseCIDR(content)
	isNetwork := errCIDR == nil

	switch rType {
	case TypeIoT:
		if !isIP && !isNetwork {
			return fmt.Errorf("智能家居 (IoT) 规则仅支持 IP 或 CIDR (例如: 192.168.1.10 或 192.168.1.0/24)")
		}
		targetPath = PathIoT
		listName = "智能家居直连 (IoT)"

	case TypeForceCN:
		if isIP || isNetwork {
			return fmt.Errorf("强制国内规则仅支持域名 (MosDNS domain_set 不支持 IP)")
		}
		targetPath = PathForceCN
		listName = "强制国内 (Force CN)"

	case TypeForceNoCN:
		if isIP || isNetwork {
			return fmt.Errorf("强制国外规则仅支持域名 (MosDNS domain_set 不支持 IP)")
		}
		targetPath = PathForceNoCN
		listName = "强制国外 (Force NoCN)"
	}

	// 2. 确保文件和目录存在
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		if err := os.MkdirAll("/etc/mosdns/rules", 0755); err != nil {
			return fmt.Errorf("无法创建目录: %v", err)
		}
		if err := os.WriteFile(targetPath, []byte{}, 0644); err != nil {
			return fmt.Errorf("无法创建规则文件: %v", err)
		}
	}

	// 3. 查重
	exists, err := checkContentExists(targetPath, content)
	if err != nil {
		return fmt.Errorf("读取规则文件失败: %v", err)
	}
	if exists {
		fmt.Printf("⚠️  内容 %s 已经在 [%s] 中了，跳过添加。\n", content, listName)
		return nil
	}

	// 4. 追加写入
	f, err := os.OpenFile(targetPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.WriteString(content + "\n"); err != nil {
		return err
	}

	fmt.Printf("✅ 已将 %s 添加到 [%s]\n", content, listName)

	// 5. 重启生效 (使用 Restart 避免 Systemd Reload 报错)
	fmt.Println("🔄 正在重载服务以生效规则...")
	if err := service.RestartService(); err != nil {
		fmt.Printf("❌ 规则已写入但服务重启失败: %v\n", err)
		return err
	}

	fmt.Println("🎉 服务重载成功，规则已生效！")
	return nil
}

func checkContentExists(path, content string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == content {
			return true, nil
		}
	}
	return false, scanner.Err()
}
