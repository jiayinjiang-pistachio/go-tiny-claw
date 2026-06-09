package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/schema"
)

// AgentRunner 是一个打破循环依赖的抽象接口
// 因为 SubAgentTool 存在于tools包，而完整的 AgenrEngine 存在于 engine 包
// 为了让 tool 能拉起 Engine，我们定义一个接口供外部注入
type AgentRunner interface {
	// RunSub 启动一个匿名的、一次性的子智能体任务，并返回其最终梳理的纯文本总结
	RunSub(ctx context.Context, taskPrompt string, readOnlyRegistry Registry, reporter interface{}) (string, error)
}

type SubAgentTool struct {
	runner AgentRunner

	// 为子智能体准备的专属、受限得 “只读”列表
	readOnlyRegistry Registry
	reporter         interface{} // 暂时用 interface 规避包循环依赖，底层通过断言实现
}

func NewSubAgentTool(runner AgentRunner, readOnlyRegistry Registry, reporter interface{}) *SubAgentTool {
	return &SubAgentTool{
		runner: runner,
		readOnlyRegistry: readOnlyRegistry,
		reporter: reporter,
	}
}

func (t *SubAgentTool) Name() string {
	return "spawn_subagent"
}

// Definition 向主 Agent 暴露这个工具的强大能力
func (t *SubAgentTool) Definition() schema.ToolDefinition {
	return schema.ToolDefinition{
		Name: t.Name(),
		Description: "派出一个专门用于探索（Exploration）的子智能体，当你需要阅读大量代码、跨文件查找逻辑时请调用此工具。它在探索完毕后，会给你返回一份极度精炼的摘要报告",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task_prompt": map[string]interface{}{
					"type": "string",
					"description": "给子智能体下达的明确指令。",
				},
			},
			"required": []string{"task_prompt"},
		},
	}
}

type subAgentArgs struct {
	TaskPrompt string`json:"task_prompt"`
}

func (t *SubAgentTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var input subAgentArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("解析参数失败: %w", err)
	}

	log.Printf("[Subagent] 🚀 主 Agent 发起委派！正在拉起探路者: [%s]...\n", input.TaskPrompt)

	// 【核心降维打击】：拉起一个完全物理隔离的子循环
	// 我们把针对该任务的专项指令传给智能体，并仅提供 readOnlyRegistry 
	// (子智能体只能读文件或执行只读的 bash，不能搞破坏)
	summary, err := t.runner.RunSub(ctx, input.TaskPrompt, t.readOnlyRegistry, t.reporter)

	if err != nil {
		return fmt.Errorf("子智能体执行失败：%v", err).Error(), nil
	}

	log.Printf("[Subagent] ✅ 子智能体任务结束。报告返回给主干...")

	// 最终，几万字的代码探索，化作了这一段轻量级的 Summary
	// 就像一次普通的 API 调用一样，返回给了始终保持清醒的主 Agent
	return fmt.Sprintf("【子智能体探索报告】：\n%s", summary), nil
}