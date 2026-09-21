// ch01/03_sdk/main.go
// 阶段一练习 3：用 OpenAI 官方 Go SDK (openai-go) 实现与 01_raw 完全相同的功能，
// 对比"SDK 帮你做了什么、藏了什么"。
//
// 用法：OPENAI_BASE_URL=... OPENAI_API_KEY=sk-xxx MODEL=deepseek-flash go run ./ch01/03_sdk
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func main() {
	baseURL := getenv("OPENAI_BASE_URL", "https://api.openai.com/v1")
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "缺少 OPENAI_API_KEY 环境变量")
		os.Exit(1)
	}

	// SDK 第一处抽象：base URL 与鉴权变成 option 包的配置，不再手拼 header
	client := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(baseURL),
	)
	model := getenv("MODEL", "deepseek-flash")
	prompt := "用一句话解释什么是 LLM 的上下文窗口。"

	// ---- 非流式：对比 01_raw ----
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	completion, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("你是一个简洁的中文技术助手，回答不超过 100 字。"),
			openai.UserMessage(prompt),
		},
		MaxTokens: openai.Int(200),
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("=== 非流式（SDK）耗时 %v ===\n", time.Since(start))
	fmt.Printf("finish_reason: %s\n", completion.Choices[0].FinishReason)
	fmt.Printf("回复: %s\n", completion.Choices[0].Message.Content)
	fmt.Printf("Token: prompt=%d completion=%d total=%d\n\n",
		completion.Usage.PromptTokens, completion.Usage.CompletionTokens, completion.Usage.TotalTokens)

	// ---- 流式：对比 02_stream ----
	// SDK 第二处抽象：SSE 解析变成迭代器，bufio/扫描器/分帧逻辑全部消失
	streamStart := time.Now()
	stream := client.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
		Model: model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("你是一个简洁的中文技术助手。"),
			openai.UserMessage("用 3 句话解释什么是流式响应。"),
		},
	})
	fmt.Printf("=== 流式（SDK）开始于 %v ===\n<<< ", time.Since(streamStart))

	var full strings.Builder
	chunks := 0
	for stream.Next() {
		chunk := stream.Current()
		for _, c := range chunk.Choices {
			if c.Delta.Content != "" {
				fmt.Print(c.Delta.Content)
				full.WriteString(c.Delta.Content)
				chunks++
			}
		}
	}
	// 第三处抽象：错误处理统一到 stream.Err()，不用区分 HTTP 错误和解析错误
	if err := stream.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "\n流式出错: %v\n", err)
	}
	io.Discard.Write(nil) // 保持 io 导入（练习代码用不到）
	fmt.Printf("\n\n=== %d 个 chunk，总耗时 %v ===\n", chunks, time.Since(streamStart))
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
