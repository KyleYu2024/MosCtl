package main

import (
	"fmt"
	"os"

	"github.com/KyleYu2024/mosctl/internal/config"
	"github.com/spf13/cobra"
)

// syncCmd 定义了 'sync' 命令
var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync config and rules from GitHub",
	Long:  `Download config.yaml, cloud_direct.txt, and cloud_proxy.txt from the cloud repository, verify them, and reload MosDNS.`,
	Run: func(cmd *cobra.Command, args []string) {
		// 调用 internal/config 里的 SyncConfig 函数
		if err := config.SyncConfig(); err != nil {
			fmt.Printf("❌ 同步失败: %v\n", err)
			os.Exit(1)
		}
	},
}

// init 函数会在 main.go 运行前自动执行，把 syncCmd 注册到 rootCmd 上
func init() {
	rootCmd.AddCommand(syncCmd)
}
