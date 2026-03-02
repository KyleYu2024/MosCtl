package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// versionCmd 代表 version 命令
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of MosCtl",
	Run: func(cmd *cobra.Command, args []string) {
		// 这里可以硬编码，也可以通过编译参数注入
		fmt.Println("0.4.3")
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
