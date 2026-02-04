package main

import (
	"fmt"
	"os"

	"github.com/KyleYu2024/mosctl/internal/rule"
	"github.com/spf13/cobra"
)

var (
	flagDirect bool
	flagProxy  bool
)

// ruleCmd 是父命令
var ruleCmd = &cobra.Command{
	Use:   "rule",
	Short: "Manage local rules (whitelist/blocklist)",
	Long:  `Add or remove domains from local_direct.txt or local_proxy.txt.`,
}

// ruleAddCmd 是 'rule add' 子命令
var ruleAddCmd = &cobra.Command{
	Use:   "add <domain>",
	Short: "Add a domain to local rules",
	Example: `  mosctl rule add example.com --direct
  mosctl rule add google.com --proxy`,
	Args: cobra.ExactArgs(1), // 必须强制输入一个域名
	Run: func(cmd *cobra.Command, args []string) {
		domain := args[0]

		// 互斥检查：不能既不选也不选，也不能两个都选
		if flagDirect == flagProxy {
			fmt.Println("❌ 错误: 请明确指定 --direct (直连) 或 --proxy (代理)，且不能同时指定。")
			cmd.Usage()
			os.Exit(1)
		}

		// 调用后端逻辑
		if err := rule.AddRule(domain, flagDirect); err != nil {
			fmt.Printf("❌ 添加失败: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	// 注册参数
	// -d 简写对应 --direct
	ruleAddCmd.Flags().BoolVarP(&flagDirect, "direct", "d", false, "Add to local whitelist (Direct)")
	// -p 简写对应 --proxy
	ruleAddCmd.Flags().BoolVarP(&flagProxy, "proxy", "p", false, "Add to local blocklist (Proxy)")

	// 组装命令树: root -> rule -> add
	ruleCmd.AddCommand(ruleAddCmd)
	rootCmd.AddCommand(ruleCmd)
}
