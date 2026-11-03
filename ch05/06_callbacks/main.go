// 阶段五练习 6：Callback。
//
// 组件 standalone 运行时，用 callbacks.InitCallbacks 注入 RunInfo 与 handler；
// OnStart/OnEnd/OnError 拿到的是组件相关的 CallbackInput/Output，可在不改业务代码的前提下埋点。
//
// 用法：DEEPSEEK_API_KEY=sk-xxx go run ./ch05/06_callbacks
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/jaguarnova/agent-lab/ch05/internal/llm"
)

func main() {
	baseCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	handler := callbacks.NewHandlerBuilder().
		OnStartFn(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
			in := model.ConvCallbackInput(input)
			if in != nil {
				fmt.Printf("[callback] start component=%s name=%s messages=%d\n",
					info.Component, info.Name, len(in.Messages))
			}
			return ctx
		}).
		OnEndFn(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
			out := model.ConvCallbackOutput(output)
			if out != nil && out.Message != nil {
				tokens := 0
				if out.Message.ResponseMeta != nil && out.Message.ResponseMeta.Usage != nil {
					tokens = out.Message.ResponseMeta.Usage.TotalTokens
				}
				fmt.Printf("[callback] end component=%s name=%s tokens=%d content_len=%d\n",
					info.Component, info.Name, tokens, len(out.Message.Content))
			}
			return ctx
		}).
		OnErrorFn(func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
			fmt.Printf("[callback] error component=%s err=%v\n", info.Component, err)
			return ctx
		}).
		Build()

	ctx := callbacks.InitCallbacks(baseCtx, &callbacks.RunInfo{
		Component: components.ComponentOfChatModel,
		Name:      "deepseek-flash",
	}, handler)

	cm := llm.New(ctx, 200)
	resp, err := cm.Generate(ctx, []*schema.Message{
		schema.SystemMessage("你是一个简洁的中文技术助手，回答不超过 50 字。"),
		schema.UserMessage("一句话说明 callback 在 Agent 系统里的作用。"),
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("<<< 回复: %s\n", resp.Content)
}
