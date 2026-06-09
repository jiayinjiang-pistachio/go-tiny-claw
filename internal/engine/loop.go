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
	enableThinking bool // 慢思考模式开关
	PlanMode       bool // 暴露给外部的计划模式开关
	// composer       *ctxpkg.PromptComposer // Prompt 组装器，用于构建动态上下文
	compactor *ctxpkg.Compactor       // 上下文压缩器
	recovery  *ctxpkg.RecoveryManager // 错误自愈管理器
	injector    *ReminderInjector       // 提醒注入器
}

// 移除 Engine 层的 workDir，因为 workDir 应跟随 Session 走
func NewAgentEngine(p provider.LLMProvider, r tools.Registry, enableThinking bool, planMode bool) *AgentEngine {
	return &AgentEngine{
		provider:       p,
		registry:       r,
		enableThinking: enableThinking, // 使用传入的参数初始化慢思考模式开关
		PlanMode:       planMode,
		// 假装这里能获取到 workDir 初始化 Composer，生产环境中应在 Run 中动态构造
		// composer: ctxpkg.NewPromptComposer("."),
		// 初始化压缩器：为了便于今天的极端测试，我们将水位线阈值设积极（例如 3000 字符），
		// 并保护最近的 6 条消息（大约两轮 Turn 交互）
		compactor: ctxpkg.NewCompactor(3000, 6),
		recovery:  ctxpkg.NewRecoveryManager(), // 初始化 Recovery
		injector: NewReminderInjector(), // 初始化注入器
	}
}

// Run 启动 Agent 的生命周期
// 移除 userPrompt，改为接收一个具体的 Session 实例
func (e *AgentEngine) Run(ctx context.Context, session *Session, reporter Reporter) error {
	log.Printf("[Engine] 唤醒会话 [%s]，锁定工作区：%s (PlanMode: %v)\n", session.ID, session.WorkDir, e.PlanMode)

	// 在每次运行前，动态生成组装器并传入当前的 PlanMode 状态
	composer := ctxpkg.NewPromptComposer(session.WorkDir, e.PlanMode)

	// 【核心修改】动态组装 System Prompt，彻底替换以前硬编码的面条提示词
	systemMsg := composer.Build()

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

		var currentTurnThinkingContent string

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
				currentTurnThinkingContent = thinkResp.Content
				// 将思考过程持久化到 session 中
				// session.Append(*thinkResp)
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

		// (上一讲修复 1214 的关键代码：合并为合法的单条 Assistant 消息)
		finalAssistantMsg := schema.Message{
			Role:      schema.RoleAssistant,
			Content:   strings.TrimSpace(currentTurnThinkingContent + "\n" + actionResp.Content),
			ToolCalls: actionResp.ToolCalls,
		}
		session.Append(finalAssistantMsg)

		// 将大模型的响应持久化到 Session 中
		// session.Append(*actionResp)
		// compactedContext = append(compactedContext, *actionResp)

		if actionResp.Content != "" && reporter != nil {
			reporter.OnMessage(ctx, actionResp.Content)
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

		// 用于收集本轮执行的最后一个工具，供 Reminder 探测器分析
		var lastToolCall schema.ToolCall
		var lastToolResult schema.ToolResult

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

				// 【核心拦截与注入】
				finalOutput := result.Output
				if result.IsError {
					// 发生错误，交由 RecoveryManager 诊断并注入“锦囊妙计”
					finalOutput = e.recovery.AnalyzeAndInject(call.Name, result.Output)
					log.Printf("  -> [Go-%d] ❌ 注入救援指南：%s\n", idx, finalOutput)
				} else {
					log.Printf("  -> [Go-%d] ✅ 工具执行成功（返回 %d 字节）\n", idx, len(result.Output))
				}

				if reporter != nil {
					// 为了防止大文件读取导致飞书消息过长被截断，我们仅汇报工具执行状态
					// 注意：传递给大语言模型的 observationMsgs 依然是完整数据，只是人类看到的 Reporter 市缩略版
					displayOutput := finalOutput
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
					Content:    finalOutput,
					ToolCallID: call.ID,
				}

				// 捕获状态供外部探测器使用
				if idx == 0 {
					lastToolCall = call
					lastToolResult = result
				}
			}(i, toolCall)
		}

		// 4. Join 阻塞等待：主循环挂起，直到所有的并发协程全部执行完毕
		wg.Wait()

		// 1. 先将普通的工具执行结果存入 Session
		// 将所有的工具执行结果（Observation）持久化到 Session 中，开启下一轮的复盘与推理
		session.Append(observationMsgs...)

		// 2. 【核心防线】：在准备进入下一轮之前，进行死循环探测
		reminderMsg := e.injector.CheckAndInject(lastToolCall, lastToolResult)
		if reminderMsg != nil {
			// 如果触发了干预探测，将这严厉的提醒作为 User 消息，强制追加到 Session 的最末尾！
			// 大模型在下一轮被唤醒时，第一眼就会看到这句话，从而打破布局执念。
			session.Append(*reminderMsg)
		}

		// 循环回到开头，模型将带着新加入的 Observation 继续它的下一轮思考...
	}

	return nil
}

// RunSub 是专为 Subagent 拉起的一次性受限循环
// 它不依赖外部 Session，打完就跑
// Reporter ： 为了让用户在终端看到子智能体的工作轨迹，我们将主线程的 Reporter 透传进来，并打上特殊标记
func (e *AgentEngine) RunSub(ctx context.Context, taskPrompt string, readOnlyRegistry tools.Registry, reporter any) (string, error) {
	//【核心优化】：子智能体极其容易偷懒，我们必须在 System Prompt 中严厉警告它必须使用工具
	contextHistory := []schema.Message{
		{
			Role: schema.RoleSystem,
			Content: `你是一个专门负责深度探索的探路者 (Explorer Subagent)。
你的任务是根据主架构师的指令，在当前工作区内仔细阅读代码、查阅日志，搜集足够的信息。
【核心纪律】
1. 你必须、且只能依靠内置工具（如 bash 的 find/grep，或 read_file）去寻找答案。绝对不允许凭空捏造或猜测！
2. 如果你没有找到确切的答案，你必须继续使用工具深入搜索。
3. 当且仅当你找到了确切的线索后，停止调用工具，直接输出一段纯文本作为你的终极汇报。主架构师会根据你的汇报来做下一步决策。`,
		},
		{
			Role: schema.RoleUser,
			Content: taskPrompt,
		},
	}

	// 限制子智能体最多只能跑 10 个 Turn，防止它自己卡死
	const maxSubTurns = 10 
	turnCount := 0

	for {
		turnCount++
		if turnCount > maxSubTurns {
			return "", fmt.Errorf("子智能体探索过于深入，超过 %d 次，被强制召回，请主 Agent 给它更明确的指令", maxSubTurns)
		}

		// 【驾驭底线】：子智能体只能获取传入的只读工具列表
		avaliableTools := readOnlyRegistry.GetAvaliableTools()

		compactedContext := e.compactor.Compact(contextHistory)

		// 子任务要求极速响应，强制关闭主体的慢思考，直接预测行动
		actionResp, err := e.provider.Generate(ctx, compactedContext, avaliableTools)
		if err != nil {
			return "", fmt.Errorf("子智能体推理失败：%w", err)
		}		

		contextHistory = append(contextHistory, *actionResp)

		// 【核心退出条件】：子智能体一旦不调用工具了，说明它做好了工作汇报
		if len(actionResp.ToolCalls) == 0 {
			// 直接将它的这个汇报内容剥离出来返回给上层
			return actionResp.Content, nil
		}

		// 执行只读工具的并发循环
		observationMsgs := make([]schema.Message, len(actionResp.ToolCalls))
		var wg sync.WaitGroup

		for i, toolCall := range actionResp.ToolCalls {
			wg.Add(1)

			go func (idx int, call schema.ToolCall)  {
				defer wg.Done()

				// 【可视化的关键】：让终端用户看到 Subagent 正在干嘛
				var r Reporter
				if reporter != nil {
					r = reporter.(Reporter)
					r.OnToolCall(ctx, fmt.Sprintf("[Subagent] %s", call.Name), string(call.Arguments))
				}

				result := readOnlyRegistry.Execute(ctx, call)

				finalOutput := result.Output
				if result.IsError {
					finalOutput = e.recovery.AnalyzeAndInject(call.Name, result.Output)
				}

				if reporter != nil {
					display := finalOutput
					if len(display) > 200 {
						display = display[:200] + "...（已截断）"
					}
					r.OnToolResult(ctx, fmt.Sprintf("[Subagent] %s", call.Name), display, result.IsError)
				}

				observationMsgs[idx] = schema.Message{
					Role: schema.RoleUser,
					Content: finalOutput,
					ToolCallID: call.ID,
				}
			}(i, toolCall)
		}

		wg.Wait()
		contextHistory = append(contextHistory, observationMsgs...)
	}
}
