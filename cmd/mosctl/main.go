package main

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"
)

// rootCmd 代表没有子命令时的基础命令
var rootCmd = &cobra.Command{
    Use:   "mosctl",
    Short: "MosCtl: The Guardian of MosDNS",
    Long:  `MosCtl is a CLI tool for managing MosDNS configurations, rules, and rescue modes in a GitOps way.`,
    Run: func(cmd *cobra.Command, args []string) {
        // 如果用户只输入 mosctl，打印帮助信息
        cmd.Help()
    },
}

func main() {
    if err := rootCmd.Execute(); err != nil {
        fmt.Println(err)
        os.Exit(1)
    }
}
