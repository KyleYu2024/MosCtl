package service

import (
	"fmt"
	"os/exec"
)

// 救援 DNS：当 MosDNS 挂掉时，流量将被劫持到这里
const RescueDNS = "223.5.5.5:53"

// EnableRescue 开启救援模式
// 逻辑：开启内核转发 -> 清空旧 NAT -> 劫持 53 端口到 223.5.5.5 -> 开启伪装
func EnableRescue() error {
	fmt.Println("🚑 正在启动救援模式 (Failover to 223.5.5.5)...")

	// 1. 开启 IPv4 转发 (必须，否则包发不出去)
	if err := runCommand("sysctl", "-w", "net.ipv4.ip_forward=1"); err != nil {
		return fmt.Errorf("无法开启内核转发: %v", err)
	}

	// 2. 清理旧规则 (防止重复添加导致混乱)
	_ = runCommand("iptables", "-t", "nat", "-F", "PREROUTING")
	_ = runCommand("iptables", "-t", "nat", "-F", "POSTROUTING")

	// 3. 添加 DNAT 规则 (把发往本机的 UDP 53 端口流量，改写目的地为 223.5.5.5)
	// command: iptables -t nat -A PREROUTING -p udp --dport 53 -j DNAT --to-destination 223.5.5.5:53
	err := runCommand("iptables", "-t", "nat", "-A", "PREROUTING", 
		"-p", "udp", "--dport", "53", 
		"-j", "DNAT", "--to-destination", RescueDNS)
	if err != nil {
		return fmt.Errorf("无法设置 DNAT 规则: %v", err)
	}

	// 4. 添加 Masquerade 规则 (确保回包能正确找到回家的路)
	// command: iptables -t nat -A POSTROUTING -j MASQUERADE
	err = runCommand("iptables", "-t", "nat", "-A", "POSTROUTING", "-j", "MASQUERADE")
	if err != nil {
		return fmt.Errorf("无法设置 Masquerade 规则: %v", err)
	}

	fmt.Println("✅ 救援模式已开启！DNS 流量已接管。")
	return nil
}

// DisableRescue 关闭救援模式
// 逻辑：直接清空 NAT 表的 PREROUTING 和 POSTROUTING 链
func DisableRescue() error {
	// 简单粗暴但有效：直接清空 NAT 表相关链
	// 注意：如果你这台机器上还有 Docker 等其他依赖 NAT 的服务，这种清空方式可能会有副作用。
	// 但对于专门跑 MosDNS 的 LXC 来说，这是最干净的。
	
	if err := runCommand("iptables", "-t", "nat", "-F", "PREROUTING"); err != nil {
		return err
	}
	if err := runCommand("iptables", "-t", "nat", "-F", "POSTROUTING"); err != nil {
		return err
	}

	// 顺便把转发关了也可以，不关也行，为了省电/安全可以关掉
	// _ = runCommand("sysctl", "-w", "net.ipv4.ip_forward=0")

	// 只有在非 Systemd 自动调用时才打印，保持日志清爽
	// fmt.Println("🛡️  救援模式已关闭，恢复正常。")
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
