// 阶段五练习 2：PromptTemplate。
//
// 把 system/user 模板与运行期变量分离，Format 产出 []*schema.Message，
// 再交给 ChatModel；变量缺失会在 Format 时直接报错，而不是等到模型返回。
//
// 用法：DEEPSEEK_API_KEY=sk-xxx go run ./ch05/02_prompt_template
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/schema"

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

	tmpl := prompt.FromMessages(schema.FString,
		schema.SystemMessage(llm.BaselineSystem),
		schema.UserMessage("{question}"),
	)

	msgs, err := tmpl.Format(ctx, map[string]any{
		"question": llm.BaselineQuestion,
	})
	if err != nil {
		return err
	}
	for _, msg := range msgs {
		fmt.Printf(">>> [%s] %s\n", msg.Role, msg.Content)
	}

	start := time.Now()
	resp, err := cm.Generate(ctx, msgs)
	latency := time.Since(start)
	if err != nil {
		return err
	}
	if err := llm.ValidateFinal(resp); err != nil {
		return err
	}

	fmt.Printf("<<< 回复: %s\n", resp.Content)
	if resp.ResponseMeta != nil && resp.ResponseMeta.Usage != nil {
		usage := resp.ResponseMeta.Usage
		fmt.Printf("<<< Token 用量: prompt=%d completion=%d total=%d\n",
			usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
	}
	fmt.Printf("<<< 耗时: %s\n", latency.Round(time.Millisecond))
	return nil
}
