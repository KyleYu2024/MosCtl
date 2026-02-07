package rule

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/KyleYu2024/mosctl/internal/service"
)

const (
	LocalDirectPath = "/etc/mosdns/rules/local_direct.txt"
	LocalProxyPath  = "/etc/mosdns/rules/local_proxy.txt"
	LocalIotPath    = "/etc/mosdns/rules/user_iot.txt" // 新增常量
)

// AddRule 添加域名或 IP 到指定列表
func AddRule(content string, isDirect bool, isIot bool) error {
	targetPath := LocalProxyPath
	listName := "代理黑名单 (Proxy)"

	// 验证输入类型
	isIP := net.ParseIP(content) != nil
	_, _, errCIDR := net.ParseCIDR(content)
	isNetwork := errCIDR == nil

	if isIot {
		if !isIP && !isNetwork {
			return fmt.Errorf("智能家居 (IoT) 规则仅支持 IP 或 CIDR (例如: 192.168.1.10 或 192.168.1.0/24)")
		}
		targetPath = LocalIotPath
		listName = "智能家居直连 (IoT)"
	} else {
		if isIP || isNetwork {
			return fmt.Errorf("直连 (Direct) 或 代理 (Proxy) 规则目前仅支持域名。IP 规则请使用 --iot 或修改配置文件。")
		}
		if isDirect {
			targetPath = LocalDirectPath
			listName = "直连白名单 (Direct)"
		}
	}

	// 1. 确保文件存在
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		if err := os.MkdirAll("/etc/mosdns/rules", 0755); err != nil {
			return fmt.Errorf("无法创建目录: %v", err)
		}
		if err := os.WriteFile(targetPath, []byte{}, 0644); err != nil {
			return fmt.Errorf("无法创建规则文件: %v", err)
		}
	}

	// 2. 查重
	exists, err := checkContentExists(targetPath, content)
	if err != nil {
		return fmt.Errorf("读取规则文件失败: %v", err)
	}
	if exists {
		fmt.Printf("⚠️  内容 %s 已经在 [%s] 中了，跳过添加。\n", content, listName)
		return nil
	}

	// 3. 追加写入
	f, err := os.OpenFile(targetPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.WriteString(content + "\n"); err != nil {
		return err
	}

	fmt.Printf("✅ 已将 %s 添加到 [%s]\n", content, listName)

	// 4. 重载生效
	if err := service.ReloadService(); err != nil {
		fmt.Printf("⚠️ 规则已写入但重载服务失败 (可能非 Linux 环境): %v\n", err)
	} else {
		fmt.Println("🎉 规则已立即生效！")
	}

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
		if line == "" || strings.HasPrefix(line, "#") { continue }
		if line == content { return true, nil }
	}
	return false, scanner.Err()
}
