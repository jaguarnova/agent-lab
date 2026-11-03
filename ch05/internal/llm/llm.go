// Package llm 提供 ch05 的 DeepSeek 配置及同题基线消息。
package llm

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cloudwego/eino-ext/components/model/deepseek"
	"github.com/cloudwego/eino/schema"
)

// New 构造 DeepSeek ChatModel；maxTokens 由各实验显式给出，避免默认值漂移。
func New(ctx context.Context, maxTokens int) (*deepseek.ChatModel, error) {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("缺少 DEEPSEEK_API_KEY 环境变量")
	}
	return deepseek.NewChatModel(ctx, &deepseek.ChatModelConfig{
		APIKey:         apiKey,
		BaseURL:        BaseURL(),
		Model:          Name(),
		Timeout:        60 * time.Second,
		MaxTokens:      maxTokens,
		Temperature:    0.7,
		ThinkingConfig: &deepseek.ThinkingConfig{Type: "disabled"},
	})
}

func Name() string {
	return Env("MODEL", "deepseek-flash")
}

func BaseURL() string {
	return Env("DEEPSEEK_BASE_URL", "https://api.deepseek.com")
}

func Env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// 非工具示例共用相同消息和预算；工具、Callback 示例另作功能演示。
const (
	BaselineSystem    = "你是一个简洁的中文技术助手，回答不超过 100 字。"
	BaselineQuestion  = "用一句话解释什么是 LLM 的 Token。"
	BaselineMaxTokens = 200
)

func BaselineMessages() []*schema.Message {
	return []*schema.Message{
		schema.SystemMessage(BaselineSystem),
		schema.UserMessage(BaselineQuestion),
	}
}
