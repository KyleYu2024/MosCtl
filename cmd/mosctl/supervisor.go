package main

import (
	"bytes"
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
	RunE: func(cmd *cobra.Command, args []string) error {
		os.Setenv(service.EnvMode, service.ModeManaged)
		return runSupervisor()
	},
}

func init() {
	rootCmd.AddCommand(dockerCmd)
}

func runSupervisor() error {
	os.Setenv(service.EnvMode, service.ModeManaged)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := kernel.Recover(config.MosDNSBin); err != nil {
		return fmt.Errorf("未完成的内核更新恢复失败: %w", err)
	}

	for _, path := range []string{config.ConfigPath, filepath.Join(config.RuleDir, "force-cn.txt"), filepath.Join(config.RuleDir, "force-nocn.txt"), filepath.Join(config.RuleDir, "user_iot.txt"), filepath.Join(config.RuleDir, "hosts.txt"), filepath.Join(config.RuleDir, "geosite_cn.txt"), filepath.Join(config.RuleDir, "geosite_no_cn.txt"), filepath.Join(config.RuleDir, "geosite_apple.txt"), filepath.Join(config.RuleDir, "geoip_cn.txt")} {
		if err := service.RecoverFile(path); err != nil {
			return fmt.Errorf("配置恢复失败: %w", err)
		}
	}
	// 1. 初始化环境
	if err := initializeEnv(); err != nil {
		return err
	}
	if err := config.OptimizeStandard(); err != nil {
		log.Printf("⚠️ 标准配置优化未应用: %v", err)
	}
	statsDone := diagnostics.QueryStats.Start(ctx, "/etc/mosdns/query_stats.json")
	if err := config.EnableQueryStats(); err != nil {
		log.Printf("⚠️ 查询统计未启用: %v\n", err)
	} else {
		diagnostics.QueryStats.SetEnabled(true)
	}

	// 2. 环境变量处理
	if local := os.Getenv("LOCAL_UPSTREAM"); local != "" {
		if err := config.SetUpstream(true, local); err != nil {
			log.Printf("⚠️ LOCAL 配置未应用: %v", err)
		}
	}
	if remote := os.Getenv("REMOTE_UPSTREAM"); remote != "" && !config.RemoteManagedByWeb() {
		if err := config.SetUpstream(false, remote); err != nil {
			log.Printf("⚠️ REMOTE 配置未应用: %v", err)
		}
	}

	currLocal, currRemote := config.GetCurrentUpstreams()
	fmt.Printf("[%s] ⚙️  配置就绪: LOCAL=%s, REMOTE=%s\n", time.Now().Format("2006-01-02 15:04:05"), currLocal, currRemote)

	// Configuration is already final before the first child starts.
	select {
	case <-service.RestartChan:
	default:
	}
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
	return nil
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
		diagnostics.SetProcess(child.Process.Pid)
		if acknowledgement != nil {
			acknowledgement <- nil
			acknowledgement = nil
		}
		done := make(chan error, 1)
		go func() { done <- child.Wait() }()
		childCtx, cancel := context.WithCancel(ctx)
		go func(pid int) {
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
			diagnostics.SampleHealth(childCtx, pid)
			for waitForRetry(childCtx, 30*time.Second) {
				diagnostics.SampleHealth(childCtx, pid)
			}
		}(child.Process.Pid)
		select {
		case err := <-done:
			cancel()
			diagnostics.SetProcess(0)
			fmt.Printf("[%s] ⚠️ MosDNS 已退出（%v），稍后重试。\n", time.Now().Format("2006-01-02 15:04:05"), err)
			if !waitForRetry(ctx, time.Second) {
				return
			}
		case request := <-service.RestartRequests:
			cancel()
			diagnostics.SetProcess(0)
			config.SaveCurrentStatsToHistory()
			stopChild(child, done)
			acknowledgement = request.Done
		case <-service.RestartChan:
			cancel()
			diagnostics.SetProcess(0)
			config.SaveCurrentStatsToHistory()
			fmt.Printf("[%s] 🔄 正在重新加载 MosDNS...\n", time.Now().Format("2006-01-02 15:04:05"))
			stopChild(child, done)
		case <-ctx.Done():
			cancel()
			diagnostics.SetProcess(0)
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
				if service.ManagedContents(event.Name) {
					continue
				}
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(1*time.Second, func() {
					service.OperationMu.Lock()
					defer service.OperationMu.Unlock()
					if ctx.Err() != nil || service.ManagedContents(event.Name) {
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

	stage, err := os.MkdirTemp("/etc/mosdns", ".rules-update-*")
	if err != nil {
		log.Printf("❌ 规则暂存失败: %v", err)
		return
	}
	defer os.RemoveAll(stage)
	changes := make(map[string][]byte)
	for url, path := range files {
		staged := filepath.Join(stage, filepath.Base(path))
		if _, err := service.DownloadFile(url, staged); err != nil {
			log.Printf("⚠️ 规则批次未应用，保留原规则: %v", err)
			return
		}
		data, err := os.ReadFile(staged)
		if err != nil {
			log.Printf("⚠️ 无法读取暂存规则: %v", err)
			return
		}
		old, _ := os.ReadFile(path)
		if !bytes.Equal(old, data) {
			changes[path] = data
		}
	}
	if len(changes) == 0 {
		log.Print("✅ 规则已是最新，无需更新。")
		return
	}
	service.OperationMu.Lock()
	defer service.OperationMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	apply := func(ctx context.Context) error {
		if err := service.RestartAndWait(ctx); err != nil {
			return err
		}
		for ctx.Err() == nil {
			if config.RunTest(ctx) {
				return nil
			}
			if !waitForRetry(ctx, 250*time.Millisecond) {
				break
			}
		}
		return fmt.Errorf("DNS 就绪检查超时")
	}
	if err := service.ApplyFiles(ctx, changes, apply); err != nil {
		log.Printf("❌ 规则批次应用失败: %v", err)
		return
	}
	log.Printf("🎉 已应用 %d 个规则文件，DNS 检查通过。", len(changes))
}

func initializeEnv() error {
	if err := os.MkdirAll("/etc/mosdns/rules", 0755); err != nil {
		return fmt.Errorf("无法创建规则目录: %w", err)
	}

	if _, err := os.Stat("/etc/mosdns/config.yaml"); os.IsNotExist(err) {
		log.Print("📢 初始化配置模板...")
		if err := copyFile("/usr/share/mosdns/config.yaml", "/etc/mosdns/config.yaml"); err != nil {
			return fmt.Errorf("初始化配置失败: %w", err)
		}

		files, _ := filepath.Glob("/usr/share/mosdns/rules/*.txt")
		for _, f := range files {
			if err := copyFile(f, filepath.Join("/etc/mosdns/rules", filepath.Base(f))); err != nil {
				return fmt.Errorf("初始化规则失败: %w", err)
			}
		}
	} else {
		if err := config.EnsureMetricsServer(); err != nil {
			return err
		}
		if err := config.EnsureDefaultTTL(); err != nil {
			return err
		}
	}

	requiredFiles := []string{
		"/etc/mosdns/rules/force-cn.txt",
		"/etc/mosdns/rules/force-nocn.txt",
		"/etc/mosdns/rules/user_iot.txt",
		"/etc/mosdns/rules/hosts.txt",
	}
	for _, rf := range requiredFiles {
		if _, err := os.Stat(rf); os.IsNotExist(err) {
			if err := os.WriteFile(rf, []byte{}, 0644); err != nil {
				return err
			}
		}
	}
	log.Print("✅ 运行环境初始化核验完成。")
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}
