package main

import (
	"context"
	"fmt"
	"net"
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
	// 强制设置北京时间 (UTC+8)
	time.Local = time.FixedZone("CST", 8*3600)
	
	setupRules()
	setEnv()

	fmt.Printf("[%s] MosCtl Entrypoint Initialized.\n", time.Now().Format("2006-01-02 15:04:05"))

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
		cmd.Env = os.Environ()
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Start(); err != nil {
			fmt.Printf("Failed to start MosDNS: %v\n", err)
		} else {
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				fmt.Printf("MosDNS core exited: %v\n", err)
			}
		}

		fmt.Println("Entering rescue mode (Failover to 223.5.5.5)...")
		stopRescue := make(chan bool)
		rescueDone := make(chan bool)
		go startRescueForwarder(stopRescue, rescueDone)

		time.Sleep(30 * time.Second)
		stopRescue <- true
		<-rescueDone
	}
}

func setupRules() {
	os.MkdirAll(RuleDir, 0755)
	files := []string{"local_direct.txt", "local_proxy.txt", "user_iot.txt", "hosts.txt", "geosite_cn.txt", "geoip_cn.txt"}
	for _, f := range files {
		path := filepath.Join(RuleDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			os.WriteFile(path, []byte("# Placeholder\n"), 0644)
		}
	}
}

func setEnv() {
	if os.Getenv("REMOTE_DNS") == "" {
		os.Setenv("REMOTE_DNS", DefaultRemote)
	}
}

func renderConfig() {
	templatePath := "/etc/mosdns/config.yaml.template"
	targetPath := "/etc/mosdns/config.yaml"
	dumpPath := "/etc/mosdns/cache.dump"

	content, err := os.ReadFile(templatePath)
	if err != nil { return }

	rendered := strings.ReplaceAll(string(content), "${REMOTE_DNS}", os.Getenv("REMOTE_DNS"))

	// 核心优化：如果缓存文件还没生成或者不合法，直接在配置中注销 dump_file，彻底消除报错
	// 我们检查文件大小，如果小于 100 字节（合法的缓存通常很大），就认为还没准备好
	hasValidCache := false
	if info, err := os.Stat(dumpPath); err == nil && info.Size() > 100 {
		hasValidCache = true
	}

	lines := strings.Split(rendered, "\n")
	var finalLines []string
	for _, line := range lines {
		if strings.Contains(line, "dump_file:") && !hasValidCache {
			// 如果缓存不合法，注释掉这一行，MosDNS 就不会尝试加载它
			finalLines = append(finalLines, "      # dump_file: \"/etc/mosdns/cache.dump\" (hidden to avoid errors)")
		} else {
			finalLines = append(finalLines, line)
		}
	}

	os.WriteFile(targetPath, []byte(strings.Join(finalLines, "\n")), 0644)
	fmt.Println("✅ Configuration rendered (Auto-optimized cache setting).")
}

func runConnectivityTest() {
	fmt.Println("\n🩺 Running DNS connectivity diagnostic...")
	test := func(domain, server string) {
		serverAddr := server
		if strings.Contains(serverAddr, "://") {
			serverAddr = strings.Split(serverAddr, "://")[1]
		}
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
		if err == nil && len(ips) > 0 {
			fmt.Printf("  [PASS] %-15s via %-15s (%v)\n", domain, serverAddr, time.Since(start).Round(time.Millisecond))
		} else {
			fmt.Printf("  [FAIL] %-15s via %-15s\n", domain, serverAddr)
		}
	}
	test("www.baidu.com", "223.5.5.5:53")
	test("www.google.com", os.Getenv("REMOTE_DNS"))
	fmt.Println()
}

func startRescueForwarder(stop chan bool, done chan bool) {
	defer func() { done <- true }()
	addr, _ := net.ResolveUDPAddr("udp", ":53")
	conn, err := net.ListenUDP("udp", addr)
	if err != nil { return }
	defer conn.Close()
	go func() { <-stop; conn.Close() }()

	remoteAddr, _ := net.ResolveUDPAddr("udp", RescueDNS)
	buf := make([]byte, 2048)
	for {
		n, clientAddr, err := conn.ReadFromUDP(buf)
		if err != nil { return }
		query := make([]byte, n)
		copy(query, buf[:n])
		go func(data []byte, cAddr *net.UDPAddr) {
			rConn, err := net.DialUDP("udp", nil, remoteAddr)
			if err != nil { return }
			defer rConn.Close()
			rConn.SetDeadline(time.Now().Add(2 * time.Second))
			rConn.Write(data)
			respBuf := make([]byte, 2048)
			rn, _, err := rConn.ReadFromUDP(respBuf)
			if err == nil {
				conn.WriteToUDP(respBuf[:rn], cAddr)
			}
		}(query, clientAddr)
	}
}
