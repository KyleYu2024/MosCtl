package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "mosctl",
	Short: "MosCtl - MosDNS process and Web management",
	Run: func(cmd *cobra.Command, args []string) {
		runSupervisor()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
