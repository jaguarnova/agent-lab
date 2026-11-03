// 阶段五练习 3：Stream。
//
// 与 ch01 手写 SSE 对照：Eino 把流统一成 *schema.StreamReader[*schema.Message]，
// 调用方只需 Recv（读一次、必须 Close），不再关心 chunk 帧格式。
//
// 用法：DEEPSEEK_API_KEY=sk-xxx go run ./ch05/03_stream
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/jaguarnova/agent-lab/ch05/internal/llm"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cm := llm.New(ctx, 200)

	messages := []*schema.Message{
		schema.SystemMessage("你是一个简洁的中文技术助手，回答不超过 100 字。"),
		schema.UserMessage("用一个类比解释 LLM 流式输出和一次性返回的区别。"),
	}

	start := time.Now()
	reader, err := cm.Stream(ctx, messages)
	if err != nil {
		log.Fatal(err)
	}
	defer reader.Close()

	var (
		content    strings.Builder
		chunks     int
		firstChunk time.Duration
		finish     string
		usage      *schema.TokenUsage
	)
	for {
		chunk, err := reader.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		if chunks == 0 {
			firstChunk = time.Since(start)
		}
		chunks++
		content.WriteString(chunk.Content)
		if chunk.ResponseMeta != nil {
			if chunk.ResponseMeta.FinishReason != "" {
				finish = chunk.ResponseMeta.FinishReason
			}
			if chunk.ResponseMeta.Usage != nil {
				usage = chunk.ResponseMeta.Usage
			}
		}
	}

	fmt.Printf("<<< 回复: %s\n", content.String())
	fmt.Printf("<<< chunks=%d 首块=%s 总耗时=%s finish=%s\n",
		chunks, firstChunk.Round(time.Millisecond), time.Since(start).Round(time.Millisecond), finish)
	if usage != nil {
		fmt.Printf("<<< Token 用量: prompt=%d completion=%d total=%d\n",
			usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
	}
}
