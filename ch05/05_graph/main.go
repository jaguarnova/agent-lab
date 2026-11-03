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

	graph := compose.NewGraph[map[string]any, *schema.Message]()
	if err := graph.AddChatTemplateNode("prompt", tmpl); err != nil {
		return err
	}
	if err := graph.AddChatModelNode("model", cm); err != nil {
		return err
	}
	if err := graph.AddEdge(compose.START, "prompt"); err != nil {
		return err
	}
	if err := graph.AddEdge("prompt", "model"); err != nil {
		return err
	}
	if err := graph.AddEdge("model", compose.END); err != nil {
		return err
	}

	runnable, err := graph.Compile(ctx, compose.WithGraphName("prompt_model"))
	if err != nil {
		return err
	}

	start := time.Now()
	resp, err := runnable.Invoke(ctx, map[string]any{"question": llm.BaselineQuestion})
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
