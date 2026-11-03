package llm_test

import (
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/jaguarnova/agent-lab/ch05/internal/llm"
)

func TestValidateFinal(t *testing.T) {
	cases := []struct {
		name    string
		message *schema.Message
		wantErr bool
	}{
		{name: "no response", wantErr: true},
		{name: "missing completion metadata", message: &schema.Message{Content: "42"}, wantErr: true},
		{name: "truncated reasoning", message: &schema.Message{ReasoningContent: "still thinking", ResponseMeta: &schema.ResponseMeta{FinishReason: "length"}}, wantErr: true},
		{name: "truncated visible answer", message: &schema.Message{Content: "the result is", ResponseMeta: &schema.ResponseMeta{FinishReason: "length"}}, wantErr: true},
		{name: "empty stopped answer", message: &schema.Message{ResponseMeta: &schema.ResponseMeta{FinishReason: "stop"}}, wantErr: true},
		{name: "whitespace stopped answer", message: &schema.Message{Content: " \n\t", ResponseMeta: &schema.ResponseMeta{FinishReason: "stop"}}, wantErr: true},
		{name: "pending tool call", message: &schema.Message{Content: "calling add", ToolCalls: []schema.ToolCall{{ID: "call1"}}, ResponseMeta: &schema.ResponseMeta{FinishReason: "tool_calls"}}, wantErr: true},
		{name: "tool call with stop marker", message: &schema.Message{Content: "calling add", ToolCalls: []schema.ToolCall{{ID: "call1"}}, ResponseMeta: &schema.ResponseMeta{FinishReason: "stop"}}, wantErr: true},
		{name: "complete answer", message: &schema.Message{Content: "42", ResponseMeta: &schema.ResponseMeta{FinishReason: "stop"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := llm.ValidateFinal(tc.message)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateFinal() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestValidateCompletion(t *testing.T) {
	cases := []struct {
		name       string
		finish     string
		hasContent bool
		wantErr    bool
	}{
		{name: "EOF without stop", hasContent: true, wantErr: true},
		{name: "length after partial content", finish: "length", hasContent: true, wantErr: true},
		{name: "tool request not final", finish: "tool_calls", hasContent: true, wantErr: true},
		{name: "reasoning only", finish: "stop", wantErr: true},
		{name: "complete visible answer", finish: "stop", hasContent: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := llm.ValidateCompletion(tc.finish, tc.hasContent)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateCompletion() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
