package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/KyleYu2024/mosctl/internal/config"
	"github.com/KyleYu2024/mosctl/internal/service"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

var dockerCmd = &cobra.Command{
	Use:   "docker",
	Short: "Run MosCtl in Docker mode",
	Run: func(cmd *cobra.Command, args []string) {
		os.Setenv("MOSCTL_MODE", "docker")
		runDockerPanel()
	},
}

func init() {
	rootCmd.AddCommand(dockerCmd)
}

func runDockerPanel() {
	fmt.Println("=====================================")
	fmt.Println("             MosCtl Docker (v0.5.0)  ")
	fmt.Println("=====================================")

	os.Setenv("MOSCTL_MODE", "docker")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. 初始化环境
	initializeDockerEnv()

	// 2. 环境变量处理
	if local := os.Getenv("LOCAL_UPSTREAM"); local != "" {
		config.SetUpstream(true, local)
	}
	if remote := os.Getenv("REMOTE_UPSTREAM"); remote != "" {
		config.SetUpstream(false, remote)
	}

	currLocal, currRemote := config.GetCurrentUpstreams()
	fmt.Printf("[%s] ⚙️  配置就绪: LOCAL=%s, REMOTE=%s\n", time.Now().Format("2006-01-02 15:04:05"), currLocal, currRemote)

	// 3. 进程管理协程
	go processManager(ctx)

	// 4. 事件驱动文件监控 (fsnotify)
	go fileWatcher(ctx)

	// 5. 定时任务 (Cron - GeoRules Update)
	go cronScheduler(ctx)

	// 6. 统计任务 (渐进式播报)
	go statsScheduler(ctx)

	// 7. 诊断
	go func() {
		time.Sleep(3 * time.Second)
		config.RunTest()
		fmt.Printf("[%s] 🚀 MosDNS 内核启动成功，分流规则已全面生效。\n", time.Now().Format("2006-01-02 15:04:05"))
		fmt.Printf("[%s] 🟢 正在按策略分流 DNS 请求，系统运行状态正常...\n", time.Now().Format("2006-01-02 15:04:05"))
	}()

	// 8. 信号捕获
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	sig := <-sigChan
	fmt.Printf("\n[%s] 📥 接收到信号 %v，准备优雅退出...\n", time.Now().Format("2006-01-02 15:04:05"), sig)
	cancel()
	
	time.Sleep(1 * time.Second)
	fmt.Println("👋 MosCtl 已安全关闭。")
}

// statsScheduler 实现渐进式播报策略
func statsScheduler(ctx context.Context) {
	initialSequence := []time.Duration{
		1 * time.Minute,
		5 * time.Minute,
		15 * time.Minute,
		1 * time.Hour,
	}

	for _, delay := range initialSequence {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
			printStats()
		}
	}

	ticker := time.NewTicker(4 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			printStats()
		}
	}
}

func printStats() {
	stats, err := config.GetCacheStats()
	if err != nil {
		return
	}
	fmt.Printf("[%s] 📊 缓存报告: %s\n", time.Now().Format("2006-01-02 15:04:05"), stats)
}

// processManager 核心进程管理逻辑
func processManager(ctx context.Context) {
	var mosdnsCmd *exec.Cmd

	for {
		select {
		case <-ctx.Done():
			if mosdnsCmd != nil && mosdnsCmd.Process != nil {
				fmt.Println("🛑 正在终止 MosDNS 进程...")
				config.SaveCurrentStatsToHistory()
				mosdnsCmd.Process.Signal(syscall.SIGTERM)
			}
			return
		default:
			fmt.Printf("[%s] 🚀 启动 MosDNS...\n", time.Now().Format("2006-01-02 15:04:05"))
			mosdnsCmd = exec.Command("/usr/local/bin/mosdns", "start", "-c", "/etc/mosdns/config.yaml")
			mosdnsCmd.Stdout = os.Stdout
			mosdnsCmd.Stderr = os.Stderr

			if err := mosdnsCmd.Start(); err != nil {
				fmt.Printf("❌ 启动失败: %v, 5秒后重试...\n", err)
				time.Sleep(5 * time.Second)
				continue
			}

			done := make(chan error, 1)
			go func() { done <- mosdnsCmd.Wait() }()

			select {
			case err := <-done:
				if ctx.Err() != nil {
					return 
				}
				fmt.Printf("[%s] ⚠️  MosDNS 进程已退出 (err: %v)，准备重启...\n", time.Now().Format("2006-01-02 15:04:05"), err)
				config.SaveCurrentStatsToHistory()
				time.Sleep(1 * time.Second)
			case <-service.DockerRestartChan:
				fmt.Printf("[%s] 🔄 收到重启信号，正在准备热重载...\n", time.Now().Format("2006-01-02 15:04:05"))
				printStats()
				if mosdnsCmd != nil && mosdnsCmd.Process != nil {
					config.SaveCurrentStatsToHistory()
					mosdnsCmd.Process.Kill()
				}
			case <-ctx.Done():
				if mosdnsCmd != nil && mosdnsCmd.Process != nil {
					config.SaveCurrentStatsToHistory()
					mosdnsCmd.Process.Signal(syscall.SIGTERM)
				}
				return
			}
		}
	}
}

// fileWatcher 监控目录变动
func fileWatcher(ctx context.Context) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Printf("❌ 无法启动监控: %v\n", err)
		return
	}
	defer watcher.Close()

	watchDirs := []string{"/etc/mosdns", "/etc/mosdns/rules"}
	for _, dir := range watchDirs {
		if err := watcher.Add(dir); err != nil {
			fmt.Printf("⚠️ 无法监控目录 %s: %v\n", dir, err)
		}
	}

	var timer *time.Timer

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			
			filename := filepath.Base(event.Name)
			isConfig := filename == "config.yaml"
			isRule := strings.HasSuffix(event.Name, ".txt") || strings.Contains(event.Name, "/rules/")

			if (isConfig || isRule) && (event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0) {
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(1*time.Second, func() {
					fmt.Printf("[%s] 📝 检测到文件变更: %s, 准备重启...\n", time.Now().Format("2006-01-02 15:04:05"), event.Name)
					service.RestartService()
				})
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			fmt.Printf("❌ 监控错误: %v\n", err)
		}
	}
}

// cronScheduler 每日定时更新 GeoRules
func cronScheduler(ctx context.Context) {
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), 2, 30, 0, 0, now.Location())
		if next.Before(now) {
			next = next.Add(24 * time.Hour)
		}
		
		timer := time.NewTimer(next.Sub(now))
		fmt.Printf("[%s] ⏰ 下次计划更新任务在: %s\n", time.Now().Format("2006-01-02 15:04:05"), next.Format("2006-01-02 15:04:05"))

		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			UpdateGeoRules()
		}
	}
}

// UpdateGeoRules 更新 GeoIP 和 GeoSite 规则
func UpdateGeoRules() {
	fmt.Println("⬇️  正在执行计划内 GeoSite/GeoIP 更新...")

	os.MkdirAll("/etc/mosdns/rules", 0755)

	ghProxy := "https://gh-proxy.com/"
	files := map[string]string{
		ghProxy + "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/direct-list.txt": "/etc/mosdns/rules/geosite_cn.txt",
		ghProxy + "https://raw.githubusercontent.com/Loyalsoldier/geoip/release/text/cn.txt":              "/etc/mosdns/rules/geoip_cn.txt",
		ghProxy + "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/apple-cn.txt":    "/etc/mosdns/rules/geosite_apple.txt",
		ghProxy + "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/proxy-list.txt":   "/etc/mosdns/rules/geosite_no_cn.txt",
	}

	anyUpdated := false
	for url, path := range files {
		updated, err := service.DownloadFile(url, path)
		if err != nil {
			fmt.Printf("⚠️  下载失败 %s: %v (将跳过该文件)\n", path, err)
		} else if updated {
			anyUpdated = true
		}
	}

	if anyUpdated {
		fmt.Println("🎉 规则文件已更新，fsnotify 将自动触发重启。")
	} else {
		fmt.Println("✅ 规则已是最新，无需更新。")
	}
}

func initializeDockerEnv() {
	if err := os.MkdirAll("/etc/mosdns/rules", 0755); err != nil {
		fmt.Printf("❌ 无法创建规则目录: %v\n", err)
	}

	if _, err := os.Stat("/etc/mosdns/config.yaml"); os.IsNotExist(err) {
		fmt.Println("📢 初始化配置模板...")
		copyFile("/usr/share/mosdns/config.yaml", "/etc/mosdns/config.yaml")
		
		files, _ := filepath.Glob("/usr/share/mosdns/rules/*.txt")
		for _, f := range files {
			copyFile(f, filepath.Join("/etc/mosdns/rules", filepath.Base(f)))
		}
	} else {
		config.EnsureMetricsServer()
		config.EnsureDefaultTTL()
	}

	requiredFiles := []string{
		"/etc/mosdns/rules/force-cn.txt",
		"/etc/mosdns/rules/force-nocn.txt",
		"/etc/mosdns/rules/user_iot.txt",
		"/etc/mosdns/rules/hosts.txt",
	}
	for _, rf := range requiredFiles {
		if _, err := os.Stat(rf); os.IsNotExist(err) {
			os.WriteFile(rf, []byte{}, 0644)
		}
	}
	fmt.Println("✅ 运行环境初始化核验完成。")
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}
