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

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	agentoptions "github.com/cloudwego/eino/flow/agent"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"

	"github.com/jaguarnova/agent-lab/ch05/internal/addtool"
	"github.com/jaguarnova/agent-lab/ch05/internal/llm"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	adder, err := addtool.New()
	if err != nil {
		return err
	}

	base, err := llm.New(ctx, 512)
	if err != nil {
		return err
	}

	agent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: base,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: []tool.BaseTool{adder},
		},
		MaxStep: 8,
	})
	if err != nil {
		return err
	}

	trace := callbacks.NewHandlerBuilder().
		OnStartFn(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
			if info.Component == components.ComponentOfTool {
				if in := tool.ConvCallbackInput(input); in != nil {
					fmt.Printf(">>> [tool] %s(%s)\n", info.Name, in.ArgumentsInJSON)
				}
			}
			return ctx
		}).
		Build()

	start := time.Now()
	resp, err := agent.Generate(ctx, []*schema.Message{
		schema.SystemMessage("需要算术时调用 add 工具，不要心算。"),
		schema.UserMessage("17 + 25 等于多少？只回答最终数字和一句解释。"),
	}, agentoptions.WithComposeOptions(compose.WithCallbacks(trace)))
	if err != nil {
		return err
	}

	if err := llm.ValidateFinal(resp); err != nil {
		return err
	}
	fmt.Printf("<<< 最终回答: %s\n", resp.Content)
	fmt.Printf("<<< 耗时: %s\n", time.Since(start).Round(time.Millisecond))
	return nil
}
