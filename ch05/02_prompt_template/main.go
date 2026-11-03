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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cm := llm.New(ctx, 200)

	tmpl := prompt.FromMessages(schema.FString,
		schema.SystemMessage("你是一个简洁的中文技术助手，读者是{role}，回答不超过 100 字。"),
		schema.UserMessage("{question}"),
	)

	msgs, err := tmpl.Format(ctx, map[string]any{
		"role":     "Go 后端工程师",
		"question": "用一句话解释什么是 LLM 的 Token。",
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, msg := range msgs {
		fmt.Printf(">>> [%s] %s\n", msg.Role, msg.Content)
	}

	start := time.Now()
	resp, err := cm.Generate(ctx, msgs)
	latency := time.Since(start)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("<<< 回复: %s\n", resp.Content)
	if resp.ResponseMeta != nil && resp.ResponseMeta.Usage != nil {
		usage := resp.ResponseMeta.Usage
		fmt.Printf("<<< Token 用量: prompt=%d completion=%d total=%d\n",
			usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
	}
	fmt.Printf("<<< 耗时: %s\n", latency.Round(time.Millisecond))
}
