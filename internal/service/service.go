package service

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	SystemCtl = "systemctl"
	EnvMode   = "MOSCTL_MODE"
	ModeDocker = "docker"
)

// DockerRestartChan 用于 Docker 模式下的重启信号
var DockerRestartChan = make(chan struct{}, 1)

// IsDockerMode 返回当前是否处于 Docker 模式
func IsDockerMode() bool {
	// 使用 strings.TrimSpace 避免潜在的格式问题
	mode := strings.TrimSpace(os.Getenv(EnvMode))
	return mode == ModeDocker
}

// RestartService restarts the mosdns service
func RestartService() error {
	// 无论如何，优先检查环境变量
	if IsDockerMode() {
		select {
		case DockerRestartChan <- struct{}{}:
			fmt.Println("🔄 Docker 模式: 已发送重启信号")
		default:
			// 如果已经有一个信号在等待，就不重复发送
		}
		return nil
	}

	if _, err := exec.LookPath(SystemCtl); err == nil {
		return exec.Command(SystemCtl, "restart", "mosdns").Run()
	}
	
	fmt.Printf("⚠️  未找到 systemctl 且非 Docker 模式 (MODE=%q), 跳过服务重启\n", os.Getenv(EnvMode))
	return nil
}

// ReloadService reloads the mosdns service
func ReloadService() error {
	if IsDockerMode() {
		return RestartService()
	}

	if _, err := exec.LookPath(SystemCtl); err == nil {
		return exec.Command(SystemCtl, "reload", "mosdns").Run()
	}
	return nil
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
