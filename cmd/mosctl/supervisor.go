package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/KyleYu2024/mosctl/internal/config"
	"github.com/KyleYu2024/mosctl/internal/diagnostics"
	"github.com/KyleYu2024/mosctl/internal/kernel"
	"github.com/KyleYu2024/mosctl/internal/service"
	webui "github.com/KyleYu2024/mosctl/internal/web"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

var dockerCmd = &cobra.Command{
	Use:    "docker",
	Short:  "Compatibility alias for managed MosCtl",
	Hidden: true,
	Run: func(cmd *cobra.Command, args []string) {
		os.Setenv(service.EnvMode, service.ModeManaged)
		runSupervisor()
	},
}

func init() {
	rootCmd.AddCommand(dockerCmd)
}

func runSupervisor() {
	os.Setenv(service.EnvMode, service.ModeManaged)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := kernel.Recover(config.MosDNSBin); err != nil {
		log.Printf("❌ 未完成的内核更新恢复失败: %v\n", err)
		return
	}

	// 1. 初始化环境
	initializeEnv()
	statsDone := diagnostics.QueryStats.Start(ctx, "/etc/mosdns/query_stats.json")
	if err := config.EnableQueryStats(); err != nil {
		log.Printf("⚠️ 查询统计未启用: %v\n", err)
	} else {
		diagnostics.QueryStats.SetEnabled(true)
	}

	// 2. 环境变量处理
	if local := os.Getenv("LOCAL_UPSTREAM"); local != "" {
		config.SetUpstream(true, local)
	}
	if remote := os.Getenv("REMOTE_UPSTREAM"); remote != "" && !config.RemoteManagedByWeb() {
		config.SetUpstream(false, remote)
	}

	currLocal, currRemote := config.GetCurrentUpstreams()
	fmt.Printf("[%s] ⚙️  配置就绪: LOCAL=%s, REMOTE=%s\n", time.Now().Format("2006-01-02 15:04:05"), currLocal, currRemote)

	// 3. 进程管理协程
	processDone := make(chan struct{})
	go func() { defer close(processDone); processManager(ctx) }()

	// 4. 事件驱动文件监控 (fsnotify)
	go fileWatcher(ctx)

	// 5. 定时任务 (Cron - GeoRules Update)
	go cronScheduler(ctx)

	// 6. Web 规则管理页（未配置登录凭据时不启动）
	go webui.Run(ctx)

	// 7. 统计任务 (渐进式播报)
	go statsScheduler(ctx)

	// 9. 信号捕获
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	sig := <-sigChan
	fmt.Printf("\n[%s] 📥 接收到信号 %v，准备优雅退出...\n", time.Now().Format("2006-01-02 15:04:05"), sig)
	cancel()

	<-processDone
	<-statsDone
	diagnostics.QueryStats.Flush("/etc/mosdns/query_stats.json")
	log.Print("👋 MosCtl 已安全关闭。")
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
	var acknowledgement chan error
	defer func() {
		if acknowledgement != nil {
			acknowledgement <- context.Canceled
		}
	}()
	for ctx.Err() == nil {
		fmt.Printf("[%s] 🚀 启动 MosDNS...\n", time.Now().Format("2006-01-02 15:04:05"))
		child := exec.Command(config.MosDNSBin, "start", "-c", config.ConfigPath)
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			if acknowledgement != nil {
				acknowledgement <- err
				acknowledgement = nil
			}
			log.Printf("❌ 启动失败: %v，5 秒后重试\n", err)
			if !waitForRetry(ctx, 5*time.Second) {
				return
			}
			continue
		}
		if acknowledgement != nil {
			acknowledgement <- nil
			acknowledgement = nil
		}
		done := make(chan error, 1)
		go func() { done <- child.Wait() }()
		childCtx, cancel := context.WithCancel(ctx)
		go func() {
			if !waitForRetry(childCtx, time.Second) {
				return
			}
			healthy := config.RunTest(childCtx)
			if childCtx.Err() != nil {
				return
			}
			if healthy {
				fmt.Printf("[%s] 🟢 MosDNS DNS 解析检查通过。\n", time.Now().Format("2006-01-02 15:04:05"))
			} else {
				fmt.Printf("[%s] ⚠️ DNS 解析检查未全部通过，请检查上游与网络。\n", time.Now().Format("2006-01-02 15:04:05"))
			}
		}()
		select {
		case err := <-done:
			cancel()
			fmt.Printf("[%s] ⚠️ MosDNS 已退出（%v），稍后重试。\n", time.Now().Format("2006-01-02 15:04:05"), err)
			if !waitForRetry(ctx, time.Second) {
				return
			}
		case request := <-service.RestartRequests:
			cancel()
			config.SaveCurrentStatsToHistory()
			stopChild(child, done)
			acknowledgement = request.Done
		case <-service.RestartChan:
			cancel()
			config.SaveCurrentStatsToHistory()
			fmt.Printf("[%s] 🔄 正在重新加载 MosDNS...\n", time.Now().Format("2006-01-02 15:04:05"))
			stopChild(child, done)
		case <-ctx.Done():
			cancel()
			config.SaveCurrentStatsToHistory()
			stopChild(child, done)
			return
		}
	}
}

func waitForRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func stopChild(child *exec.Cmd, done <-chan error) {
	_ = child.Process.Signal(syscall.SIGTERM)
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		log.Print("⚠️ MosDNS 退出超时，强制结束旧进程。")
		_ = child.Process.Kill()
		<-done
	}
}

// fileWatcher 监控目录变动
func fileWatcher(ctx context.Context) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("❌ 无法启动监控: %v\n", err)
		return
	}
	defer watcher.Close()

	watchDirs := []string{"/etc/mosdns", "/etc/mosdns/rules"}
	for _, dir := range watchDirs {
		if err := watcher.Add(dir); err != nil {
			log.Printf("⚠️ 无法监控目录 %s: %v\n", dir, err)
		}
	}

	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

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
			isRule := filepath.Dir(event.Name) == config.RuleDir && strings.HasSuffix(filename, ".txt") && !strings.HasPrefix(filename, ".")

			if (isConfig || isRule) && (event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0) {
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(1*time.Second, func() {
					if ctx.Err() != nil {
						return
					}
					fmt.Printf("[%s] 📝 检测到文件变更: %s, 准备重启...\n", time.Now().Format("2006-01-02 15:04:05"), event.Name)
					service.RestartService()
				})
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("❌ 监控错误: %v\n", err)
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
	log.Print("⬇️  正在执行计划内 GeoSite/GeoIP 更新...")

	os.MkdirAll("/etc/mosdns/rules", 0755)

	ghProxy := "https://gh-proxy.com/"
	files := map[string]string{
		ghProxy + "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/direct-list.txt": "/etc/mosdns/rules/geosite_cn.txt",
		ghProxy + "https://raw.githubusercontent.com/Loyalsoldier/geoip/release/text/cn.txt":               "/etc/mosdns/rules/geoip_cn.txt",
		ghProxy + "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/apple-cn.txt":    "/etc/mosdns/rules/geosite_apple.txt",
		ghProxy + "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/proxy-list.txt":  "/etc/mosdns/rules/geosite_no_cn.txt",
	}

	anyUpdated := false
	for url, path := range files {
		updated, err := service.DownloadFile(url, path)
		if err != nil {
			log.Printf("⚠️  下载失败 %s: %v (将跳过该文件)\n", path, err)
		} else if updated {
			anyUpdated = true
		}
	}

	if anyUpdated {
		log.Print("🎉 规则文件已更新，fsnotify 将自动触发重启。")
	} else {
		log.Print("✅ 规则已是最新，无需更新。")
	}
}

func initializeEnv() {
	if err := os.MkdirAll("/etc/mosdns/rules", 0755); err != nil {
		log.Printf("❌ 无法创建规则目录: %v\n", err)
	}

	if _, err := os.Stat("/etc/mosdns/config.yaml"); os.IsNotExist(err) {
		log.Print("📢 初始化配置模板...")
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
	log.Print("✅ 运行环境初始化核验完成。")
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}
