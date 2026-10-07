package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/KyleYu2024/mosctl/internal/ruleformat"
)

const (
	SystemCtl   = "systemctl"
	EnvMode     = "MOSCTL_MODE"
	ModeManaged = "managed"
)

// RestartChan requests a restart of the MosDNS child process.
var RestartChan = make(chan struct{}, 1)

// RestartRequests acknowledges only after the old child exits and its replacement starts.
type RestartRequest struct{ Done chan error }

var RestartRequests = make(chan RestartRequest)

func RestartAndWait(ctx context.Context) error {
	if !IsManagedMode() {
		return fmt.Errorf("内核更新需要 MosCtl 进程管理模式")
	}
	request := RestartRequest{Done: make(chan error, 1)}
	select {
	case RestartRequests <- request:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-request.Done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func IsManagedMode() bool {
	// 使用 strings.TrimSpace 避免潜在的格式问题
	mode := strings.TrimSpace(os.Getenv(EnvMode))
	return mode == ModeManaged || mode == "docker" // compatibility with older installations
}

// RestartService restarts the mosdns service
func RestartService() error {
	// 无论如何，优先检查环境变量
	if IsManagedMode() {
		select {
		case RestartChan <- struct{}{}:
			log.Print("🔄 已请求重新加载 MosDNS")
		default:
			// 如果已经有一个信号在等待，就不重复发送
		}
		return nil
	}

	if _, err := exec.LookPath(SystemCtl); err == nil {
		return exec.Command(SystemCtl, "restart", "mosdns").Run()
	}

	return fmt.Errorf("未启用进程管理且未找到 systemctl，无法重载 MosDNS")
}

// ReloadService reloads the mosdns service
func ReloadService() error {
	if IsManagedMode() {
		return RestartService()
	}

	if _, err := exec.LookPath(SystemCtl); err == nil {
		return exec.Command(SystemCtl, "reload", "mosdns").Run()
	}
	return fmt.Errorf("未找到 systemctl，无法重载 MosDNS")
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
	newContent, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20+1))
	if err != nil {
		return false, err
	}

	if len(newContent) > 16<<20 {
		return false, fmt.Errorf("规则文件超过 16 MB")
	}
	if len(newContent) < 100 {
		return false, fmt.Errorf("下载内容太小，可能是错误的响应")
	}

	id := "force-cn"
	if filepath.Base(dest) == "geoip_cn.txt" {
		id = "user-iot"
	}
	if err := ruleformat.Validate(id, string(newContent)); err != nil {
		return false, fmt.Errorf("下载规则校验失败: %w", err)
	}
	// 2. 读取旧内容进行对比
	oldContent, err := os.ReadFile(dest)
	if err == nil && bytes.Equal(oldContent, newContent) {
		// 内容一致，跳过写入，避免触发 fsnotify 重启
		return false, nil
	}

	// 3. 内容不一致，原子写入
	if err := AtomicWrite(dest, newContent, 0644); err != nil {
		return false, err
	}

	log.Printf("✅ 文件已更新: %s\n", filepath.Base(dest))
	return true, nil
}
