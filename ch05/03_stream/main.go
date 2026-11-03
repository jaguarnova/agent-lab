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
	"os"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/jaguarnova/agent-lab/ch05/internal/llm"
)

func main() {
	if err := run(context.Background(), os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(parent context.Context, out io.Writer) error {
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cm, err := llm.New(ctx, llm.BaselineMaxTokens)
	if err != nil {
		return err
	}

	start := time.Now()
	reader, err := cm.Stream(ctx, llm.BaselineMessages())
	if err != nil {
		return fmt.Errorf("创建流式回复: %w", err)
	}
	defer reader.Close()

	var (
		chunks         int
		firstChunk     time.Duration
		firstAnswer    time.Duration
		total          time.Duration
		firstChunkKind string
		hasContent     bool
		answerStarted  bool
		finish         string
		usage          *schema.TokenUsage
	)
	for {
		chunk, err := reader.Recv()
		if errors.Is(err, io.EOF) {
			total = time.Since(start)
			break
		}
		if err != nil {
			return fmt.Errorf("读取流式回复: %w", err)
		}
		visible := strings.TrimSpace(chunk.Content) != ""
		if chunks == 0 {
			firstChunk = time.Since(start)
			firstChunkKind = "回答内容"
			if !visible {
				firstChunkKind = "仅推理/空白/元数据（非首答）"
			}
		}
		chunks++
		if visible && !hasContent {
			firstAnswer = time.Since(start)
			hasContent = true
		}
		if len(chunk.ToolCalls) != 0 {
			return fmt.Errorf("回答未完成: 流式回复包含未执行的工具调用")
		}
		if chunk.ResponseMeta != nil {
			if chunk.ResponseMeta.FinishReason != "" {
				finish = chunk.ResponseMeta.FinishReason
			}
			if chunk.ResponseMeta.Usage != nil {
				usage = chunk.ResponseMeta.Usage
			}
		}
		if chunk.Content != "" {
			if !answerStarted {
				if _, err := io.WriteString(out, "<<< 回复: "); err != nil {
					return fmt.Errorf("输出回复: %w", err)
				}
				answerStarted = true
			}
			if _, err := io.WriteString(out, chunk.Content); err != nil {
				return fmt.Errorf("输出回复: %w", err)
			}
		}
	}

	if err := llm.ValidateCompletion(finish, hasContent); err != nil {
		return fmt.Errorf("流式回复: %w", err)
	}
	if _, err := fmt.Fprintf(out, "\n<<< chunks=%d 首块=%s 首答=%s 总耗时=%s 首块类型=%s finish=%s\n",
		chunks, firstChunk, firstAnswer, total, firstChunkKind, finish); err != nil {
		return fmt.Errorf("输出流式统计: %w", err)
	}
	if usage != nil {
		if _, err := fmt.Fprintf(out, "<<< Token 用量: prompt=%d completion=%d total=%d\n",
			usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens); err != nil {
			return fmt.Errorf("输出 Token 用量: %w", err)
		}
	}
	return nil
}
