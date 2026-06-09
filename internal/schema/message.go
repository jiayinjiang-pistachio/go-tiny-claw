package schema

import "encoding/json"

// Role 定义消息角色，这是与大模型沟通的基石
type Role string

const (
	RoleSystem Role = "system" // 系统提示词：确立 Agent 的性格和红线
	RoleUser   Role = "user" // 用户输入 / 工具执行的返回结果（Observation）
	RoleAssistant Role = "assistant" // 模型的输出：包含推理（Reasoning）、或工具调用（ToolCall）
)

type Usege struct {
	PromptTokens int `json:"prompt_tokens"` // 输入的 token 数量
	CompletionTokens int `json:"completion_tokens"` // 产生的 token 数量
}


// Message 代表上下文中传递的单条消息
type Message struct {
	Role Role `json:"role"` // 消息的角色
	Content string `json:"content"` // 消息的内容，通常是文本

	// 如果模型决定调用工具，此字段将被填充（支持并行调用多个工具）
	ToolCalls []ToolCall `json:"tool_calls,omitempty"` // omitempty 作用： 如果 ToolCalls 是 nil 或者长度为 0 的空切片 []ToolCall{}，生成的 JSON 中不会包含 "tool_calls" 这个键。
	// 如果这是对某个工具调用的响应，此字段必须填写，以告知模型上下文的关联性
	ToolCallID string `json:"tool_call_id,omitempty"` // 仅当 Role 是 assistant 且 Content 是推理时，才会有这个字段

	// 如果这是大模型（Assistant）的回复，此字段存放本次调用的 token 消耗
	Usage *Usege `json:"usage,omitempty"`
}

// ToolCall 代表模型请求调用某个具体的工具
type ToolCall struct {
	ID string `json:"id"` // 工具调用的唯一ID
	Name string `json:"name"` // 想要调用的工具名称（例如“bash”）
	// Arguments 存放 JSON 参数，使用 RawMessage 是为了延迟解析，将解析责任交给具体工具
	// json.RawMessage 是 Go encoding/json 包中定义的一个类型，其本质是 []byte切片。
	// 	🎯 核心作用：“延迟解析”或“原样保留”
	// 当你使用 json.RawMessage 时，你告诉 JSON 解码器（Unmarshal）或编码器（Marshal）：
	// “不要尝试解析这个字段内部的结构，把它当作一段原始的 JSON 字节流直接存下来（或原样输出）。”
	Arguments json.RawMessage `json:"arguments"` // 工具调用的参数，原始 JSON 格式，具体结构由工具定义
}

// ToolResult 代表工具在本地执行完毕后返回的物理结果
type ToolResult struct {
	ToolCallID string `json:"tool_call_id"` // 关联的工具调用 ID，必须填写以便模型正确关联上下文
	Output string `json:"output"` // 工具执行的控制台输出或报错堆栈
	IsError bool `json:"is_error"` // 标记是否失败，供后续的驾驭工程进行错误自愈
}

// ToolDefinition 描述了一个大模型可以调用的工具元信息（供模型理解工具有什么用）
type ToolDefinition struct {
	Name string `json:"name"` // 工具名称，必须唯一
	Description string `json:"description"` // 工具功能描述，供模型理解工具的用途
	// 在 Go 中，interface{}（在 Go 1.18+ 中也可以写成 any）表示空接口。
	// 当你需要立即访问或修改 JSON 内部的某个字段，且结构不完全固定时，用 interface{} 来接收并解析它是非常方便的。
	// 只有当你需要在后端主动校验 Schema 合法性，或者动态修改 Schema 内容时，才考虑解析它。
	InputSchema interface{} `json:"input_schema"` // 工具参数的 JSON Schema 定义，供模型构造正确的调用参数
}
