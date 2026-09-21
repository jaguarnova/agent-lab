// ch01/01_raw/main.go
// 阶段一练习 1：不使用任何 SDK，用 net/http 直接调用 OpenAI 兼容的 chat/completions 接口。
// 目标：手动构造请求体、解析响应 JSON，看清一次 LLM 调用的完整数据流。
//
// 用法：OPENAI_BASE_URL=https://api.deepseek.com/v1 OPENAI_API_KEY=sk-xxx go run ./ch01/01_raw
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// ---- 请求体：与 OpenAI chat/completions 协议一一对应 ----

type Message struct {
	Role    string `json:"role"`    // system | user | assistant | tool
	Content string `json:"content"` // ponytail: 阶段一只用纯文本 content，多模态/工具调用阶段再升级
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"` // 采样温度：越高越随机
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

// ---- 响应体 ----

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"` // stop | length | tool_calls ...
}

type ChatResponse struct {
	ID      string   `json:"id"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

func main() {
	baseURL := getenv("OPENAI_BASE_URL", "https://api.openai.com/v1")
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "缺少 OPENAI_API_KEY 环境变量")
		os.Exit(1)
	}

	reqBody, err := json.MarshalIndent(ChatRequest{
		Model: getenv("MODEL", "deepseek-flash"),
		Messages: []Message{
			{Role: "system", Content: "你是一个简洁的中文技术助手，回答不超过 100 字。"},
			{Role: "user", Content: "用一句话解释什么是 LLM 的 Token。"},
		},
		Temperature: 0.7,
		MaxTokens:   200,
	}, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Printf(">>> 请求体:\n%s\n\n", reqBody)

	url := baseURL + "/chat/completions"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	// 后端老规矩：任何外部调用都必须有超时
	ctx := req.Context()
	client := &http.Client{Timeout: 60 * time.Second}

	start := time.Now()
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	fmt.Printf(">>> HTTP %d %s，耗时 %v\n\n", resp.StatusCode, resp.Status, time.Since(start))

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "请求失败: %s\n", body)
		os.Exit(1)
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		panic(err)
	}

	fmt.Printf("<<< 模型: %s\n", chatResp.Model)
	fmt.Printf("<<< finish_reason: %s\n", chatResp.Choices[0].FinishReason)
	fmt.Printf("<<< 回复: %s\n", chatResp.Choices[0].Message.Content)
	fmt.Printf("<<< Token 用量: prompt=%d completion=%d total=%d\n",
		chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, chatResp.Usage.TotalTokens)
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
