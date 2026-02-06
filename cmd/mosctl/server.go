package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
)

// 全局变量用于接收命令行参数
var (
	serverPort string
	serverUser string
	serverPass string
)

// serverCmd 定义 server 子命令
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the Web API server",
	Run: func(cmd *cobra.Command, args []string) {
		runServer()
	},
}

func init() {
	// 注册参数，设置默认值
	serverCmd.Flags().StringVarP(&serverPort, "port", "P", ":8989", "Port to listen on")
	serverCmd.Flags().StringVarP(&serverUser, "user", "u", "admin", "Basic Auth Username")
	serverCmd.Flags().StringVarP(&serverPass, "pass", "p", "password", "Basic Auth Password")
	
	// 将 server 子命令添加到 root 命令中
	rootCmd.AddCommand(serverCmd)
}

// 辅助函数：执行 Shell 命令 (调用 mosctl 自身)
func runShellCommand(args ...string) (string, error) {
	cmd := exec.Command("mosctl", args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func runServer() {
	// 设置 Gin 为发布模式，减少控制台噪音
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	// 配置 CORS (允许跨域)
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	config.AllowCredentials = true
	config.AddAllowHeaders("Authorization")
	r.Use(cors.New(config))

	// 配置 Basic Auth 认证
	auth := gin.BasicAuth(gin.Accounts{
		serverUser: serverPass,
	})

	fmt.Printf("🚀 MosDNS Web Backend running at %s (User: %s)\n", serverPort, serverUser)

	api := r.Group("/api", auth)
	{
		// 1. 验证接口
		api.GET("/auth", func(c *gin.Context) {
			c.JSON(200, gin.H{"msg": "Authorized", "user": serverUser})
		})

		// 2. 状态统计接口
		api.GET("/stats", func(c *gin.Context) {
			// 服务状态
			statusOut, _ := exec.Command("systemctl", "is-active", "mosdns").CombinedOutput()
			status := strings.TrimSpace(string(statusOut))

			// 内存占用 (RSS)
			memOut, _ := exec.Command("bash", "-c", "ps -o rss= -p $(pidof mosdns) || echo 0").CombinedOutput()
			memKb := strings.TrimSpace(string(memOut))

			// 版本信息 (调用 mosctl version)
			verOut, _ := runShellCommand("version")

			// [优化] 日志大小统计 (代替 wc -l)
			logFile := "/var/log/mosdns.log"
			var logSize int64 = 0
			if info, err := os.Stat(logFile); err == nil {
				logSize = info.Size()
			}
			
			// 运行时间
			uptimeOut, _ := exec.Command("bash", "-c", "ps -p $(pidof mosdns) -o etime= || echo '00:00'").CombinedOutput()
			
			c.JSON(200, gin.H{
				"status":         status,
				"memory_kb":      memKb,
				"version":        strings.TrimSpace(verOut),
				"log_size_bytes": logSize, // 前端显示时再转换为 MB/GB
				"uptime":         strings.TrimSpace(string(uptimeOut)),
				"server_time":    time.Now().Format("15:04:05"),
			})
		})

		// 3. 读取日志接口
		api.GET("/logs", func(c *gin.Context) {
			// 读取最后 100 行日志
			out, err := exec.Command("tail", "-n", "100", "/var/log/mosdns.log").CombinedOutput()
			if err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
			c.JSON(200, gin.H{"logs": string(out)})
		})

		// 4. 执行操作接口
		api.POST("/action", func(c *gin.Context) {
			action := c.Query("cmd")
			val := c.Query("val")

			var args []string
			switch action {
			case "restart":
				// 异步重启，防止卡住 HTTP 请求
				go func() {
					time.Sleep(100 * time.Millisecond)
					exec.Command("systemctl", "restart", "mosdns").Run()
				}()
				c.JSON(200, gin.H{"msg": "正在重启服务..."})
				return
			case "set_ttl":
				if val == "" {
					c.JSON(400, gin.H{"msg": "缺少参数 val"})
					return
				}
				args = []string{"cache-ttl", val}
			case "test_dns":
				args = []string{"test"}
			case "update_geo":
				args = []string{"update"}
			case "flush_cache":
				args = []string{"flush"}
			case "sync_config":
				args = []string{"sync"}
			case "rescue_on":
				args = []string{"rescue", "enable"}
			case "rescue_off":
				args = []string{"rescue", "disable"}
			default:
				c.JSON(400, gin.H{"msg": "未知指令"})
				return
			}

			// 调用 mosctl 自身的命令逻辑
			output, err := runShellCommand(args...)
			if err != nil {
				c.JSON(500, gin.H{"msg": "执行出错", "output": string(output)})
				return
			}
			
			// 清理 ANSI 颜色代码，防止前端显示乱码
			cleanOutput := strings.ReplaceAll(string(output), "\x1b[0;32m", "") // 去除绿色
			cleanOutput = strings.ReplaceAll(cleanOutput, "\x1b[0m", "")     // 去除重置符
			
			c.JSON(200, gin.H{"msg": "执行成功", "output": cleanOutput})
		})
	}

	// 启动服务
	if err := r.Run(serverPort); err != nil {
		fmt.Printf("❌ Server start failed: %v\n", err)
		os.Exit(1)
	}
}
