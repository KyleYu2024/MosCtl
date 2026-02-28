package service

import (
	"fmt"
	"os/exec"
)

// 救援 DNS：当 MosDNS 挂掉时，流量将被劫持到这里
const RescueDNS = "223.5.5.5:53"

// EnableRescue 开启救援模式
func EnableRescue() error {
	fmt.Println("🚑 正在启动救援模式 (Failover to 223.5.5.5)...")

	// 1. 开启 IPv4 转发
	if err := runCommand("sysctl", "-w", "net.ipv4.ip_forward=1"); err != nil {
		return fmt.Errorf("无法开启内核转发: %v", err)
	}

	// 1.5 确保 INPUT 链放行 53 端口 (防止被拦截)
	_ = runCommand("iptables", "-I", "INPUT", "-p", "udp", "--dport", "53", "-j", "ACCEPT")
	_ = runCommand("iptables", "-I", "INPUT", "-p", "tcp", "--dport", "53", "-j", "ACCEPT")

	// 2. 创建并初始化自定义链
	_ = runCommand("iptables", "-t", "nat", "-N", "MOSCTL_RESCUE")
	_ = runCommand("iptables", "-t", "nat", "-F", "MOSCTL_RESCUE")

	// 3. 在自定义链中添加规则
	err := runCommand("iptables", "-t", "nat", "-A", "MOSCTL_RESCUE", 
		"-p", "udp", "--dport", "53", 
		"-j", "DNAT", "--to-destination", RescueDNS)
	if err != nil {
		return fmt.Errorf("无法设置 DNAT 规则: %v", err)
	}

	err = runCommand("iptables", "-t", "nat", "-A", "MOSCTL_RESCUE", 
		"-p", "tcp", "--dport", "53", 
		"-j", "DNAT", "--to-destination", RescueDNS)
	if err != nil {
		return fmt.Errorf("无法设置 TCP DNAT 规则: %v", err)
	}

	// 4. 将自定义链挂载到 PREROUTING (如果还没挂载)
	// 检查是否已经存在跳转规则
	checkCmd := exec.Command("iptables", "-t", "nat", "-C", "PREROUTING", "-j", "MOSCTL_RESCUE")
	if err := checkCmd.Run(); err != nil {
		// 不存在则添加
		_ = runCommand("iptables", "-t", "nat", "-I", "PREROUTING", "1", "-j", "MOSCTL_RESCUE")
	}

	// 5. 添加特定的 MASQUERADE 规则，只针对发往救援 DNS 的流量
	// 先创建 POSTROUTING 专用链
	_ = runCommand("iptables", "-t", "nat", "-N", "MOSCTL_RESCUE_POST")
	_ = runCommand("iptables", "-t", "nat", "-F", "MOSCTL_RESCUE_POST")
	_ = runCommand("iptables", "-t", "nat", "-A", "MOSCTL_RESCUE_POST", "-d", "223.5.5.5", "-j", "MASQUERADE")

	// 挂载到 POSTROUTING
	checkPostCmd := exec.Command("iptables", "-t", "nat", "-C", "POSTROUTING", "-j", "MOSCTL_RESCUE_POST")
	if err := checkPostCmd.Run(); err != nil {
		_ = runCommand("iptables", "-t", "nat", "-I", "POSTROUTING", "1", "-j", "MOSCTL_RESCUE_POST")
	}

	fmt.Println("✅ 救援模式已开启！DNS 流量已接管。")
	return nil
}

// DisableRescue 关闭救援模式
func DisableRescue() error {
	fmt.Println("🛡️  正在关闭救援模式...")

	// 1. 从主链卸载自定义链
	_ = runCommand("iptables", "-t", "nat", "-D", "PREROUTING", "-j", "MOSCTL_RESCUE")
	_ = runCommand("iptables", "-t", "nat", "-D", "POSTROUTING", "-j", "MOSCTL_RESCUE_POST")

	// 2. 清空并删除自定义链
	_ = runCommand("iptables", "-t", "nat", "-F", "MOSCTL_RESCUE")
	_ = runCommand("iptables", "-t", "nat", "-X", "MOSCTL_RESCUE")
	
	_ = runCommand("iptables", "-t", "nat", "-F", "MOSCTL_RESCUE_POST")
	_ = runCommand("iptables", "-t", "nat", "-X", "MOSCTL_RESCUE_POST")

	fmt.Println("✅ 救援模式已关闭，恢复正常操作。")
	return nil
}

// runCommand 简单的命令封装
func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed: %v\nOutput: %s", name, err, string(output))
	}
	return nil
}
