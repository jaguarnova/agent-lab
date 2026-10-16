package main

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

type ToolCall struct {
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type Report struct {
	Strategy             string  `json:"strategy"`
	TokensBefore         int     `json:"tokens_before"`
	TokensAfter          int     `json:"tokens_after"`
	ReductionPercent     float64 `json:"reduction_percent"`
	MessagesBefore       int     `json:"messages_before"`
	MessagesAfter        int     `json:"messages_after"`
	MessagesProcessed    int     `json:"messages_processed"`
	PairingValid         bool    `json:"pairing_valid"`
	OffloadedBytes       int64   `json:"offloaded_bytes,omitempty"`
	OffloadedFiles       int     `json:"offloaded_files,omitempty"`
	SummaryInputTokens   int     `json:"summary_input_tokens,omitempty"`
	SummaryOutputTokens  int     `json:"summary_output_tokens,omitempty"`
	SummaryLatencyMillis int64   `json:"summary_latency_ms,omitempty"`
}

type Result struct {
	Messages []Message
	Report   Report
}

func DecodeMessages(raw []byte) ([]Message, error) {
	var envelope struct {
		Messages []Message `json:"messages"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if len(envelope.Messages) == 0 {
		return nil, fmt.Errorf("history contains no messages")
	}
	return envelope.Messages, nil
}

func EstimateMessages(messages []Message) int {
	return (messageRunes(messages) + 1) / 2
}

func messageRunes(messages []Message) int {
	runes := 0
	for _, message := range messages {
		runes += utf8.RuneCountInString(message.Content) + 4
		for _, call := range message.ToolCalls {
			runes += utf8.RuneCountInString(call.Function.Name + call.Function.Arguments)
		}
	}
	return runes
}

func newReport(strategy string, before, after []Message, processed int) Report {
	beforeTokens := EstimateMessages(before)
	afterTokens := EstimateMessages(after)
	reduction := 0.0
	if beforeTokens > 0 {
		reduction = float64(beforeTokens-afterTokens) / float64(beforeTokens) * 100
	}
	return Report{
		Strategy:          strategy,
		TokensBefore:      beforeTokens,
		TokensAfter:       afterTokens,
		ReductionPercent:  reduction,
		MessagesBefore:    len(before),
		MessagesAfter:     len(after),
		MessagesProcessed: processed,
		PairingValid:      ValidatePairing(after) == nil,
	}
}
