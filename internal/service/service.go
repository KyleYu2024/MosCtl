package service

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	SystemCtl  = "systemctl"
	EnvMode    = "MOSCTL_MODE"
	ModeDocker = "docker"
)

// DockerRestartChan 用于 Docker 模式下的重启信号
var DockerRestartChan = make(chan struct{}, 1)

var (
	dockerRestartMu   sync.Mutex
	lastDockerRestart time.Time
)

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
		dockerRestartMu.Lock()
		if time.Since(lastDockerRestart) < 1500*time.Millisecond {
			dockerRestartMu.Unlock()
			return nil
		}
		lastDockerRestart = time.Now()
		dockerRestartMu.Unlock()
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

// DownloadFile downloads a file from URL to dest, only if content is different.
// Returns (true, nil) if file was updated, (false, nil) if content is same.
func DownloadFile(url, dest string) (bool, error) {
	client := http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return false, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// 1. 读取新内容到内存
	newContent, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	if len(newContent) < 100 {
		return false, fmt.Errorf("下载内容太小，可能是错误的响应")
	}

	// 2. 读取旧内容进行对比
	oldContent, err := os.ReadFile(dest)
	if err == nil && bytes.Equal(oldContent, newContent) {
		// 内容一致，跳过写入，避免触发 fsnotify 重启
		return false, nil
	}

	// 3. 内容不一致，原子写入
	tmpDest := dest + ".tmp"
	if err := os.WriteFile(tmpDest, newContent, 0644); err != nil {
		return false, err
	}

	// 原子替换
	if err := os.Rename(tmpDest, dest); err != nil {
		os.Remove(tmpDest)
		return false, err
	}

	fmt.Printf("✅ 文件已更新: %s\n", filepath.Base(dest))
	return true, nil
}
