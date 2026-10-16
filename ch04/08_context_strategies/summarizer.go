package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Usage struct {
	InputTokens  int
	OutputTokens int
	Latency      time.Duration
}

type Summarizer interface {
	Summarize(context.Context, []Message) (string, Usage, error)
}

type HTTPSummarizer struct {
	BaseURL string
	APIKey  string
	Model   string
	Client  *http.Client
}

func (s HTTPSummarizer) Summarize(ctx context.Context, messages []Message) (string, Usage, error) {
	if strings.TrimSpace(s.BaseURL) == "" || strings.TrimSpace(s.APIKey) == "" || strings.TrimSpace(s.Model) == "" {
		return "", Usage{}, fmt.Errorf("base URL, API key, and model are required")
	}
	history, err := json.Marshal(messages)
	if err != nil {
		return "", Usage{}, fmt.Errorf("encode history: %w", err)
	}
	prompt := "Summarize the following conversation history. Preserve concise facts, decisions, tool outcomes, data sources, and unresolved questions. Do not invent facts.\n\n" + string(history)
	requestBody, err := json.Marshal(map[string]any{
		"model": s.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"temperature": 0,
	})
	if err != nil {
		return "", Usage{}, fmt.Errorf("encode request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.BaseURL, "/")+"/chat/completions", bytes.NewReader(requestBody))
	if err != nil {
		return "", Usage{}, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+s.APIKey)
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}

	started := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return "", Usage{}, fmt.Errorf("summarize request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 8<<10))
		if readErr != nil {
			return "", Usage{}, fmt.Errorf("summary provider returned %s; read error body: %w", response.Status, readErr)
		}
		return "", Usage{}, fmt.Errorf("summary provider returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     *int `json:"prompt_tokens"`
			CompletionTokens *int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return "", Usage{}, fmt.Errorf("decode summary response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return "", Usage{}, fmt.Errorf("summary response contains no choices")
	}
	text := strings.TrimSpace(decoded.Choices[0].Message.Content)
	if text == "" {
		return "", Usage{}, fmt.Errorf("summary response content is empty")
	}
	if decoded.Usage == nil || decoded.Usage.PromptTokens == nil || decoded.Usage.CompletionTokens == nil {
		return "", Usage{}, fmt.Errorf("summary response usage is incomplete")
	}
	if *decoded.Usage.PromptTokens <= 0 || *decoded.Usage.CompletionTokens <= 0 {
		return "", Usage{}, fmt.Errorf("summary response usage must contain positive token counts")
	}
	return text, Usage{
		InputTokens:  *decoded.Usage.PromptTokens,
		OutputTokens: *decoded.Usage.CompletionTokens,
		Latency:      time.Since(started),
	}, nil
}

func RunSummarize(ctx context.Context, messages []Message, keepRecent int, summarizer Summarizer) (Result, error) {
	if keepRecent <= 0 {
		return Result{}, fmt.Errorf("keep recent must be positive")
	}
	if summarizer == nil {
		return Result{}, fmt.Errorf("summarizer is required")
	}
	if err := ValidatePairing(messages); err != nil {
		return Result{}, err
	}
	firstGroupIndex := 0
	if len(messages) > 0 && messages[0].Role == "system" {
		firstGroupIndex = 1
	}
	groups := atomicGroups(messages, firstGroupIndex)
	recentStart := len(groups)
	recentMessages := 0
	for index := len(groups) - 1; index >= 0 && recentMessages < keepRecent; index-- {
		recentStart = index
		recentMessages += groups[index].end - groups[index].start
	}
	if recentStart == 0 {
		return Result{}, fmt.Errorf("history has no complete old groups to summarize")
	}
	oldEnd := groups[recentStart].start
	old := messages[firstGroupIndex:oldEnd]
	text, usage, err := summarizer.Summarize(ctx, cloneMessages(old))
	if err != nil {
		return Result{}, fmt.Errorf("summarize history: %w", err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Result{}, fmt.Errorf("summarizer returned empty content")
	}

	out := make([]Message, 0, firstGroupIndex+1+recentMessages)
	if firstGroupIndex == 1 {
		out = appendClonedMessages(out, messages[:1])
	}
	out = append(out, Message{Role: "system", Content: "[history summary]\n" + text})
	for _, group := range groups[recentStart:] {
		out = appendClonedMessages(out, messages[group.start:group.end])
	}
	if err := ValidatePairing(out); err != nil {
		return Result{}, fmt.Errorf("summarized history is invalid: %w", err)
	}
	report := newReport("summarize", messages, out, len(old))
	report.SummaryInputTokens = usage.InputTokens
	report.SummaryOutputTokens = usage.OutputTokens
	report.SummaryLatencyMillis = usage.Latency.Milliseconds()
	return Result{Messages: out, Report: report}, nil
}
