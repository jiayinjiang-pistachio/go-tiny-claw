package engine

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	ctxpkg "github.com/jiayinjiang-pistachio/go-tiny-claw/internal/context" // 引入我们新建的 context 包
	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/provider"
	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/schema"
	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/tools"
)

// AgentEngine 是微型 OS 的核心驱动
type AgentEngine struct {
	provider provider.LLMProvider
	registry tools.Registry

	// workDir 工作区：借鉴 OpenClaw 的理念，Agent 必须有一个明确的物理边界
	enableThinking bool                   // 慢思考模式开关
	composer       *ctxpkg.PromptComposer // Prompt 组装器，用于构建动态上下文
	compactor      *ctxpkg.Compactor      // 上下文压缩器
}

// 移除 Engine 层的 workDir，因为 workDir 应跟随 Session 走
func NewAgentEngine(p provider.LLMProvider, r tools.Registry, enableThinking bool) *AgentEngine {
	return &AgentEngine{
		provider:       p,
		registry:       r,
		enableThinking: enableThinking, // 使用传入的参数初始化慢思考模式开关
		// 假装这里能获取到 workDir 初始化 Composer，生产环境中应在 Run 中动态构造
		composer: ctxpkg.NewPromptComposer("."),
		// 初始化压缩器：为了便于今天的极端测试，我们将水位线阈值设积极（例如 3000 字符），
		// 并保护最近的 6 条消息（大约两轮 Turn 交互）
		compactor: ctxpkg.NewCompactor(3000, 6),
	}
}

// Run 启动 Agent 的生命周期
// 移除 userPrompt，改为接收一个具体的 Session 实例
func (e *AgentEngine) Run(ctx context.Context, session *Session, reporter Reporter) error {
	log.Printf("[Engine] 唤醒会话 [%s]，锁定工作区：%s\n", session.ID, session.WorkDir)

	// 根据当前的 Session 工作区，懂他组装最新的 System Prompt
	e.composer = ctxpkg.NewPromptComposer(session.WorkDir)

	// 【核心修改】动态组装 System Prompt，彻底替换以前硬编码的面条提示词
	systemMsg := e.composer.Build()

	for {
		// 获取当前挂载的所有工具定义
		avaliableTools := e.registry.GetAvaliableTools()

		// 1. 【上下文组装】：SystemPrompt + 截取最近的 20 条消息作为 Working Memory，给压缩器留下足够的判断空间
		// 在实际业务中，由于工具返回结果可能很长，短期记忆往往设置为 6-10 条足以维系连贯对话
		workingMemory := session.GetWorkingMemory(20)

		var contextHistory []schema.Message
		contextHistory = append(contextHistory, systemMsg)
		contextHistory = append(contextHistory, workingMemory...)

		// 2. 在向 Provider 发起推理前，过一遍内存压缩器！
		// 无论你带出了多少上下文，如果字符数超标，早期日志将被掩码化，超大日志将被掐头去尾
		compactedContext := e.compactor.Compact(contextHistory)

		// 3. 后续的 Provider.Generate 全面使用被保护过的新鲜上下文（compactedContext）
		// ================== Phase 1: Thinking ========================
		if e.enableThinking {
			if reporter != nil {
				// 【触发 Reporter】：开始慢思考
				reporter.OnThinking(ctx)
			}
			// 核心机制：传入的 avaliableTools 为 nil
			// 大模型看不到任何 JSON Schema，被迫只能输出纯文本的思考过程
			thinkResp, err := e.provider.Generate(ctx, compactedContext, nil)
			if err != nil {
				return fmt.Errorf("Thinking 生成失败：%w", err)
			}

			// 如果模型输出了思考过程，我们将其作为 Assistant 消息追加到上下文
			if thinkResp.Content != "" {
				// 将思考过程持久化到 session 中
				session.Append(*thinkResp)
				// 把它追加到这一轮的临时上下文中，供 Action 阶段使用
				compactedContext = append(compactedContext, *thinkResp)
			}
		}

		//  ================= Phase 2: 行动阶段（Action）===================
		// 此时的 contextHistory 中已经包含了上一阶段模型自己的 Thinking Trace
		// 模型会顺着自己的规划逻辑，综合恢复的 avaliableTools，发起精准的工具调用
		actionResp, err := e.provider.Generate(ctx, compactedContext, avaliableTools)
		if err != nil {
			return fmt.Errorf("Action 生成失败：%w", err)
		}

		// 将大模型的响应持久化到 Session 中
		session.Append(*actionResp)
		compactedContext = append(compactedContext, *actionResp)

		if actionResp.Content != "" && reporter != nil {
			// 【触发 Reporter】：输出阶段性总结或最终回复
			// 避免向上游发送仅包含空白字符的消息
			trimmed := strings.TrimSpace(actionResp.Content)
			if trimmed != "" {
				reporter.OnMessage(ctx, fmt.Sprintf("%s: "+trimmed, session.ID))
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

		// 将所有的工具执行结果（Observation）持久化到 Session 中，开启下一轮的复盘与推理
		session.Append(observationMsgs...)

		// 循环回到开头，模型将带着新加入的 Observation 继续它的下一轮思考...
	}

	return nil
}
