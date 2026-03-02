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

	anyUpdated := false
	for url, path := range files {
		updated, err := service.DownloadFile(url, path)
		if err != nil {
			fmt.Printf("⚠️  下载失败 %s: %v (将跳过该文件)\n", path, err)
		} else if updated {
			anyUpdated = true
		}
	}

	if anyUpdated {
		fmt.Println("🎉 规则文件已更新。")
		fmt.Println("💡 提示: 系统检测到规则变动，将在几秒内自动重启内核以应用更改。")
	} else {
		fmt.Println("✅ 规则已是最新，无需更新。")
	}
}
