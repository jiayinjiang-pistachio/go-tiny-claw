package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/engine"
	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/schema"
)

// =================================================================
// 1. 伪造的大模型 Provider
// =================================================================
type mockProvider struct {
	turn int
}

// 2. 模型大模型的响应：第一轮请求执行 bash，第二轮输出结果
func (m *mockProvider) Generate(ctx context.Context, msgs []schema.Message, tools []schema.ToolDefinition) (*schema.Message, error) {
	// 如果工具列表为空，说明这是引擎发起的 Phase 1：Thinking 阶段
	if len(tools) == 0 {
		return &schema.Message{
				Role:    schema.RoleAssistant,
				Content: "【推理中】目标是检查文件。我不能盲猜，我需要先调用 bash 工具执行 ls 命令，看看当前目录下有什么，然后再定夺。",
			},
			nil
	}

	// 如果工具列表不为空，说明这是 Phase 2：Action 阶段
	m.turn++

	if m.turn == 1 {
		// 第一轮 Action：顺着刚才的 Thinking，精准调用工具
		return &schema.Message{
				Role:    schema.RoleAssistant,
				Content: "我要执行我刚才计划的步骤了。",
				ToolCalls: []schema.ToolCall{
					{ID: "call_123", Name: "bash", Arguments: []byte(`{"command": "ls -la"}`)},
				},
			},
			nil
	}

	// 爹第二轮 Action：直接总结输出
	return &schema.Message{
			Role:    schema.RoleAssistant,
			Content: "根据工具的返回结果，我看到了 main.go，任务圆满完成！",
		},
		nil
}

// =================================================================
// 2. 伪造的 Tool Registry
// =================================================================
type mockRegistry struct{}

func (m *mockRegistry) GetAvaliableTools() []schema.ToolDefinition {
	// 为了让 Phase 2 能检测到工具，这里返回一个伪造的工具定义数组
	return []schema.ToolDefinition{
		{
			Name: "bash",
		},
	}
}

func (m *mockRegistry) Execute(ctx context.Context, call schema.ToolCall) schema.ToolResult {
	return schema.ToolResult{
		TooCallID: call.ID,
		Output:    "-rw-r--r-- 1 user group 234 Oct 24 10:00 main.go\n",
		IsError:   false,
	}
}

// =================================================================
// 3. 组装运行
// =================================================================
func main() {
	fmt.Println("🚀 欢迎来到 go-tiny-claw 引擎启动序列")

	// 获取当前执行目录作为 WorkDir 物理边界
	workDir, _ := os.Getwd()

	p := &mockProvider{}
	r := &mockRegistry{}

	// 实例化核心引擎，开启 EnableThinking 慢思考模式
	eng := engine.NewAgentEngine(p, r, workDir, true)

	// 发起任务指令
	err := eng.Run(context.Background(), "帮我检查当前目录的文件")

	if err != nil {
		log.Fatalf("引擎奔溃：%v", err)
	}

	// log.Println("架构蓝图搭建完毕，等待各核心模块注入！")
}
