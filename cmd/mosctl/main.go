package main

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	ConfigDir     = "/etc/mosdns"
	RuleDir       = "/etc/mosdns/rules"
	RescueDNS     = "223.5.5.5:53"
	DefaultRemote = "udp://8.8.8.8"
)

func main() {
	time.Local = time.FixedZone("CST", 8*3600)
	setupRules()
	setEnv()
	
	// 第一次更新改为同步执行，确保启动时文件存在
	updateGeoData()
	
	go startDailyUpdateLoop() // 之后改为后台轮询

	// 打印当前启动时间
	fmt.Printf("[%s] Starting mosctl entrypoint...\n", time.Now().Format("2006-01-02 15:04:05"))

	// Signal handling for graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		fmt.Println("Shutting down...")
		os.Exit(0)
	}()

	for {
		runConnectivityTest()
		renderConfig()
		fmt.Printf("[%s] Starting MosDNS core...\n", time.Now().Format("2006-01-02 15:04:05"))
		cmd := exec.Command("mosdns", "start", "-c", "/etc/mosdns/config.yaml")
		cmd.Env = os.Environ() // 确保继承 TZ 环境变量
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Start(); err != nil {
			fmt.Printf("Failed to start MosDNS: %v\n", err)
		} else {
			// Monitor MosDNS
			done := make(chan error, 1)
			go func() {
				done <- cmd.Wait()
			}()

			select {
			case err := <-done:
				fmt.Printf("MosDNS core exited: %v\n", err)
			}
		}

		// Rescue mode: simple forwarder
		fmt.Println("Entering rescue mode (Failover to 223.5.5.5)...")
		stopRescue := make(chan bool)
		rescueDone := make(chan bool)
		go startRescueForwarder(stopRescue, rescueDone)

		// Wait before trying to restart MosDNS
		time.Sleep(30 * time.Second)
		stopRescue <- true
		<-rescueDone
		fmt.Println("Restarting MosDNS core...")
	}
}

func setupRules() {
	os.MkdirAll(RuleDir, 0755)
	files := []string{"local_direct.txt", "local_proxy.txt", "user_iot.txt", "hosts.txt", "geosite_cn.txt", "geoip_cn.txt"}
	for _, f := range files {
		path := filepath.Join(RuleDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			os.WriteFile(path, []byte("# Placeholder for "+f+"\n"), 0644)
		}
	}
}

func setEnv() {
	if os.Getenv("REMOTE_DNS") == "" {
		fmt.Printf("REMOTE_DNS not set, using default: %s\n", DefaultRemote)
		os.Setenv("REMOTE_DNS", DefaultRemote)
	} else {
		fmt.Printf("Using REMOTE_DNS: %s\n", os.Getenv("REMOTE_DNS"))
	}
}

func startRescueForwarder(stop chan bool, done chan bool) {
	defer func() { done <- true }()
	addr, err := net.ResolveUDPAddr("udp", ":53")
	if err != nil {
		fmt.Printf("Rescue error: %v\n", err)
		return
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		fmt.Printf("Rescue forwarder failed to listen on :53 (maybe port occupied?): %v\n", err)
		return
	}
	defer conn.Close()

	fmt.Println("Rescue forwarder active on :53 -> 223.5.5.5:53")

	go func() {
		<-stop
		conn.Close()
	}()

	remoteAddr, _ := net.ResolveUDPAddr("udp", RescueDNS)
	buf := make([]byte, 2048)

	for {
		n, clientAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}

		query := make([]byte, n)
		copy(query, buf[:n])

		go func(data []byte, cAddr *net.UDPAddr) {
			rConn, err := net.DialUDP("udp", nil, remoteAddr)
			if err != nil {
				return
			}
			defer rConn.Close()

			rConn.SetDeadline(time.Now().Add(2 * time.Second))
			_, err = rConn.Write(data)
			if err != nil {
				return
			}

			respBuf := make([]byte, 2048)
			rn, _, err := rConn.ReadFromUDP(respBuf)
			if err != nil {
				return
			}

			conn.WriteToUDP(respBuf[:rn], cAddr)
		}(query, clientAddr)
	}
}

func runConnectivityTest() {
	fmt.Println("\n🩺 Running DNS connectivity diagnostic...")
	
	test := func(domain, server string) {
		// Clean up server address (remove protocol prefix if present)
		serverAddr := server
		if strings.Contains(serverAddr, "://") {
			parts := strings.Split(serverAddr, "://")
			serverAddr = parts[1]
		}
		// Add default port if missing
		if !strings.Contains(serverAddr, ":") {
			serverAddr = serverAddr + ":53"
		}

		r := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: 3 * time.Second}
				return d.DialContext(ctx, "udp", serverAddr)
			},
		}
		
		start := time.Now()
		ips, err := r.LookupHost(context.Background(), domain)
		duration := time.Since(start)

		if err == nil && len(ips) > 0 {
			fmt.Printf("  [PASS] %-15s via %-15s (%v) IP: %s\n", domain, serverAddr, duration.Round(time.Millisecond), ips[0])
		} else {
			fmt.Printf("  [FAIL] %-15s via %-15s (Error: %v)\n", domain, serverAddr, err)
		}
	}

	// Test domestic connectivity via Alidns
	test("www.baidu.com", "223.5.5.5:53")
	
	// Test remote connectivity via the configured REMOTE_DNS
	remote := os.Getenv("REMOTE_DNS")
	if remote == "" {
		remote = DefaultRemote
	}
	test("www.google.com", remote)
	fmt.Println()
}

func renderConfig() {
	templatePath := "/etc/mosdns/config.yaml.template"
	targetPath := "/etc/mosdns/config.yaml"

	// 如果模板不存在（比如在开发环境），尝试直接使用现有的
	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		fmt.Println("⚠️  Configuration template not found, skipping rendering.")
		return
	}

	content, err := os.ReadFile(templatePath)
	if err != nil {
		fmt.Printf("❌ Failed to read template: %v\n", err)
		return
	}

	remoteDNS := os.Getenv("REMOTE_DNS")
	if remoteDNS == "" {
		remoteDNS = DefaultRemote
	}

	rendered := strings.ReplaceAll(string(content), "${REMOTE_DNS}", remoteDNS)

	err = os.WriteFile(targetPath, []byte(rendered), 0644)
	if err != nil {
		fmt.Printf("❌ Failed to write rendered config: %v\n", err)
		return
	}
	ensureValidCacheDump()
	fmt.Println("✅ Configuration rendered successfully.")
}

func ensureValidCacheDump() {
	dumpPath := "/etc/mosdns/cache.dump"
	if _, err := os.Stat(dumpPath); err == nil {
		return // 文件已存在，跳过
	}

	// 创建一个合法的空 Gzip 文件
	f, err := os.Create(dumpPath)
	if err != nil {
		return
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	gw.Write([]byte("mosdns_cache_v2"))
	gw.Close()
}

func startDailyUpdateLoop() {
	for {
		time.Sleep(24 * time.Hour)
		updateGeoData()
	}
}

func updateGeoData() {
	fmt.Printf("[%s] Checking for Geo data updates...\n", time.Now().Format("2006-01-02 15:04:05"))
	
	mirrors := []string{
		"https://ghproxy.net/https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/",
		"https://mirror.ghproxy.com/https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/",
	}
	
	files := map[string]string{
		"geoip-cn.txt":   filepath.Join(RuleDir, "geoip_cn.txt"),
		"geosite-cn.txt": filepath.Join(RuleDir, "geosite_cn.txt"),
	}

	for remote, local := range files {
		success := false
		for _, base := range mirrors {
			err := downloadFile(base+remote, local)
			if err == nil {
				fmt.Printf("  [SUCCESS] Updated %s from mirror\n", remote)
				success = true
				break
			}
			fmt.Printf("  [RETRY] Mirror failed for %s, trying next...\n", remote)
		}
		
		if !success {
			fmt.Printf("  [WARN] All mirrors failed for %s. Using existing/placeholder file.\n", remote)
		}
	}
}

func downloadFile(url, path string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}
