package main

import (
	"context"
	"log"
	"os"

	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/engine"
	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/provider"
	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/tools"
	"github.com/joho/godotenv"
)

func init() {
	godotenv.Load() // 加载 .env
}

// =================================================================
// 3. 组装运行
// =================================================================
func main() {
	// 1. 初始化引擎依赖
	workDir, _ := os.Getwd()
	workDir += "/workspace" // 锁定工作区，所有工具操作都必须在这个目录下进行

	// 确保已设置 环境变量
	if os.Getenv("ZHIPU_API_KEY") == "" {
		log.Fatal("请先导出 ZHIPU_API_KEY 环境变量")
	}

	// 2. 初始化真实的 Provider 大脑（指向智谱 GLM-4.5）
	// 这里可以任意切换 NewZhipuClaudeProvider、NewZhipuOpenAIProvider，效果完全一致
	llmProvider := provider.NewZhipuClaudeProvider("glm-4.5-air")

	// 2. 初始化真实的 Tool Registry
	registry := tools.NewRegistry()

	// 将真实的工具挂载到注册表中
	registry.Register(tools.NewReadFileTool(workDir))
	registry.Register(tools.NewWriteFileTool(workDir))
	registry.Register(tools.NewBashTool(workDir))
	registry.Register(tools.NewEditFileTool(workDir))

	// 3. 实例化核心引擎，开启 EnableThinking 慢思考模式
	eng := engine.NewAgentEngine(llmProvider, registry, workDir, true)

	// 注入新实现的终端输出器
	reporter := engine.NewTerminalReporter()

	prompt := `
	我需要在当前目录下新建一个 ping.go，提供一个简单的 http ping 接口。
	写完之后，帮我把代码用 git 提交一下。
	`

	// // 4. 初始化飞书 Bot 调度器
	// bot := feishu.NewFeishuBot(eng)
	// handler := httpserverext.NewEventHandlerFunc(bot.GetEventDispatcher())

	// // 5. 注册路由并启动 HTTP 服务
	// http.HandleFunc("/webhook/event", handler)

	// port := ":48080"
	// log.Printf("🚀 go-tiny-claw 飞书服务端已启动，正在监听 %s 端口\n", port)

	// // 发起任务指令
	// err := http.ListenAndServe(port, nil)

	// if err != nil {
	// 	log.Fatalf("服务器启动失败: %v", err)
	// }

	err := eng.Run(context.Background(), prompt, reporter)
	if err != nil {
		log.Fatalf("引擎运行失败: %v", err)
	}

	// log.Println("架构蓝图搭建完毕，等待各核心模块注入！")
}
