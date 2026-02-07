package main

import (
	"fmt"
	"os"

	"github.com/KyleYu2024/mosctl/internal/config"
	"github.com/spf13/cobra"
)

var (
	isLocal bool
)

// flushCmd
var flushCmd = &cobra.Command{
	Use:   "flush",
	Short: "Flush DNS cache",
	Run: func(cmd *cobra.Command, args []string) {
		if err := config.FlushCache(); err != nil {
			fmt.Printf("❌ 清空失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ 缓存已清空并重启服务")
	},
}

// testCmd
var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Test DNS resolution",
	Run: func(cmd *cobra.Command, args []string) {
		config.RunTest()
	},
}

// cacheTtlCmd
var cacheTtlCmd = &cobra.Command{
	Use:   "cache-ttl <seconds>",
	Short: "Set lazy_cache_ttl",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := config.SetCacheTTL(args[0]); err != nil {
			fmt.Printf("❌ 设置失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ 缓存时间已更新并重启服务")
	},
}

// upstreamCmd
var upstreamCmd = &cobra.Command{
	Use:   "upstream <address>",
	Short: "Set upstream DNS address",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := config.SetUpstream(isLocal, args[0]); err != nil {
			fmt.Printf("❌ 设置失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ 上游 DNS 已更新并重启服务")
	},
}

func init() {
	upstreamCmd.Flags().BoolVarP(&isLocal, "local", "l", false, "Set local upstream (default is remote)")

	rootCmd.AddCommand(flushCmd)
	rootCmd.AddCommand(testCmd)
	rootCmd.AddCommand(cacheTtlCmd)
	rootCmd.AddCommand(upstreamCmd)
}