// 阶段五练习 1：最小 Eino 应用。
//
// 使用 ch05 统一的 system/user 消息、预算与显式关闭 thinking 的配置，
// 通过 Eino ChatModel 完成一次生成调用，并读取统一的 ResponseMeta。
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
	"time"

	"github.com/jaguarnova/agent-lab/ch05/internal/llm"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cm, err := llm.New(ctx, llm.BaselineMaxTokens)
	if err != nil {
		return err
	}

	fmt.Printf(">>> 模型: %s (base: %s, thinking=disabled)\n", llm.Name(), llm.BaseURL())
	start := time.Now()
	resp, err := cm.Generate(ctx, llm.BaselineMessages())
	latency := time.Since(start)
	if err != nil {
		return err
	}
	if err := llm.ValidateFinal(resp); err != nil {
		return err
	}

	fmt.Printf("<<< 回复: %s\n", resp.Content)
	fmt.Printf("<<< finish_reason: %s\n", resp.ResponseMeta.FinishReason)
	if usage := resp.ResponseMeta.Usage; usage != nil {
		fmt.Printf("<<< Token 用量: prompt=%d completion=%d total=%d\n",
			usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
	}
	fmt.Printf("<<< 耗时: %s\n", latency.Round(time.Millisecond))
	return nil
}
