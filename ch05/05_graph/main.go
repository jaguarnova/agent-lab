// 阶段五练习 5：Graph / Node / Edge。
//
// 把 PromptTemplate 与 ChatModel 装进有向图：START → prompt → model → END。
// Compile 之后得到 Runnable，调用方只关心输入输出类型，不再手动串组件。
//
// 用法：DEEPSEEK_API_KEY=sk-xxx go run ./ch05/05_graph
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/jaguarnova/agent-lab/ch05/internal/llm"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tmpl := prompt.FromMessages(schema.FString,
		schema.SystemMessage("你是一个简洁的中文技术助手，回答不超过 100 字。"),
		schema.UserMessage("{question}"),
	)

	graph := compose.NewGraph[map[string]any, *schema.Message]()
	if err := graph.AddChatTemplateNode("prompt", tmpl); err != nil {
		log.Fatal(err)
	}
	if err := graph.AddChatModelNode("model", llm.New(ctx, 200)); err != nil {
		log.Fatal(err)
	}
	if err := graph.AddEdge(compose.START, "prompt"); err != nil {
		log.Fatal(err)
	}
	if err := graph.AddEdge("prompt", "model"); err != nil {
		log.Fatal(err)
	}
	if err := graph.AddEdge("model", compose.END); err != nil {
		log.Fatal(err)
	}

	runnable, err := graph.Compile(ctx, compose.WithGraphName("prompt_model"))
	if err != nil {
		log.Fatal(err)
	}

	start := time.Now()
	resp, err := runnable.Invoke(ctx, map[string]any{"question": "用一句话解释什么是 LLM 的 Token。"})
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
