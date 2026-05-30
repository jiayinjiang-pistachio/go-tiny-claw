package main

import (
	"context"
	"log"
	"os"

	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/engine"
	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/provider"
	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/schema"
	"github.com/joho/godotenv"
)

func init() {
	godotenv.Load() // 加载 .env
}

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
			Name: "get_weather",
			Description: "获取指定城市的当前天气情况",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"city": map[string]interface{}{
						"type": "string",
					},
				},
				"required": []string{"city"},
			},
		},
	}
}

func (m *mockRegistry) Execute(ctx context.Context, call schema.ToolCall) schema.ToolResult {
	log.Printf("[Mock 工具执行] 获取 %s 的天气中...\n", call.Name)
	return schema.ToolResult{
		ToolCallID: call.ID,
		Output: "API 返回：今天是晴天，气温 25 度",
		IsError: false,
	}
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

	// 2. 注入伪造的工具注册表
	registry := &mockRegistry{}

	// 3. 实例化核心引擎，开启 EnableThinking 慢思考模式
	eng := engine.NewAgentEngine(llmProvider, registry, workDir, false)

	// 设定测试任务
	prompt := "我想去北京跑步，帮我查查天气合适吗？"

	// 发起任务指令
	err := eng.Run(context.Background(), prompt)

	if err != nil {
		log.Fatalf("引擎奔溃：%v", err)
	}

	// log.Println("架构蓝图搭建完毕，等待各核心模块注入！")
}
