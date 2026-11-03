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
	"github.com/cloudwego/eino/compose"
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
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	base, err := llm.New(ctx, 512)
	if err != nil {
		return err
	}

	adder, err := addtool.New()
	if err != nil {
		return err
	}
	info, err := adder.Info(ctx)
	if err != nil {
		return err
	}
	withTools, err := base.WithTools([]*schema.ToolInfo{info})
	if err != nil {
		return err
	}

	messages := []*schema.Message{
		schema.SystemMessage("需要算术时调用 add 工具，不要心算。"),
		schema.UserMessage("17 + 25 等于多少？"),
	}

	start := time.Now()
	assistant, err := withTools.Generate(ctx, messages)
	latency := time.Since(start)
	if err != nil {
		return err
	}
	if assistant == nil || assistant.ResponseMeta == nil {
		return fmt.Errorf("模型工具响应缺少消息或完成元数据")
	}
	if assistant.ResponseMeta.FinishReason != "tool_calls" {
		return fmt.Errorf("模型工具响应未完成工具调用: finish_reason=%q", assistant.ResponseMeta.FinishReason)
	}
	if len(assistant.ToolCalls) == 0 {
		return fmt.Errorf("模型没有调用工具，content=%q", assistant.Content)
	}
	for _, call := range assistant.ToolCalls {
		fmt.Printf("<<< 模型请求工具: %s(%s)，决策耗时=%s\n",
			call.Function.Name, call.Function.Arguments, latency.Round(time.Millisecond))
	}

	toolsNode, err := compose.NewToolNode(ctx, &compose.ToolsNodeConfig{Tools: []tool.BaseTool{adder}})
	if err != nil {
		return err
	}
	toolMessages, err := toolsNode.Invoke(ctx, assistant)
	if err != nil {
		return err
	}
	for _, msg := range toolMessages {
		fmt.Printf("<<< 工具结果 [%s]: %s\n", msg.Role, msg.Content)
	}

	conversation := append(messages, assistant)
	conversation = append(conversation, toolMessages...)
	final, err := withTools.Generate(ctx, conversation)
	if err != nil {
		return err
	}
	if err := llm.ValidateFinal(final); err != nil {
		return err
	}
	fmt.Printf("<<< 最终回答: %s\n", final.Content)
	return nil
}
