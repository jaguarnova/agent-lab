// Package llm 收敛 ch05 各示例的模型构造，保证实验之间只有实验变量不同。
package llm

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/cloudwego/eino-ext/components/model/deepseek"
)

// New 构造 DeepSeek ChatModel；maxTokens 由各实验显式给出，避免默认值漂移。
func New(ctx context.Context, maxTokens int) *deepseek.ChatModel {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		log.Fatal("缺少 DEEPSEEK_API_KEY 环境变量")
	}
	cm, err := deepseek.NewChatModel(ctx, &deepseek.ChatModelConfig{
		APIKey:      apiKey,
		BaseURL:     BaseURL(),
		Model:       Name(),
		Timeout:     60 * time.Second,
		MaxTokens:   maxTokens,
		Temperature: 0.7,
	})
	if err != nil {
		log.Fatal(err)
	}
	return cm
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
