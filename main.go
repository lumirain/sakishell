package main

import (
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"sakishell/mcp"

	service "sakishell/app"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// headless 模式：sakishell mcp --stdio
	// 供外部 AI 客户端以"添加 MCP server"方式 spawn 本程序，不开 GUI
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		if err := mcp.RunCLI(os.Args[2:]); err != nil {
			log.Printf("MCP 服务退出: %v", err)
			os.Exit(1)
		}
		return
	}

	// 初始化应用服务（连接配置存储 + SSH 会话池 + 主机密钥存储）
	appService, err := service.NewApp("")
	if err != nil {
		log.Fatalf("初始化应用服务失败: %v", err)
	}

	// 创建 Wails 应用
	err = wails.Run(&options.App{
		Title:     "SakiShell",
		Width:     1200,
		Height:    800,
		MinWidth:  900,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 30, G: 30, B: 30, A: 1},
		OnStartup:        appService.Startup,
		OnShutdown:       appService.Shutdown,
		Bind: []interface{}{
			appService,
		},
	})
	if err != nil {
		log.Printf("应用运行出错: %v", err)
	}
}
