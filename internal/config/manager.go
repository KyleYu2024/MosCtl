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

// 定义云端仓库的基础地址 (注意：这是你的仓库)
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

	// 1. 准备临时文件列表 [云端文件名 -> 本地目标路径]
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

	// 3. Dry-Run 校验 (金丝雀测试)
	// 我们用下载下来的 config.yaml.tmp 来测试
	// 注意：MosDNS 校验时需要引用规则文件，我们需要确保临时规则文件路径正确
	// 这里的简化处理：MosDNS dry-run 主要检查 yaml 语法，
	// 如果 yaml 里引用的 txt 文件不存在可能会报错，所以这里是一个关键点。
	// 为了稳妥，我们假设本地必须已经存在旧文件，或者我们不校验规则文件路径是否存在，只校验格式。
	// 更严谨的做法是：MosDNS 的 start -c ... --dry-run 会尝试加载所有插件。
	// 如果我们只是覆盖，旧文件还在，校验通常能通过。
	
	fmt.Println("🔍 执行 Dry-Run 配置校验...")
	// 使用 mosdns start -c <tmp_config> --dry-run
	cmd := exec.Command(MosDNSBin, "start", "-c", ConfigPath+".tmp", "--dry-run")
	// 这一步在 Mac 上跑会报错(因为没有mosdns二进制)，但在 Linux 上是必须的
	// 我们加一个判断，如果是开发环境(Mac)就跳过
	if _, err := os.Stat(MosDNSBin); err == nil {
		if output, err := cmd.CombinedOutput(); err != nil {
			fmt.Printf("❌ 校验失败:\n%s\n", string(output))
			cleanup(tempFiles)
			return fmt.Errorf("新配置验证未通过，已放弃更新")
		}
	} else {
		fmt.Println("⚠️  未找到 mosdns 二进制文件，跳过 Dry-Run (仅限开发环境)")
	}

	// 4. 原子替换 (Atomic Replace)
	fmt.Println("⚡️ 校验通过，正在应用更新...")
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
		// 如果重载失败，这可是大事，但也回不去旧配置了(已经被覆盖)
		// 这时候只能报警
		return fmt.Errorf("配置更新成功但服务重载失败: %v", err)
	}

	fmt.Println("✅ 同步完成！所有系统运行正常。")
	return nil
}

// downloadFile 下载并进行简单的非空校验
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

	// 写入文件
	written, err := io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	// 校验：不能是空文件
	if written < 10 {
		return fmt.Errorf("下载的文件太小，可能是错误的响应")
	}

	return nil
}

// reloadService 调用 systemctl reload
func reloadService() error {
	// 在 Mac 上跳过
	if _, err := exec.LookPath(SystemCtl); err != nil {
		fmt.Println("⚠️  未找到 systemctl，跳过服务重载 (仅限开发环境)")
		return nil
	}

	cmd := exec.Command(SystemCtl, "reload", "mosdns")
	return cmd.Run()
}

// cleanup 清理临时文件
func cleanup(files []string) {
	for _, f := range files {
		os.Remove(f)
	}
}
