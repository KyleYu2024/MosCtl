package main

import (
	"fmt"
	"os"

	"github.com/KyleYu2024/mosctl/internal/service"
	"github.com/spf13/cobra"
)

// rescueCmd 父命令
var rescueCmd = &cobra.Command{
	Use:   "rescue",
	Short: "Manage rescue mode (iptables failover)",
	Long:  `Control the emergency failover mode. When enabled, all DNS traffic (UDP 53) is forwarded to 223.5.5.5 using iptables NAT.`,
}

// rescueEnableCmd 开启
var rescueEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Turn ON rescue mode (Forward to 223.5.5.5)",
	Run: func(cmd *cobra.Command, args []string) {
		if err := service.EnableRescue(); err != nil {
			fmt.Printf("❌ 开启失败: %v\n", err)
			os.Exit(1)
		}
	},
}

// rescueDisableCmd 关闭
var rescueDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Turn OFF rescue mode (Restore normal operation)",
	Run: func(cmd *cobra.Command, args []string) {
		if err := service.DisableRescue(); err != nil {
			fmt.Printf("❌ 关闭失败: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	// 组装命令: root -> rescue -> [enable, disable]
	rescueCmd.AddCommand(rescueEnableCmd)
	rescueCmd.AddCommand(rescueDisableCmd)
	rootCmd.AddCommand(rescueCmd)
}
