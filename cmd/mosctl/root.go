package main

import (
	"os"

	"github.com/spf13/cobra"
)

// rootCmd 代表没有调用子命令时的基础命令
var rootCmd = &cobra.Command{
	Use:   "mosctl",
	Short: "MosDNS control tool and web manager",
	Long:  `MosCtl is a CLI tool to manage MosDNS service, rules, and rescue modes.`,
	// 如果用户直接运行 'mosctl' 而不带参数，默认打印帮助信息
	// Run: func(cmd *cobra.Command, args []string) { }, 
}

// Execute 是 main.go 调用的入口
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
