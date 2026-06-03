package main

import (
	"context"
	"log"
	"os"

	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/engine"
	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/provider"
	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/schema"
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
	// // 通过命令行参数接收用户的 prompt
	// promptPtr := flag.String("prompt", "", "要交给 Agent 执行的任务描述")
	// flag.Parse()

	// if *promptPtr == "" {
	// 	fmt.Println("用法：go run cmd/claw/main.go -prompt \"你的任务命令\"")
	// 	os.Exit(1)
	// }

	// 确保已设置 环境变量
	if os.Getenv("ZHIPU_API_KEY") == "" {
		log.Fatal("请先导出 ZHIPU_API_KEY 环境变量")
	}

		// 1. 初始化引擎依赖
	workDir, _ := os.Getwd()
	workDir += "/workspace"

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

	// 3. 实例化核心引擎，开启 EnableThinking 慢思考模式、开启计划模式 (PlanMode=true)
	eng := engine.NewAgentEngine(llmProvider, registry, false, false)

	// 注入新实现的终端输出器
	reporter := engine.NewTerminalReporter()

	sessionID := "test_recovery_001"
	sess := engine.GlobalSessionMgr.GetOrCreate(sessionID, workDir)

	// 这是一个巨大的陷阱指令：
  // 我们不给它查看文件的机会，直接命令它凭初始上下文去修改文件，目的是诱发 old_text 不匹配的错误。
	prompt := `
	我当前目录下有一个 auth.go 文件。 
	请修改 auth.go 中的 login 函数。
	请直接使用 edit_file 工具替换下面的代码块，将判断条件改为同时允许"admin"、"root"和"guest"三种用户登录： 
	
	// 鉴权入口函数
	func login(user string) bool {
			// 检查用户名
			if user == "admin" {
					return true
			}
			return false
	}
`


	log.Println("\n>>> 🚀 启动自愈测试任务...")
	sess.Append(schema.Message{Role: schema.RoleUser, Content: prompt})

	err := eng.Run(context.Background(), sess, reporter)
	if err != nil { 
		log.Fatalf("引擎运行崩溃: %v", err) 
	}

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

	// // 唤醒引擎执行
	// err := eng.Run(context.Background(), sess, reporter)
	// if err != nil {
	// 	log.Fatalf("引擎运行崩溃: %v", err)
	// }

	// log.Println("架构蓝图搭建完毕，等待各核心模块注入！")
}
