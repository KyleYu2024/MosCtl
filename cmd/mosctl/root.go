package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/KyleYu2024/mosctl/internal/config"
	"github.com/KyleYu2024/mosctl/internal/rule"
	"github.com/KyleYu2024/mosctl/internal/service"
	"github.com/spf13/cobra"
)

// rootCmd 代表没有调用子命令时的基础命令
var rootCmd = &cobra.Command{
	Use:   "mosctl",
	Short: "MosDNS control tool",
	Long:  `MosCtl is a CLI tool to manage MosDNS service, rules, and rescue modes.`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			// 检测是否在 Docker 环境中运行
			if os.Getenv("DOCKER_CONTAINER") != "" || isDocker() {
				runDockerPanel()
			} else {
				showMenu()
			}
		} else {
			cmd.Help()
		}
	},
}

func isDocker() bool {
	// 简单检测方法：检查 /.dockerenv 是否存在，或者 /proc/1/cgroup 是否包含 docker
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	
	cgroup, err := os.ReadFile("/proc/1/cgroup")
	if err == nil && strings.Contains(string(cgroup), "docker") {
		return true
	}
	
	// 如果在 alpine 中运行，可能是 /proc/self/cgroup
	cgroup, err = os.ReadFile("/proc/self/cgroup")
	if err == nil && strings.Contains(string(cgroup), "docker") {
		return true
	}

	return false
}

func showMenu() {
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Println("\n\033[0;32m=====================================\033[0m")
		fmt.Println("\033[0;32m         MosDNS 管理面板 [0.4.2-docker]      \033[0m")
		fmt.Println("\033[0;32m=====================================\033[0m")
		
		status := "🟢 运行中"
		if exec.Command("systemctl", "is-active", "mosdns").Run() != nil {
			status = "🔴 未运行"
		}

		version := "未知"
		vCmd := exec.Command("/usr/local/bin/mosdns", "version")
		if vOut, err := vCmd.Output(); err == nil {
			vStr := strings.TrimSpace(string(vOut))
			vStr = strings.TrimPrefix(vStr, "mosdns")
			vStr = strings.TrimSpace(vStr)
			if vStr != "" {
				version = vStr
			}
		}

		fmt.Printf(" 状态: %s | 核心: %s\n", status, version)
		fmt.Println("\033[0;32m=====================================\033[0m")
		fmt.Println(" [1] 服务管理 (启动/停止/重启)")
		fmt.Println(" [2] 参数设置 (上游/缓存/TTL)")
		fmt.Println(" [3] 规则管理 (强制国内/国外/IoT)")
		fmt.Println(" [4] 更新 Geo 数据库")
		fmt.Println(" [5] 救援模式管理")
		fmt.Println(" [6] 日志管理中心")
		fmt.Println(" [7] DNS 解析测试")
		fmt.Println(" [8] 彻底卸载脚本")
		fmt.Println(" [0] 退出程序")
		fmt.Println("\033[0;32m=====================================\033[0m")
		fmt.Print(" 请选择: ")

		if !scanner.Scan() {
			break
		}
		choice := strings.TrimSpace(scanner.Text())

		switch choice {
		case "1":
			serviceMenu(scanner)
		case "2":
			dnsSettingsMenu(scanner)
		case "3":
			rulesMenu(scanner)
		case "4":
			UpdateGeoRules()
		case "5":
			rescueMenu(scanner)
		case "6":
			logMenu(scanner)
		case "7":
			config.RunTest()
		case "8":
			fmt.Print("⚠️  高危操作：确定要彻底卸载 MosDNS 吗？(y/n): ")
			scanner.Scan()
			if strings.ToLower(scanner.Text()) == "y" {
				uninstall()
			}
		case "0":
			os.Exit(0)
		default:
			fmt.Println("❌ 无效选项")
		}
		
		if choice != "0" && choice != "7" {
			fmt.Print("\n按回车键继续...")
			scanner.Scan()
		}
	}
}

// ... logMenu, serviceMenu, dnsSettingsMenu, rescueMenu, uninstall 代码保持不变 ...
// (请保留您原有的这些函数，此处仅展示修改后的 rulesMenu)

func logMenu(scanner *bufio.Scanner) {
	for {
		size := config.GetLogSize()
		level := config.GetLogLevel()
		fmt.Println("\n--- 日志管理中心 ---")
		fmt.Printf("  当前日志大小: %s | 当前级别: %s\n", size, level)
		fmt.Println("  1. 📜  实时查看日志 (Tail)")
		fmt.Println("  2. ⚙️   修改日志级别 (debug/info/warn/error)")
		fmt.Println("  3. 🧹  立即清空日志")
		fmt.Println("  0. 🔙  返回")
		fmt.Print("请选择: ")
		scanner.Scan()
		sel := scanner.Text()
		switch sel {
		case "1":
			cmd := exec.Command("tail", "-n", "50", "-f", "/var/log/mosdns.log")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			fmt.Println("按 Ctrl+C 退出日志查看...")
			cmd.Run()
		case "2":
			fmt.Print("请输入日志级别 (debug/info/warn/error): ")
			scanner.Scan()
			lv := strings.ToLower(strings.TrimSpace(scanner.Text()))
			if lv == "debug" || lv == "info" || lv == "warn" || lv == "error" {
				config.SetLogLevel(lv)
				fmt.Printf("✅ 日志级别已设为 %s\n", lv)
			} else {
				fmt.Println("❌ 无效级别")
			}
		case "3":
			if err := config.ClearLogs(); err != nil {
				fmt.Printf("❌ 清理失败: %v\n", err)
			} else {
				fmt.Println("✅ 日志已清空")
			}
		case "0":
			return
		}
	}
}

func serviceMenu(scanner *bufio.Scanner) {
	fmt.Println("\n--- 服务管理 ---")
	fmt.Println("  1. ▶️  启动服务")
	fmt.Println("  2. ⏹️  停止服务")
	fmt.Println("  3. 🔄  重启服务")
	fmt.Println("  0. 🔙  返回")
	fmt.Print("请选择: ")
	scanner.Scan()
	switch scanner.Text() {
	case "1":
		exec.Command("systemctl", "start", "mosdns").Run()
		fmt.Println("✅ 已发送启动指令")
	case "2":
		exec.Command("systemctl", "stop", "mosdns").Run()
		fmt.Println("🛑 已发送停止指令")
	case "3":
		service.RestartService()
		fmt.Println("✅ 已发送重启指令")
	}
}

func dnsSettingsMenu(scanner *bufio.Scanner) {
	local, remote := config.GetCurrentUpstreams()
	ttl := config.GetCurrentTTL()

	fmt.Println("\n--- DNS 参数设置 ---")
	fmt.Printf("  1. 📡  修改国内上游 DNS (当前: %s)\n", local)
	fmt.Printf("  2. 🌍  修改国外上游 DNS (当前: %s)\n", remote)
	fmt.Printf("  3. ⏱️  设置缓存 TTL (当前: %s 秒)\n", ttl)
	fmt.Println("  4. 🧹  清空 DNS 缓存")
	fmt.Println("  0. 🔙  返回")
	fmt.Print("请选择: ")
	scanner.Scan()
	switch scanner.Text() {
	case "1":
		fmt.Print("输入新的国内 DNS (如 udp://119.29.29.29): ")
		scanner.Scan()
		config.SetUpstream(true, scanner.Text())
	case "2":
		fmt.Print("输入新的国外 DNS (如 127.0.0.1:5353): ")
		scanner.Scan()
		config.SetUpstream(false, scanner.Text())
	case "3":
		fmt.Print("输入新的 TTL (秒): ")
		scanner.Scan()
		config.SetCacheTTL(scanner.Text())
	case "4":
		config.FlushCache()
	}
}

func rescueMenu(scanner *bufio.Scanner) {
	fmt.Println("\n--- 救援模式 ---")
	fmt.Println("  1. ✅  开启救援模式")
	fmt.Println("  2. ⏹️  关闭救援模式")
	fmt.Println("  0. 🔙  返回")
	fmt.Print("请选择: ")
	scanner.Scan()
	switch scanner.Text() {
	case "1":
		service.EnableRescue()
	case "2":
		service.DisableRescue()
	}
}

func uninstall() {
	fmt.Println("⏳ 正在彻底卸载...")
	exec.Command("systemctl", "stop", "mosdns").Run()
	exec.Command("systemctl", "disable", "mosdns").Run()
	os.Remove("/etc/systemd/system/mosdns.service")
	os.Remove("/etc/systemd/system/mosdns-rescue.service")
	os.RemoveAll("/etc/mosdns")
	os.Remove("/usr/local/bin/mosdns")
	os.Remove("/usr/local/bin/mosctl")
	fmt.Println("✅ 卸载完成。")
	os.Exit(0)
}

// -----------------------------------------------------
// 重点修复的 rulesMenu 函数
// -----------------------------------------------------
func rulesMenu(scanner *bufio.Scanner) {
	fmt.Println("\n--- 规则管理 ---")
	fmt.Println("  1. 🇨🇳 添加域名 -> 强制国内 (Force CN)")
	fmt.Println("  2. 🌍 添加域名 -> 强制国外 (Force NoCN)")
	fmt.Println("  3. 🔌 添加 IP/CIDR -> 智能家居 (IoT)")
	fmt.Println("  4. 📝 手动编辑规则文件 (Nano)")
	fmt.Println("  0. 🔙  返回")
	fmt.Print("请选择: ")
	scanner.Scan()
	sel := scanner.Text()
	
	if sel == "1" || sel == "2" || sel == "3" {
		fmt.Print("请输入内容 (域名或 IP): ")
		scanner.Scan()
		content := strings.TrimSpace(scanner.Text())
		if content == "" {
			return
		}

		var err error
		if sel == "1" {
			err = rule.AddRule(content, rule.TypeForceCN)
		} else if sel == "2" {
			err = rule.AddRule(content, rule.TypeForceNoCN)
		} else {
			err = rule.AddRule(content, rule.TypeIoT)
		}
		
		if err != nil {
			fmt.Printf("❌ 失败: %v\n", err)
		}
	} else if sel == "4" {
		// 手动编辑子菜单
		manualEditMenu(scanner)
	}
}

func manualEditMenu(scanner *bufio.Scanner) {
	fmt.Println("\n--- 请选择要编辑的文件 ---")
	fmt.Println("  1. 🇨🇳 强制国内名单 (force-cn.txt)")
	fmt.Println("  2. 🌍 强制国外名单 (force-nocn.txt)")
	fmt.Println("  3. 🔌 智能家居名单 (user_iot.txt)")
	fmt.Println("  4. 📔 自定义 Hosts (hosts.txt)")
	fmt.Println("  0. 🔙  返回")
	fmt.Print("请选择: ")
	scanner.Scan()
	
	var fileToEdit string
	switch scanner.Text() {
	case "1":
		fileToEdit = rule.PathForceCN
	case "2":
		fileToEdit = rule.PathForceNoCN
	case "3":
		fileToEdit = rule.PathIoT
	case "4":
		fileToEdit = "/etc/mosdns/rules/hosts.txt"
	case "0":
		return
	default:
		fmt.Println("❌ 无效选项")
		return
	}

	// 调用 Nano 编辑
	fmt.Printf("📝 正在打开编辑器: %s ...\n", fileToEdit)
	
	// 确保文件存在，否则 nano 打开可能是空文件
	if _, err := os.Stat(fileToEdit); os.IsNotExist(err) {
		os.MkdirAll("/etc/mosdns/rules", 0755)
		os.WriteFile(fileToEdit, []byte{}, 0644)
	}

	cmd := exec.Command("nano", fileToEdit)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("❌ 编辑出错 (请确保系统已安装 nano): %v\n", err)
	} else {
		// 编辑完成后询问重启
		fmt.Print("❓ 是否重启 MosDNS 以应用更改? (Y/n): ")
		scanner.Scan()
		ans := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if ans == "" || ans == "y" {
			if err := service.RestartService(); err != nil {
				fmt.Printf("❌ 重启失败: %v\n", err)
			} else {
				fmt.Println("✅ 服务已重启，规则生效。")
			}
		}
	}
}

// Execute 是 main.go 调用的入口
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
