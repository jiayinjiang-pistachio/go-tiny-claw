package provider

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jiayinjiang-pistachio/go-tiny-claw/internal/schema"
)

func TestZhipuClaudeProvider_MultiToolRoundTrip(t *testing.T) {
	if os.Getenv("ZHIPU_API_KEY") == "" {
		t.Skip("ZHIPU_API_KEY not set")
	}

	p := NewZhipuClaudeProvider("glm-4.5-air")
	msgs := []schema.Message{
		{Role: schema.RoleSystem, Content: "You are a helpful assistant."},
		{Role: schema.RoleUser, Content: "read two files"},
		{
			Role:      schema.RoleAssistant,
			Content:   "I'll read them",
			ToolCalls: []schema.ToolCall{
				{ID: "call_a", Name: "read_file", Arguments: json.RawMessage(`{"path":"a.txt"}`)},
				{ID: "call_b", Name: "read_file", Arguments: json.RawMessage(`{"path":"b.txt"}`)},
			},
		},
		{Role: schema.RoleUser, Content: "content-a", ToolCallID: "call_a"},
		{Role: schema.RoleUser, Content: "content-b", ToolCallID: "call_b"},
	}

	_, err := p.Generate(context.Background(), msgs, nil)
	if err != nil {
		t.Fatalf("multi tool result round trip failed: %v", err)
	}
}
