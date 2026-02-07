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
	flagIot    bool
)

// ruleCmd 是父命令
var ruleCmd = &cobra.Command{
	Use:   "rule",
	Short: "Manage local rules (whitelist/blocklist)",
	Long:  `Add or remove domains/IPs from local rules.`,
}

// ruleAddCmd 是 'rule add' 子命令
var ruleAddCmd = &cobra.Command{
	Use:   "add <domain_or_ip>",
	Short: "Add a domain or IP to local rules",
	Example: `  mosctl rule add example.com --direct
  mosctl rule add google.com --proxy
  mosctl rule add 10.10.1.0/25 --iot`,
	Args: cobra.ExactArgs(1), // 必须强制输入一个参数
	Run: func(cmd *cobra.Command, args []string) {
		content := args[0]

		// 互斥检查：必须且只能选一个
		count := 0
		if flagDirect { count++ }
		if flagProxy { count++ }
		if flagIot { count++ }

		if count != 1 {
			fmt.Println("❌ 错误: 请明确指定且仅指定一种类型: --direct (-d), --proxy (-p) 或 --iot (-i)")
			cmd.Usage()
			os.Exit(1)
		}

		// 调用后端逻辑
		if err := rule.AddRule(content, flagDirect, flagIot); err != nil {
			fmt.Printf("❌ 添加失败: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	// 注册参数
	ruleAddCmd.Flags().BoolVarP(&flagDirect, "direct", "d", false, "Add to local whitelist (Direct)")
	ruleAddCmd.Flags().BoolVarP(&flagProxy, "proxy", "p", false, "Add to local blocklist (Proxy)")
	ruleAddCmd.Flags().BoolVarP(&flagIot, "iot", "i", false, "Add to IoT source bypass list (Smart Home)")

	// 组装命令树: root -> rule -> add
	ruleCmd.AddCommand(ruleAddCmd)
	rootCmd.AddCommand(ruleCmd)
}
