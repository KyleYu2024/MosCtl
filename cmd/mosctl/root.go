package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "mosctl",
	Short: "MosCtl - A management tool for MosDNS (Docker Native)",
	Run: func(cmd *cobra.Command, args []string) {
		os.Setenv("MOSCTL_MODE", "docker")
		runDockerPanel()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
