package main

import (
	"fmt"
	"os"

	"github.com/KyleYu2024/mosctl/internal/service"
	"github.com/spf13/cobra"
)

// updateCmd 代表 update 命令
var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update GeoIP and GeoSite rules",
	Run: func(cmd *cobra.Command, args []string) {
		UpdateGeoRules()
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)
}

func UpdateGeoRules() {
	fmt.Println("⬇️  正在执行计划内 GeoSite/GeoIP 更新...")

	os.MkdirAll("/etc/mosdns/rules", 0755)

	ghProxy := "https://gh-proxy.com/"
	files := map[string]string{
		ghProxy + "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/direct-list.txt": "/etc/mosdns/rules/geosite_cn.txt",
		ghProxy + "https://raw.githubusercontent.com/Loyalsoldier/geoip/release/text/cn.txt":              "/etc/mosdns/rules/geoip_cn.txt",
		ghProxy + "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/apple-cn.txt":    "/etc/mosdns/rules/geosite_apple.txt",
		ghProxy + "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/proxy-list.txt":   "/etc/mosdns/rules/geosite_no_cn.txt",
	}

	anySuccess := false
	for url, path := range files {
		if err := service.DownloadFile(url, path); err != nil {
			fmt.Printf("⚠️  下载失败 %s: %v (将跳过该文件)\n", path, err)
		} else {
			anySuccess = true
		}
	}

	if anySuccess {
		fmt.Println("🔄 规则已更新，正在通过 killall 重启内核...")
		service.RestartService()
	} else {
		fmt.Println("❌ 更新全部失败，保持当前版本继续运行。")
	}
}
