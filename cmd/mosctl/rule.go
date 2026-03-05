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
	Short: "Manage custom rules",
	Long:  `Add domains or IPs to custom lists (Force CN, Force NoCN, IoT).`,
}

// ruleAddCmd 是 'rule add' 子命令
var ruleAddCmd = &cobra.Command{
	Use:   "add <domain_or_ip>",
	Short: "Add a rule",
	Example: `  mosctl rule add example.com --direct   # Force Domestic
  mosctl rule add google.com --proxy     # Force Foreign
  mosctl rule add 10.10.1.0/25 --iot     # Smart Home Bypass`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		content := args[0]

		// 互斥检查
		count := 0
		if flagDirect { count++ }
		if flagProxy { count++ }
		if flagIot { count++ }

		if count != 1 {
			fmt.Println("❌ 错误: 请明确指定且仅指定一种类型: --direct (-d), --proxy (-p) 或 --iot (-i)")
			cmd.Usage()
			os.Exit(1)
		}

		var err error
		if flagDirect {
			err = rule.AddRule(content, rule.TypeForceCN)
		} else if flagProxy {
			err = rule.AddRule(content, rule.TypeForceNoCN)
		} else if flagIot {
			err = rule.AddRule(content, rule.TypeIoT)
		}

		if err != nil {
			fmt.Printf("❌ 添加失败: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	// 注册参数
	ruleAddCmd.Flags().BoolVarP(&flagDirect, "direct", "d", false, "Add to Force CN list (Domestic)")
	ruleAddCmd.Flags().BoolVarP(&flagProxy, "proxy", "p", false, "Add to Force NoCN list (Foreign)")
	ruleAddCmd.Flags().BoolVarP(&flagIot, "iot", "i", false, "Add to IoT source bypass list (Smart Home)")

	ruleCmd.AddCommand(ruleAddCmd)
	rootCmd.AddCommand(ruleCmd)
}
