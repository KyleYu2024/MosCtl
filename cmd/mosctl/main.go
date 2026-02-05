package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// =================配置区=================
const (
	USER     = "admin"
	PASSWORD = "password" // 以后你可以改这里
	PORT     = ":8989"    // 面板运行端口
)
// =======================================

func runCommand(args ...string) (string, error) {
	cmd := exec.Command("mosctl", args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func main() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	config.AllowCredentials = true
	config.AddAllowHeaders("Authorization")
	r.Use(cors.New(config))

	auth := gin.BasicAuth(gin.Accounts{
		USER: PASSWORD,
	})

	api := r.Group("/api", auth)
	{
		api.GET("/auth", func(c *gin.Context) {
			c.JSON(200, gin.H{"msg": "Authorized", "user": USER})
		})

		api.GET("/stats", func(c *gin.Context) {
			statusOut, _ := exec.Command("systemctl", "is-active", "mosdns").CombinedOutput()
			status := strings.TrimSpace(string(statusOut))

			memOut, _ := exec.Command("bash", "-c", "ps -o rss= -p $(pidof mosdns) || echo 0").CombinedOutput()
			memKb := strings.TrimSpace(string(memOut))

			verOut, _ := runCommand("version")

			logCountOut, _ := exec.Command("bash", "-c", "wc -l < /var/log/mosdns.log || echo 0").CombinedOutput()
			logCount := strings.TrimSpace(string(logCountOut))

			uptimeOut, _ := exec.Command("bash", "-c", "ps -p $(pidof mosdns) -o etime= || echo '00:00'").CombinedOutput()
			
			c.JSON(200, gin.H{
				"status":      status,
				"memory_kb":   memKb,
				"version":     strings.TrimSpace(verOut),
				"queries":     logCount,
				"uptime":      strings.TrimSpace(string(uptimeOut)),
				"server_time": time.Now().Format("15:04:05"),
			})
		})

		api.GET("/logs", func(c *gin.Context) {
			out, err := exec.Command("tail", "-n", "100", "/var/log/mosdns.log").CombinedOutput()
			if err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
			c.JSON(200, gin.H{"logs": string(out)})
		})

		api.POST("/action", func(c *gin.Context) {
			action := c.Query("cmd")
			val := c.Query("val")

			var args []string
			switch action {
			case "restart":
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

			output, err := runCommand(args...)
			if err != nil {
				c.JSON(500, gin.H{"msg": "执行出错", "output": string(output)})
				return
			}
			cleanOutput := strings.ReplaceAll(string(output), "\x1b[0;32m", "")
			cleanOutput = strings.ReplaceAll(cleanOutput, "\x1b[0m", "")
			c.JSON(200, gin.H{"msg": "执行成功", "output": cleanOutput})
		})
	}

	fmt.Printf("MosDNS Web Backend listening on %s\n", PORT)
	r.Run(PORT)
}
