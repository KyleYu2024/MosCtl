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
	fmt.Println("⬇️  正在更新 GeoSite/GeoIP...")

	// 确保目录存在
	os.MkdirAll("/etc/mosdns/rules", 0755)

	ghProxy := "https://gh-proxy.com/"

	files := map[string]string{
		ghProxy + "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/direct-list.txt": "/etc/mosdns/rules/geosite_cn.txt",
		ghProxy + "https://raw.githubusercontent.com/Loyalsoldier/geoip/release/text/cn.txt":              "/etc/mosdns/rules/geoip_cn.txt",
		ghProxy + "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/apple-cn.txt":    "/etc/mosdns/rules/geosite_apple.txt",
		ghProxy + "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/proxy-list.txt":   "/etc/mosdns/rules/geosite_no_cn.txt",
	}

	for url, path := range files {
		fmt.Printf("Downloading %s ...\n", path)
		if err := service.DownloadFile(url, path); err != nil {
			fmt.Printf("❌ 下载失败 %s: %v\n", path, err)
		}
	}

	// 重启 MosDNS
	fmt.Println("🔄 重启 MosDNS 服务...")
	if err := service.RestartService(); err != nil {
		fmt.Printf("❌ 重启失败: %v\n", err)
	} else {
		fmt.Println("✅ 规则更新完毕！")
	}
}
