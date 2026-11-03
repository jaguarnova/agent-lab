// 阶段五练习 7：ReAct Agent。
//
// react.NewAgent 把「模型决策 → 工具执行 → 结果回填」的循环装进 Graph；
// 我们只提供 ToolCallingModel、工具集合和最大步数。
//
// 用法：DEEPSEEK_API_KEY=sk-xxx go run ./ch05/07_react_agent
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
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
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	adder, err := utils.InferTool("add", "计算两个整数之和；需要算术时调用",
		func(_ context.Context, in addInput) (addOutput, error) {
			fmt.Printf(">>> [tool] add(%d, %d)\n", in.A, in.B)
			return addOutput{Sum: in.A + in.B}, nil
		})
	if err != nil {
		log.Fatal(err)
	}

	agent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: llm.New(ctx, 512),
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: []tool.BaseTool{adder},
		},
		MaxStep: 8,
	})
	if err != nil {
		log.Fatal(err)
	}

	start := time.Now()
	resp, err := agent.Generate(ctx, []*schema.Message{
		schema.SystemMessage("需要算术时调用 add 工具，不要心算。"),
		schema.UserMessage("17 + 25 等于多少？只回答最终数字和一句解释。"),
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("<<< 最终回答: %s\n", resp.Content)
	fmt.Printf("<<< 耗时: %s\n", time.Since(start).Round(time.Millisecond))
}
