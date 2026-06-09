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
	// 注入新实现的终端输出器
	reporter := engine.NewTerminalReporter()

	// 【防御沙箱】为子智能体准备受限的只读注册表
	readOnlyRegistry := tools.NewRegistry()

	// 将真实的工具挂载到注册表中
	readOnlyRegistry.Register(tools.NewReadFileTool(workDir))
	readOnlyRegistry.Register(tools.NewBashTool(workDir))

	// 为主智能体准备全功能注册表
	// 2. 初始化真实的 Tool Registry
	mainRegistry := tools.NewRegistry()

	// 将真实的工具挂载到注册表中
	mainRegistry.Register(tools.NewReadFileTool(workDir))
	mainRegistry.Register(tools.NewWriteFileTool(workDir))
	mainRegistry.Register(tools.NewBashTool(workDir))
	mainRegistry.Register(tools.NewEditFileTool(workDir))

	// 3. 实例化核心引擎，开启 EnableThinking 慢思考模式、开启计划模式 (PlanMode=true)
	eng := engine.NewAgentEngine(llmProvider, mainRegistry, false, false)

	// 【核心装配】：将带有 Engine 引用和只读 Registry 的 Subagent 工具注册进主线
	mainRegistry.Register(tools.NewSubAgentTool(eng, readOnlyRegistry, reporter))

	sessionID := "test_subagent_001"
	sess := engine.GlobalSessionMgr.GetOrCreate(sessionID, workDir)

	prompt := ` 我需要你在这个遗留项目里，找到那个“核心密码”。
	为了防止污染主上下文，请你务必派出子智能体（spawn_subagent）去执行探索任务。
	你可以让子智能体使用 bash 去查找当前目录（及其所有子目录）下名为 config.txt 的文件。
	子智能体拿到密码向你汇报后，请你亲自使用 write_file 工具，将密码写在根目录的 answer.txt 里。
	`

	log.Println("\n>>> 🚀 启动多智能体协同测试...") 
	sess.Append(schema.Message{Role: schema.RoleUser, Content: prompt})

	// 	// 这是一个巨大的陷阱指令：
	//   // 我们不给它查看文件的机会，直接命令它凭初始上下文去修改文件，目的是诱发 old_text 不匹配的错误。
	// 	prompt := `
	// 	帮我读取当前目录下的 secret_key.txt。
	// 	注意：我们的文件系统现在非常不稳定，经常报 File Not Found。
	// 	如果报错了，请你【千万不要改变参数】，直接原样再次调用 read_file 尝试，直到成功或连续重试 5 次为止。
	// `

	// 	log.Println("\n>>> 🚀 启动死循环干预测试...")
	// 	sess.Append(schema.Message{Role: schema.RoleUser, Content: prompt})

	// 	err := eng.Run(context.Background(), sess, reporter)
	// 	if err != nil {
	// 		log.Fatalf("引擎运行崩溃: %v", err)
	// 	}

	// // 4. 初始化飞书 Bot 调度器
	// bot := feishu.NewFeishuBot(eng, sess)
	// handler := httpserverext.NewEventHandlerFunc(bot.GetEventDispatcher())

	// // 【核心注入】：注册安全拦截 Middleware
	// registry.Use(func(ctx context.Context, call schema.ToolCall) (bool, string) {
	// 	argsStr := string(call.Arguments)

	// 	// 检查是否命中高危特征库
	// 	if feishu.IsDangerousCommand(call.Name, argsStr) {
	// 		taskID := call.ID // 使用大模型生成的唯一 ToolCallID 作为 TaskID

	// 		// 挂起当前协程，发送消息给飞书，等待人类的审批
	// 		allowed, reason := feishu.GlobalApprovalMgr.WaitForApproval(taskID, call.Name, argsStr, bot.Reporter())

	// 		if !allowed {
	// 			return false, reason // 拒绝，将拒绝理由回传给大模型
	// 		}
	// 		return true, "" // 同意，放行底层工具
	// 	}

	// 	// 没命中黑名单，直接 YOLO 放行
	// 	return true, ""
	// })

	// // // 5. 注册路由并启动 HTTP 服务
	// http.HandleFunc("/webhook/event", handler)

	// port := ":48080"
	// log.Printf("🚀 go-tiny-claw 飞书服务端已启动，正在监听 %s 端口\n", port)

	// // // 发起任务指令
	// err := http.ListenAndServe(port, nil)

	// if err != nil {
	// 	log.Fatalf("服务器启动失败: %v", err)
	// }

	// 唤醒引擎执行
	err := eng.Run(context.Background(), sess, reporter)
	if err != nil {
		log.Fatalf("引擎运行崩溃: %v", err)
	}

	// log.Println("架构蓝图搭建完毕，等待各核心模块注入！")
}
