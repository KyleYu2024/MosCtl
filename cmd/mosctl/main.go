package main

import (
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
	// 强制设置北京时间 (UTC+8)
	time.Local = time.FixedZone("CST", 8*3600)
	
	setupRules()
	setEnv()

	fmt.Printf("[%s] MosCtl Entrypoint Initialized.\n", time.Now().Format("2006-01-02 15:04:05"))

	// 首次启动时检查并更新 Geo 数据
	ensureGeoData()

	// 启动定时更新任务 (每天 02:30)
	go startScheduledUpdate()

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
	fmt.Printf("[%s] Initializing rules in: %s\n", time.Now().Format("2006-01-02 15:04:05"), RuleDir)
	
	if err := os.MkdirAll(RuleDir, 0755); err != nil {
		fmt.Printf("❌ ERROR: Failed to create/access RuleDir %s: %v\n", RuleDir, err)
	}

	// 基础规则文件
	files := []string{"local_direct.txt", "local_proxy.txt", "user_iot.txt", "hosts.txt", "geosite_cn.txt", "geoip_cn.txt"}
	for _, f := range files {
		path := filepath.Join(RuleDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Printf("  >> Creating missing rule: %s\n", f)
			if err := os.WriteFile(path, []byte("# Placeholder\n"), 0644); err != nil {
				fmt.Printf("  ❌ ERROR creating %s: %v\n", f, err)
			}
		}
	}

	// 自动恢复 config.yaml 到挂载的目录
	userConfigPath := filepath.Join(RuleDir, "config.yaml")
	backupPath := "/etc/mosctl/config.yaml.template"
	if _, err := os.Stat(userConfigPath); os.IsNotExist(err) {
		fmt.Printf("  >> config.yaml not found, restoring from %s\n", backupPath)
		if input, err := os.ReadFile(backupPath); err == nil {
			if err := os.WriteFile(userConfigPath, input, 0644); err != nil {
				fmt.Printf("  ❌ ERROR restoring config.yaml: %v\n", err)
			} else {
				fmt.Println("  ✅ Successfully restored config.yaml")
			}
		} else {
			fmt.Printf("  ❌ ERROR: Backup template missing: %v\n", err)
		}
	}
}

func setEnv() {
	if os.Getenv("REMOTE_DNS") == "" {
		os.Setenv("REMOTE_DNS", DefaultRemote)
	}
}

func startScheduledUpdate() {
	for {
		now := time.Now()
		// 计算距离下次 02:30 的时间
		next := time.Date(now.Year(), now.Month(), now.Day(), 2, 30, 0, 0, now.Location())
		if now.After(next) {
			next = next.Add(24 * time.Hour)
		}
		
		fmt.Printf("[%s] Next Geo data update scheduled at %s\n", 
			time.Now().Format("2006-01-02 15:04:05"), 
			next.Format("2006-01-02 15:04:05"))
		
		time.Sleep(time.Until(next))
		updateGeoData()
	}
}

func ensureGeoData() {
	files := []string{
		filepath.Join(RuleDir, "geoip_cn.txt"),
		filepath.Join(RuleDir, "geosite_cn.txt"),
	}
	
	needsUpdate := false
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil || info.Size() < 100 {
			needsUpdate = true
			break
		}
		// 检查内容是否包含 "404"
		content, _ := os.ReadFile(f)
		if strings.Contains(string(content), "404: Not Found") {
			needsUpdate = true
			break
		}
	}

	if needsUpdate {
		fmt.Printf("[%s] Geo data missing or invalid, updating now...\n", time.Now().Format("2006-01-02 15:04:05"))
		updateGeoData()
	}
}

func updateGeoData() {
	fmt.Printf("[%s] Updating Geo data (ghproxy.net)...\n", time.Now().Format("2006-01-02 15:04:05"))
	
	files := map[string]string{
		"https://ghproxy.net/https://raw.githubusercontent.com/Loyalsoldier/geoip/release/text/cn.txt":        filepath.Join(RuleDir, "geoip_cn.txt"),
		"https://ghproxy.net/https://raw.githubusercontent.com/Loyalsoldier/domain-list-custom/release/cn.txt": filepath.Join(RuleDir, "geosite_cn.txt"),
	}

	for remote, local := range files {
		err := downloadFile(remote, local)
		if err != nil {
			fmt.Printf("  [WARN] Failed to update %s: %v\n", remote, err)
		} else {
			fmt.Printf("  [SUCCESS] Updated %s\n", filepath.Base(local))
		}
	}
}

func downloadFile(url, path string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	out, err := os.Create(path + ".tmp")
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}
	
	out.Close()
	return os.Rename(path+".tmp", path)
}

func renderConfig() {
	userConfigPath := filepath.Join(RuleDir, "config.yaml")
	targetPath := "/etc/mosdns/config.yaml"
	dumpPath := "/etc/mosdns/cache.dump"

	content, err := os.ReadFile(userConfigPath)
	if err != nil { 
		fmt.Printf("❌ ERROR: Failed to read user config at %s: %v\n", userConfigPath, err)
		return 
	}

	fmt.Printf("✅ Loading config from: %s\n", userConfigPath)
	rendered := strings.ReplaceAll(string(content), "${REMOTE_DNS}", os.Getenv("REMOTE_DNS"))

	hasValidCache := false
	if info, err := os.Stat(dumpPath); err == nil && info.Size() > 100 {
		hasValidCache = true
	}

	lines := strings.Split(rendered, "\n")
	var finalLines []string
	for _, line := range lines {
		if strings.Contains(line, "dump_file:") && !hasValidCache {
			finalLines = append(finalLines, "      # dump_file: \"/etc/mosdns/cache.dump\" (hidden to avoid errors)")
		} else {
			finalLines = append(finalLines, line)
		}
	}

	os.WriteFile(targetPath, []byte(strings.Join(finalLines, "\n")), 0644)
	fmt.Println("✅ Configuration rendered (Auto-optimized cache setting).")
}

func runConnectivityTest() {
	fmt.Printf("[%s] 🩺 Running DNS connectivity diagnostic...\n", time.Now().Format("2006-01-02 15:04:05"))
	
	test := func(domain, server string) {
		host := server
		if strings.Contains(host, "://") {
			host = strings.Split(host, "://")[1]
		}
		if strings.Contains(host, ":") {
			host = strings.Split(host, ":")[0]
		}

		fmt.Printf("  >> Testing %s via %s... ", domain, host)
		cmd := exec.Command("nslookup", domain, host)
		output, err := cmd.CombinedOutput()
		
		if err != nil {
			fmt.Printf("❌ FAILED\n     Error: %v\n     Output: %s\n", err, strings.TrimSpace(string(output)))
		} else {
			outStr := strings.TrimSpace(string(output))
			if strings.Contains(outStr, "Address") {
				fmt.Println("✅ OK")
				// 打印简短的解析结果
				lines := strings.Split(outStr, "\n")
				for _, line := range lines {
					if strings.Contains(line, "Address") && !strings.Contains(line, "#") {
						fmt.Printf("     %s\n", strings.TrimSpace(line))
					}
				}
			} else {
				fmt.Printf("❓ UNKNOWN\n     Output: %s\n", outStr)
			}
		}
	}

	test("baidu.com", "223.5.5.5")
	remote := os.Getenv("REMOTE_DNS")
	if remote == "" {
		remote = DefaultRemote
	}
	test("google.com", remote)
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
