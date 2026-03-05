package service

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"
)

const SystemCtl = "systemctl"

// RestartService restarts the mosdns service
func RestartService() error {
	if _, err := exec.LookPath(SystemCtl); err != nil {
		fmt.Println("⚠️  未找到 systemctl，跳过服务重载 (仅限开发环境)")
		return nil
	}
	return exec.Command(SystemCtl, "restart", "mosdns").Run()
}

// ReloadService reloads the mosdns service
func ReloadService() error {
	if _, err := exec.LookPath(SystemCtl); err != nil {
		return nil
	}
	return exec.Command(SystemCtl, "reload", "mosdns").Run()
}

// DownloadFile downloads a file from URL to dest
func DownloadFile(url, dest string) error {
	client := http.Client{Timeout: 30 * time.Second}
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