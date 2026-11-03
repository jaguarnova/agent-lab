// 阶段五练习 4：ToolsNode。
//
// 拆开 ReAct 的两半：ChatModel.WithTools 负责让模型产出 ToolCalls，
// ToolsNode 负责按 ToolCalls 执行工具并返回 ToolMessage。这里手动串一轮，
// 看清框架替我们做了什么，也看清哪里仍要自己控制循环。
//
// 用法：DEEPSEEK_API_KEY=sk-xxx go run ./ch05/04_tools_node
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/jaguarnova/agent-lab/ch05/internal/llm"
)

type addInput struct {
	A int `json:"a" jsonschema:"第一个加数"`
	B int `json:"b" jsonschema:"第二个加数"`
}

type addOutput struct {
	Sum int `json:"sum"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	base := llm.New(ctx, 512)

	adder, err := utils.InferTool("add", "计算两个整数之和；需要算术时调用",
		func(_ context.Context, in addInput) (addOutput, error) {
			return addOutput{Sum: in.A + in.B}, nil
		})
	if err != nil {
		log.Fatal(err)
	}
	info, err := adder.Info(ctx)
	if err != nil {
		log.Fatal(err)
	}
	withTools, err := base.WithTools([]*schema.ToolInfo{info})
	if err != nil {
		log.Fatal(err)
	}

	messages := []*schema.Message{
		schema.SystemMessage("需要算术时调用 add 工具，不要心算。"),
		schema.UserMessage("17 + 25 等于多少？"),
	}

	start := time.Now()
	assistant, err := withTools.Generate(ctx, messages)
	latency := time.Since(start)
	if err != nil {
		log.Fatal(err)
	}
	if len(assistant.ToolCalls) == 0 {
		log.Fatalf("模型没有调用工具，content=%q", assistant.Content)
	}
	fmt.Printf("<<< 模型请求工具: %s(%s)，决策耗时=%s\n",
		assistant.ToolCalls[0].Function.Name, assistant.ToolCalls[0].Function.Arguments, latency.Round(time.Millisecond))

	toolsNode, err := compose.NewToolNode(ctx, &compose.ToolsNodeConfig{Tools: []tool.BaseTool{adder}})
	if err != nil {
		log.Fatal(err)
	}
	toolMessages, err := toolsNode.Invoke(ctx, assistant)
	if err != nil {
		log.Fatal(err)
	}
	for _, msg := range toolMessages {
		fmt.Printf("<<< 工具结果 [%s]: %s\n", msg.Role, msg.Content)
	}

	conversation := append(messages, assistant)
	conversation = append(conversation, toolMessages...)
	final, err := withTools.Generate(ctx, conversation)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("<<< 最终回答: %s\n", final.Content)
}
