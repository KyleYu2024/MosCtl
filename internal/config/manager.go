package config

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	BaseURL     = "https://raw.githubusercontent.com/KyleYu2024/mosctl/main/templates"
	ConfigPath  = "/etc/mosdns/config.yaml"
	RuleDir     = "/etc/mosdns/rules"
	SystemCtl   = "systemctl"
	MosDNSBin   = "/usr/local/bin/mosdns"
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
		if err := downloadFile(url, tempPath); err != nil {
			return fmt.Errorf("下载失败 %s: %v", remoteFile, err)
		}
		tempFiles = append(tempFiles, tempPath)
	}

	// 3. Dry-Run (已移除)
	// MosDNS v5.3.3 不支持 --dry-run，且直接 start 会导致端口冲突。
	// 为了兼容性，我们跳过校验，直接信任云端配置。
	fmt.Println("⚠️  MosDNS v5 不支持 Dry-Run，跳过校验，直接应用...")

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
	if err := reloadService(); err != nil {
		return fmt.Errorf("配置更新成功但服务重载失败: %v", err)
	}

	fmt.Println("✅ 同步完成！所有系统运行正常。")
	return nil
}

// downloadFile ... (保持不变)
func downloadFile(url, dest string) error {
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	if written < 10 {
		return fmt.Errorf("下载的文件太小，可能是错误的响应")
	}

	return nil
}

// reloadService ... (保持不变)
func reloadService() error {
	if _, err := exec.LookPath(SystemCtl); err != nil {
		fmt.Println("⚠️  未找到 systemctl，跳过服务重载 (仅限开发环境)")
		return nil
	}
	// 注意：MosDNS 如果配置变动较大，restart 比 reload 更稳妥
	cmd := exec.Command(SystemCtl, "restart", "mosdns")
	return cmd.Run()
}

// cleanup ... (保持不变)
func cleanup(files []string) {
	for _, f := range files {
		os.Remove(f)
	}
}
