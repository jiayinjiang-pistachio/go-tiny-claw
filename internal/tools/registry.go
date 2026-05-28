package tools

import (
	"context"

	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/schema"
)

// Registry 定义了工具的注册与分发执行接口
type Registry interface {
	// 返回当前系统挂载的所有可用的工具 Schema
	GetAvaliableTools() []schema.ToolDefinition

	// Execute 实际执行模型请求的工具，并返回结果
	Execute(ctx context.Context, call schema.ToolCall) schema.ToolResult
}