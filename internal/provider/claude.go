package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/schema"
)

// ClaudeProvider 基于 Anthropic SDK，通过智谱 Claude 兼容端点调用 GLM 模型。
// 内部 schema.Message 与 Anthropic Messages API 格式不同，Generate 负责双向翻译。
type ClaudeProvider struct {
	client anthropic.Client
	model  string
}

// NewZhipuClaudeProvider 构造智谱 Claude 兼容 Provider。
// SDK 会自动在 baseURL 后追加 /v1/messages，因此须使用 /api/anthropic，
// 而非 OpenAI 兼容的 /api/paas/v4/（后者会拼出无效路径 /v4/v1/messages）。
func NewZhipuClaudeProvider(model string) *ClaudeProvider {
	apiKey := os.Getenv("ZHIPU_API_KEY")
	if apiKey == "" {
		panic("请设置 ZHIPU_API_KEY 环境变量")
	}
	baseURL := "https://open.bigmodel.cn/api/anthropic"
	return &ClaudeProvider{
		client: anthropic.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseURL)),
		model:  model,
	}
}

// buildAnthropicMessages 将内部消息序列翻译为 Anthropic API 格式。
//
// 引擎层把每条 tool result 存成独立的 user 消息，但 Anthropic/智谱要求：
// 一次 assistant 的 tool_use 之后，所有 tool_result 必须合并在同一条 user 消息里，
// 否则会触发 1214 "messages 参数非法"。
// 同理，Thinking + Action 两阶段会产生连续 assistant 消息，也需要合并后再发送。
func buildAnthropicMessages(msgs []schema.Message) (systemPrompt string, anthropicMsgs []anthropic.MessageParam) {
	type toolResult struct {
		id      string
		content string
	}

	// 缓冲区内暂存待合并的消息，遇到角色切换时再 flush 到 anthropicMsgs
	var pendingToolResults []toolResult
	var pendingAssistant *schema.Message

	// 将累积的 tool result 合并为一条 user 消息（含多个 tool_result block）
	flushToolResults := func() {
		if len(pendingToolResults) == 0 {
			return
		}
		blocks := make([]anthropic.ContentBlockParamUnion, 0, len(pendingToolResults))
		for _, tr := range pendingToolResults {
			blocks = append(blocks, anthropic.NewToolResultBlock(tr.id, tr.content, false))
		}
		anthropicMsgs = append(anthropicMsgs, anthropic.NewUserMessage(blocks...))
		pendingToolResults = nil
	}

	// 将累积的 assistant 内容合并为一条消息（text + tool_use blocks）
	flushAssistant := func() {
		if pendingAssistant == nil {
			return
		}
		// 即使 Content 为空也必须填充 TextBlock，否则智谱 API 会报 1214
		blocks := []anthropic.ContentBlockParamUnion{
			anthropic.NewTextBlock(pendingAssistant.Content),
		}
		for _, tc := range pendingAssistant.ToolCalls {
			var inputMap map[string]interface{}
			_ = json.Unmarshal(tc.Arguments, &inputMap)
			if inputMap == nil {
				inputMap = map[string]interface{}{}
			}
			blocks = append(blocks, anthropic.ContentBlockParamUnion{
				OfToolUse: &anthropic.ToolUseBlockParam{
					ID:    tc.ID,
					Name:  tc.Name,
					Input: inputMap,
				},
			})
		}
		anthropicMsgs = append(anthropicMsgs, anthropic.NewAssistantMessage(blocks...))
		pendingAssistant = nil
	}

	// 合并连续 assistant 消息：拼接文本，追加 tool_calls
	mergeAssistant := func(msg schema.Message) {
		if pendingAssistant == nil {
			copy := msg
			pendingAssistant = &copy
			return
		}
		if msg.Content != "" {
			if pendingAssistant.Content != "" {
				pendingAssistant.Content += "\n" + msg.Content
			} else {
				pendingAssistant.Content = msg.Content
			}
		}
		pendingAssistant.ToolCalls = append(pendingAssistant.ToolCalls, msg.ToolCalls...)
	}

	for _, msg := range msgs {
		switch msg.Role {
		case schema.RoleSystem:
			// Anthropic 将 system prompt 作为独立字段，不放入 messages 数组
			systemPrompt = msg.Content
		case schema.RoleUser:
			if msg.ToolCallID != "" {
				// 工具执行结果：先结束 assistant 轮次，再缓冲 tool result
				flushAssistant()
				pendingToolResults = append(pendingToolResults, toolResult{
					id:      msg.ToolCallID,
					content: msg.Content,
				})
			} else {
				// 普通用户消息：flush 所有缓冲，保持 user/assistant 严格交替
				flushAssistant()
				flushToolResults()
				anthropicMsgs = append(anthropicMsgs, anthropic.NewUserMessage(
					anthropic.NewTextBlock(msg.Content),
				))
			}
		case schema.RoleAssistant:
			// 新的 assistant 轮次开始前，先 flush 上一批 tool result
			flushToolResults()
			mergeAssistant(msg)
		}
	}

	// 处理末尾尚未 flush 的缓冲
	flushAssistant()
	flushToolResults()
	return systemPrompt, anthropicMsgs
}

func (p *ClaudeProvider) Generate(ctx context.Context, msgs []schema.Message, availableTools []schema.ToolDefinition) (*schema.Message, error) {
	// 1. 消息翻译：内部 schema → Anthropic Messages 格式
	systemPrompt, anthropicMsgs := buildAnthropicMessages(msgs)

	// 2. 工具 Schema 翻译
	var anthropicTools []anthropic.ToolUnionParam
	for _, toolDef := range availableTools {
		// ToolInputSchemaParam 是结构体，需要通过 Properties 字段填充
		var properties map[string]any
		var required []string

		if m, ok := toolDef.InputSchema.(map[string]interface{}); ok {
			if p, ok := m["properties"].(map[string]interface{}); ok {
				properties = p
			}
			if r, ok := m["required"].([]string); ok {
				required = r
			}
		}

		tp := anthropic.ToolParam{
			Name:        toolDef.Name,
			Description: anthropic.String(toolDef.Description),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: properties,
				Required:   required,
			},
		}
		anthropicTools = append(anthropicTools, anthropic.ToolUnionParam{OfTool: &tp})
	}

	// 3. 构建请求并发送
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(p.model),
		MaxTokens: 4096,
		Messages:  anthropicMsgs,
	}

	if systemPrompt != "" {
		params.System = []anthropic.TextBlockParam{
			{Text: systemPrompt},
		}
	}

	// availableTools 为 nil 时不挂载 tools（引擎 Thinking 阶段会传 nil 以禁止工具调用）
	if len(anthropicTools) > 0 {
		params.Tools = anthropicTools
	}

	resp, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("Claude/Zhipu API 请求失败: %w", err)
	}

	// 4. 反向解析：Anthropic 响应 → 内部 schema.Message
	resultMsg := &schema.Message{
		Role: schema.RoleAssistant,
	}

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			resultMsg.Content += block.Text
		case "tool_use":
			argsBytes, _ := json.Marshal(block.Input)
			resultMsg.ToolCalls = append(resultMsg.ToolCalls, schema.ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: argsBytes,
			})
		}
	}

	return resultMsg, nil
}
