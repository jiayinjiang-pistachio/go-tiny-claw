package provider

// import (
// 	"encoding/json"
// 	"testing"

// 	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/schema"
// )

// func TestBuildAnthropicMessages_MergesConsecutiveToolResults(t *testing.T) {
// 	msgs := []schema.Message{
// 		{Role: schema.RoleUser, Content: "hello"},
// 		{
// 			Role: schema.RoleAssistant,
// 			Content: "calling tools",
// 			ToolCalls: []schema.ToolCall{
// 				{ID: "call_1", Name: "read_file", Arguments: json.RawMessage(`{"path":"a.go"}`)},
// 				{ID: "call_2", Name: "read_file", Arguments: json.RawMessage(`{"path":"b.go"}`)},
// 			},
// 		},
// 		{Role: schema.RoleUser, Content: "output-a", ToolCallID: "call_1"},
// 		{Role: schema.RoleUser, Content: "output-b", ToolCallID: "call_2"},
// 	}

// 	_, anthropicMsgs := buildAnthropicMessages(msgs)
// 	if len(anthropicMsgs) != 3 {
// 		t.Fatalf("expected 3 anthropic messages, got %d", len(anthropicMsgs))
// 	}
// }

// func TestBuildAnthropicMessages_MergesConsecutiveAssistants(t *testing.T) {
// 	msgs := []schema.Message{
// 		{Role: schema.RoleUser, Content: "hello"},
// 		{Role: schema.RoleAssistant, Content: "thinking trace"},
// 		{
// 			Role: schema.RoleAssistant,
// 			Content: "action",
// 			ToolCalls: []schema.ToolCall{
// 				{ID: "call_1", Name: "bash", Arguments: json.RawMessage(`{"command":"ls"}`)},
// 			},
// 		},
// 		{Role: schema.RoleUser, Content: "ls output", ToolCallID: "call_1"},
// 	}

// 	_, anthropicMsgs := buildAnthropicMessages(msgs)
// 	if len(anthropicMsgs) != 3 {
// 		t.Fatalf("expected 3 anthropic messages, got %d", len(anthropicMsgs))
// 	}
// }
