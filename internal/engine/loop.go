package engine

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/provider"
	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/schema"
	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/tools"
)

// AgentEngine 是微型 OS 的核心驱动
type AgentEngine struct {
	provider provider.LLMProvider
	registry tools.Registry

	// workDir 工作区：借鉴 OpenClaw 的理念，Agent 必须有一个明确的物理边界
	workDir        string
	enableThinking bool // 慢思考模式开关
}

func NewAgentEngine(p provider.LLMProvider, r tools.Registry, workDir string, enableThinking bool) *AgentEngine {
	return &AgentEngine{
		provider:       p,
		registry:       r,
		workDir:        workDir,
		enableThinking: enableThinking, // 使用传入的参数初始化慢思考模式开关
	}
}

// Run 启动 Agent 的生命周期
func (e *AgentEngine) Run(ctx context.Context, userPrompt string, reporter Reporter) error {
	log.Printf("[Engine]引擎启动，锁定工作区：%s\n", e.workDir)

	// 1. 初始化会话的 Context（上下文内存）
	// 在真实的场景中，这里会由动态的 Prompt 组装器加载 AGENTS.md，目前我们先硬编码
	contextHistory := []schema.Message{
		{
			Role:    schema.RoleSystem,
			Content: "You are go-tiny-claw, an expert coding assistant. ",
		},
		{
			Role:    schema.RoleUser,
			Content: userPrompt,
		},
	}

	turnCount := 0

	// 2. the main loop 心跳开始（标准的ReACT循环）
	for {
		turnCount++

		// 获取当前挂载的所有工具定义
		avaliableTools := e.registry.GetAvaliableTools()

		// =========================================================
		// 1. Phase 1: 慢思考阶段（Thinking）- 剥夺工具，强制规划
		// =========================================================
		if e.enableThinking {
			if reporter != nil {
				// 【触发 Reporter】：开始慢思考
				reporter.OnThinking(ctx)
			}

			// 核心机制：传入的 avaliableTools 为 nil
			// 大模型看不到任何 JSON Schema，被迫只能输出纯文本的思考过程
			thinkResp, err := e.provider.Generate(ctx, contextHistory, nil)
			if err != nil {
				return fmt.Errorf("Thinking 生成失败：%w", err)
			}

			// 如果模型输出了思考过程，我们将其作为 Assistant 消息追加到上下文
			if thinkResp.Content != "" {
				contextHistory = append(contextHistory, *thinkResp)
			}
		}

		// =========================================================
		// 2. Phase 2: 行动阶段（Action）- 恢复工具，顺着规划执行
		// =========================================================

		// 此时的 contextHistory 中已经包含了上一阶段模型自己的 Thinking Trace
		// 模型会顺着自己的规划逻辑，综合恢复的 avaliableTools，发起精准的工具调用
		actionResp, err := e.provider.Generate(ctx, contextHistory, avaliableTools)
		if err != nil {
			return fmt.Errorf("Action 生成失败：%w", err)
		}

		contextHistory = append(contextHistory, *actionResp)

		if reporter != nil {
			// 【触发 Reporter】：输出阶段性总结或最终回复
			// 避免向上游发送仅包含空白字符的消息
			trimmed := strings.TrimSpace(actionResp.Content)
			if trimmed != "" {
				reporter.OnMessage(ctx, trimmed)
			}
		}

		// 3. 退出条件判断
		// 如果模型没有请求任何工具调用，说明它认为任务已经完成，跳出循环
		if len(actionResp.ToolCalls) == 0 {
			break
		}

		// 4. 执行行动（Action）与获取观察结果（Observation）
		// 核心改进：从串行演进为并行
		// 1. 预分配一个固定长度的切片，用于安全地存放各个并发工具的执行结果（Observation）
		// 长度与 ToolCalls 的数量完全一致
		observationMsgs := make([]schema.Message, len(actionResp.ToolCalls))

		// 2. 声明 WaitGroup 用于阻塞等待所有协程完成
		var wg sync.WaitGroup

		// 3. 遍历模型请求的所有工具，为每一个工具单独 Fork 出一个 Goroutine
		for i, toolCall := range actionResp.ToolCalls {
			wg.Add(1) // 增加计数器

			// 开启协程。注意：一定要将索引 i 和 toolCall 作为参数传入匿名函数，防止闭包变量捕获陷进
			go func(idx int, call schema.ToolCall) {
				defer wg.Done() // 协程结束时计数器减一

				if reporter != nil {
					// 【触发 Reporter】：报告即将在底层执行的工具
					reporter.OnToolCall(ctx, call.Name, string(call.Arguments))
				}

				// 调用底层 Registry 执行工具（物理操作）
				result := e.registry.Execute(ctx, call)

				if reporter != nil {
					// 为了防止大文件读取导致飞书消息过长被截断，我们仅汇报工具执行状态
					// 注意：传递给大语言模型的 observationMsgs 依然是完整数据，只是人类看到的 Reporter 市缩略版
					displayOutput := result.Output
					if len(displayOutput) > 200 {
						displayOutput = displayOutput[:200] + "...（已截断）"
					}

					// 【触发 Reporter】：汇报工具物理执行的结果
					reporter.OnToolResult(ctx, call.Name, displayOutput, result.IsError)
				}

				// 将执行结果封装为一条用户信息（RoleUser）
				// 【线程安全】由于每个 Goroutine 操作的是预分配切片的不同索引，
				// 这里不需要加锁（Mutex），性能极高
				observationMsgs[idx] = schema.Message{
					Role:       schema.RoleUser,
					Content:    result.Output,
					ToolCallID: call.ID,
				}
			}(i, toolCall)
		}

		// 4. Join 阻塞等待：主循环挂起，直到所有的并发协程全部执行完毕
		wg.Wait()

		// 5. 聚合装填：将并行的结果，按照原本的顺序，一次性追加到上下文时间线中
		// 这等价于 contextHistory = append(contextHistory, observationMsgs...)
		for _, obs := range observationMsgs {
			contextHistory = append(contextHistory, obs)
		}

		// 循环回到开头，模型将带着新加入的 Observation 继续它的下一轮思考...
	}

	return nil
}
