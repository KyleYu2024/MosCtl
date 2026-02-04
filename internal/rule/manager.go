package rule

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	LocalDirectPath = "/etc/mosdns/rules/local_direct.txt"
	LocalProxyPath  = "/etc/mosdns/rules/local_proxy.txt"
	SystemCtl       = "systemctl"
)

// AddRule 添加域名到指定列表
func AddRule(domain string, isDirect bool) error {
	targetPath := LocalProxyPath
	listName := "代理黑名单 (Proxy)"
	if isDirect {
		targetPath = LocalDirectPath
		listName = "直连白名单 (Direct)"
	}

	// 1. 确保文件存在 (如果不存在就创建)
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		if err := os.WriteFile(targetPath, []byte{}, 0644); err != nil {
			return fmt.Errorf("无法创建规则文件: %v", err)
		}
	}

	// 2. 查重
	exists, err := checkDomainExists(targetPath, domain)
	if err != nil {
		return fmt.Errorf("读取规则文件失败: %v", err)
	}
	if exists {
		fmt.Printf("⚠️  域名 %s 已经在 [%s] 中了，跳过添加。\n", domain, listName)
		return nil
	}

	// 3. 追加写入
	f, err := os.OpenFile(targetPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.WriteString(domain + "\n"); err != nil {
		return err
	}

	fmt.Printf("✅ 已将 %s 添加到 [%s]\n", domain, listName)

	// 4. 重载生效
	fmt.Println("🔄 正在重载 MosDNS...")
	if err := reloadService(); err != nil {
		return fmt.Errorf("规则写入成功但服务重载失败: %v", err)
	}
	fmt.Println("🎉 规则已立即生效！")

	return nil
}

// checkDomainExists 简单的全文件扫描查重
func checkDomainExists(path, domain string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// 忽略空行和注释
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 简单匹配 (精确匹配)
		// 如果你想做模糊匹配(比如包含关系)，可以在这里改，但精确匹配最稳
		if line == domain {
			return true, nil
		}
	}
	return false, scanner.Err()
}

// reloadService 重载 MosDNS
func reloadService() error {
	// Mac 开发环境跳过
	if _, err := exec.LookPath(SystemCtl); err != nil {
		fmt.Println("⚠️  未找到 systemctl，跳过重载 (仅限开发环境)")
		return nil
	}
	return exec.Command(SystemCtl, "reload", "mosdns").Run()
}
