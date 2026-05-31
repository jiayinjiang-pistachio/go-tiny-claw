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
	// 确保已设置 环境变量
	if os.Getenv("ZHIPU_API_KEY") == "" {
		log.Fatal("请先导出 ZHIPU_API_KEY 环境变量")
	}

	// 获取当前执行目录作为 WorkDir 物理边界
	workDir, _ := os.Getwd()

	// 1. 初始化真实的 Provider 大脑（指向智谱 GLM-4.5）
	// 这里可以任意切换 NewZhipuClaudeProvider、NewZhipuOpenAIProvider，效果完全一致
	llmProvider := provider.NewZhipuClaudeProvider("glm-4.5-air")

	// 2. 初始化真实的 Tool Registry
	registry := tools.NewRegistry()

	// 将真实的工具挂载到注册表中
	registry.Register(tools.NewReadFileTool(workDir))
	registry.Register(tools.NewWriteFileTool(workDir))
	registry.Register(tools.NewBashTool(workDir))

	// 3. 实例化核心引擎，开启 EnableThinking 慢思考模式
	eng := engine.NewAgentEngine(llmProvider, registry, workDir, false)

	// 发起一个需要连贯物理动作的任务
	prompt := `
	请帮我执行以下操作：
	1. 用 bash 查看一下我当前电脑的 Go 版本。
	2. 帮我写一个简单的 helloworld.go 文件，输出 "Hello, go-tiny-claw"。
	3. 用 bash 编译并运行这个 go 文件，确认它能正常工作。
	`

	// 发起任务指令
	err := eng.Run(context.Background(), prompt)

	if err != nil {
		log.Fatalf("引擎奔溃：%v", err)
	}

	// log.Println("架构蓝图搭建完毕，等待各核心模块注入！")
}
