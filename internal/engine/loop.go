package engine

import (
	"context"
	"fmt"
	"log"

	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/provider"
	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/schema"
	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/tools"
)

// AgentEngine 是微型 OS 的核心驱动
type AgentEngine struct {
	provider provider.LLMProvider
	registry tools.Registry

	// workDir 工作区：借鉴 OpenClaw 的理念，Agent 必须有一个明确的物理边界
	workDir string
	enableThinking bool // 慢思考模式开关
}

func NewAgentEngine(p provider.LLMProvider, r tools.Registry, workDir string, enableThinking bool) *AgentEngine {
	return &AgentEngine{
		provider: p,
		registry: r,
		workDir: workDir,
		enableThinking: enableThinking, // 使用传入的参数初始化慢思考模式开关
	}
}

// Run 启动 Agent 的生命周期
func (e *AgentEngine) Run(ctx context.Context, userPrompt string) error {
	log.Printf("[Engine]引擎启动，锁定工作区：%s\n", e.workDir)
	log.Printf("[Engine] 慢思考模式（Thinking Phase）：%v\n", e.enableThinking)

	// 1. 初始化会话的 Context（上下文内存）
	// 在真实的场景中，这里会由动态的 Prompt 组装器加载 AGENTS.md，目前我们先硬编码
	contextHistory := []schema.Message{
		{
			Role: schema.RoleSystem,
			Content: "You are go-tiny-claw, an expert coding assistant. You have full access to tools in the workspace.",
		},
		{
			Role: schema.RoleUser,
			Content: userPrompt,
		},
	}

	turnCount := 0


	// 2. the main loop 心跳开始（标准的ReACT循环）
	for {
		turnCount++
		log.Printf("========== [Turn %d] 开始 ==========\n", turnCount)

		// 获取当前挂载的所有工具定义
		avaliableTools := e.registry.GetAvaliableTools()

		// =========================================================
		// 1. Phase 1: 慢思考阶段（Thinking）- 剥夺工具，强制规划
		// =========================================================
		if e.enableThinking {
			log.Println("[Engine][Phase 1] 剥夺工具访问权，强制进入慢思考与规划阶段...")

			// 核心机制：传入的 avaliableTools 为 nil
			// 大模型看不到任何 JSON Schema，被迫只能输出纯文本的思考过程
			thinkResp, err := e.provider.Generate(ctx, contextHistory, nil)	
			if err != nil {
				return fmt.Errorf("Thinking 阶段生成失败：%w", err)
			}

			// 如果模型输出了思考过程，我们将其作为 Assistant 消息追加到上下文
			if thinkResp.Content != "" {
				fmt.Printf("🧠 [内部思考 Trace]：%s\n", thinkResp.Content)
				contextHistory = append(contextHistory, *thinkResp)
			}
		}

		// =========================================================
		// 2. Phase 2: 行动阶段（Action）- 恢复工具，顺着规划执行
		// =========================================================
		log.Println("[Engine][Phase 2] 恢复工具挂载，等待模型采取行动...")

		// 此时的 contextHistory 中已经包含了上一阶段模型自己的 Thinking Trace
		// 模型会顺着自己的规划逻辑，综合恢复的 avaliableTools，发起精准的工具调用
		actionResp, err := e.provider.Generate(ctx, contextHistory, avaliableTools)
		if err != nil {
			return fmt.Errorf("Action 阶段生成失败：%w", err)
		}

		contextHistory = append(contextHistory, *actionResp)

		if actionResp.Content != "" {
			fmt.Printf("🤖 [对外回复]: %s\n", actionResp.Content)
		}

		// 3. 退出条件判断
		// 如果模型没有请求任何工具调用，说明它认为任务已经完成，跳出循环
		if len(actionResp.ToolCalls) == 0 {
			log.Println("[Engine] 任务完成，退出循环。")
			break
		}

		// 4. 执行行动（Action）与获取观察结果（Observation）
		log.Printf("[Engine] 模型请求调用 %d 个工具...\n", len(actionResp.ToolCalls))

		for _, toolCall := range actionResp.ToolCalls {
			log.Printf("-> 🛠️ 执行工具: %s, 参数: %s\n", toolCall.Name, string(toolCall.Arguments))

			// 通过 Registry 路由并执行底层工具
			result := e.registry.Execute(ctx, toolCall)

			if result.IsError {
				log.Printf(" -> ❌ 工具执行报错: %s\n", result.Output)
			} else {
				log.Printf(" -> ✅ 工具执行成功 (返回 %d 字节)\n", len(result.Output))
			}

			// 将工具执行的观察结果（Observation）封装为 UserMessage 追加到上下文中
			// 注意：ToolCallID 必须携带！这是维系大模型推理链条的关键
			observationMsg := schema.Message{
				Role: schema.RoleUser, // 因为工具的执行结果不是由大语言模型调用生成的，所以归为用户message比较合适
				Content: result.Output,
				ToolCallID: toolCall.ID,
			}
			contextHistory = append(contextHistory, observationMsg)
		}

		// 循环回到开头，模型将带着新加入的 Observation 继续它的下一轮思考...
	}

	return nil
}

