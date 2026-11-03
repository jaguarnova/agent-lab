// 阶段五练习 1：最小 Eino 应用。
//
// 对比 ch01/01_raw：同样的 system/user 消息与采样参数，不再手写 HTTP/JSON/SSE，
// 而是通过 Eino 的 ChatModel 抽象完成一次生成调用，并读取统一的 ResponseMeta。
//
// 用法：
//
//	DEEPSEEK_API_KEY=sk-xxx go run ./ch05/01_minimal
//	DEEPSEEK_API_KEY=sk-xxx MODEL=deepseek-flash DEEPSEEK_BASE_URL=https://api.deepseek.com go run ./ch05/01_minimal
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cloudwego/eino-ext/components/model/deepseek"
	"github.com/cloudwego/eino/schema"
)

func main() {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		log.Fatal("缺少 DEEPSEEK_API_KEY 环境变量")
	}
	modelName := getenv("MODEL", "deepseek-flash")
	baseURL := getenv("DEEPSEEK_BASE_URL", "https://api.deepseek.com")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Eino 组件统一在构造时接收配置，Generate/Stream 只接收消息与调用选项。
	cm, err := deepseek.NewChatModel(ctx, &deepseek.ChatModelConfig{
		APIKey:      apiKey,
		BaseURL:     baseURL,
		Model:       modelName,
		Timeout:     60 * time.Second,
		MaxTokens:   200,
		Temperature: 0.7,
	})
	if err != nil {
		log.Fatal(err)
	}

	messages := []*schema.Message{
		schema.SystemMessage("你是一个简洁的中文技术助手，回答不超过 100 字。"),
		schema.UserMessage("用一句话解释什么是 LLM 的 Token。"),
	}

	fmt.Printf(">>> 模型: %s (base: %s)\n", modelName, baseURL)
	start := time.Now()
	resp, err := cm.Generate(ctx, messages)
	latency := time.Since(start)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("<<< 回复: %s\n", resp.Content)
	if resp.ResponseMeta != nil {
		fmt.Printf("<<< finish_reason: %s\n", resp.ResponseMeta.FinishReason)
		if usage := resp.ResponseMeta.Usage; usage != nil {
			fmt.Printf("<<< Token 用量: prompt=%d completion=%d total=%d\n",
				usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
		}
	}
	fmt.Printf("<<< 耗时: %s\n", latency.Round(time.Millisecond))
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
