package provider

import (
	"context"

	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/schema"
)

// LLMProvider 定义了与大语言模型通信的统一契约，无论是调用 OpenAI、Anthropic 还是本地部署的模型，都必须实现这个接口
type LLMProvider interface {
	// Generate 接收当前的上下文历史、可用工具列表，并发起一次模型推理
	Generate(ctx context.Context, messages []schema.Message, avaliableTools []schema.ToolDefinition) (*schema.Message, error)
}