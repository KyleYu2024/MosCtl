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
			showMenu()
		} else {
			cmd.Help()
		}
	},
}

func showMenu() {
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Println("\n\033[0;32m=====================================\033[0m")
		fmt.Println("\033[0;32m         MosDNS 管理面板 [0.4.6]      \033[0m")
		fmt.Println("\033[0;32m=====================================\033[0m")
		
		status := "🟢 运行中"
		if exec.Command("systemctl", "is-active", "mosdns").Run() != nil {
			status = "🔴 未运行"
		}

		// 改进的版本获取逻辑
		version := "未知"
		vCmd := exec.Command("/usr/local/bin/mosdns", "version")
		if vOut, err := vCmd.Output(); err == nil {
			vStr := strings.TrimSpace(string(vOut))
			// 过滤掉输出中可能存在的 "mosdns" 前缀或空格
			vStr = strings.TrimPrefix(vStr, "mosdns")
			vStr = strings.TrimSpace(vStr)
			if vStr != "" {
				version = vStr
			}
		}

		fmt.Printf(" 状态: %s | 核心: %s\n", status, version)
		fmt.Println("\033[0;32m=====================================\033[0m")
		fmt.Println(" [1] 服务管理 (启动/停止/重启)")
		fmt.Println(" [2] 同步配置 (Git Pull)")
		fmt.Println(" [3] 参数设置 (上游/缓存/TTL)")
		fmt.Println(" [4] 规则管理 (直连/代理/IoT)")
		fmt.Println(" [5] 更新 Geo 数据库")
		fmt.Println(" [6] 救援模式管理")
		fmt.Println(" [7] 查看运行日志")
		fmt.Println(" [8] DNS 解析测试")
		fmt.Println(" [9] 彻底卸载脚本")
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
			config.SyncConfig()
		case "3":
			dnsSettingsMenu(scanner)
		case "4":
			rulesMenu(scanner)
		case "5":
			UpdateGeoRules()
		case "6":
			rescueMenu(scanner)
		case "7":
			cmd := exec.Command("tail", "-n", "50", "-f", "/var/log/mosdns.log")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			fmt.Println("按 Ctrl+C 退出日志查看...")
			cmd.Run()
		case "8":
			config.RunTest()
		case "9":
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

func rulesMenu(scanner *bufio.Scanner) {
	fmt.Println("\n--- 规则管理 ---")
	fmt.Println("  1. 🇨🇳 添加域名到直连名单")
	fmt.Println("  2. 🌍 添加域名到代理名单")
	fmt.Println("  3. 🔌 添加 IP/CIDR 到 IoT 名单")
	fmt.Println("  4. 📝 直接编辑规则文件")
	fmt.Println("  0. 🔙  返回")
	fmt.Print("请选择: ")
	scanner.Scan()
	sel := scanner.Text()
	if sel == "1" || sel == "2" || sel == "3" {
		fmt.Print("请输入内容 (域名或 IP): ")
		scanner.Scan()
		content := scanner.Text()
		var err error
		if sel == "1" {
			err = rule.AddRule(content, true, false)
		} else if sel == "2" {
			err = rule.AddRule(content, false, false)
		} else {
			err = rule.AddRule(content, false, true)
		}
		if err != nil {
			fmt.Printf("❌ 失败: %v\n", err)
		}
	} else if sel == "4" {
		fmt.Println("请手动编辑 /etc/mosdns/rules/ 目录下的文件。")
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

// Execute 是 main.go 调用的入口
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}