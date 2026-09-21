// ch01/02_stream/main.go
// 阶段一练习 2：SSE 流式输出（"打字机"效果），仍然不用 SDK。
//
// SSE（Server-Sent Events）协议要点：
//   - HTTP 响应 Content-Type: text/event-stream，body 是一个持久的文本流
//   - 每条事件以空行分隔，形如:
//     data: {"id":"...","choices":[{"delta":{"content":"你"}}]}
//   - 流结束时服务端发送: data: [DONE]
//   - 与非流式的区别：choices[].delta（增量）替代 choices[].message（全量）
//
// 本版在生产化方向补了两件事（阶段一 P0：Timeout / Cancellation / Retry）：
//   1. Idle timeout：watchdog 定时器，30s 没有新数据就取消 context 断开流，
//      防止"连接还在但永远没有 [DONE]"的静默挂死（NAT 超时、上游卡死）。
//   2. 无状态重试：SSE 无法续流，但 LLM 调用是无状态的——messages 就是完整状态，
//      断了直接整体重发，最多 3 次，指数退避。
//
// 用法：OPENAI_BASE_URL=... OPENAI_API_KEY=sk-xxx go run ./ch01/02_stream
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"` // 关键开关：true = SSE 流式
}

type Delta struct {
	Content string `json:"content"`
}

type StreamChoice struct {
	Index        int    `json:"index"`
	Delta        Delta  `json:"delta"`
	FinishReason string `json:"finish_reason"`
}

type StreamResponse struct {
	Choices []StreamChoice `json:"choices"`
}

const (
	maxAttempts   = 3                // 断流重试次数
	retryBaseWait = 1 * time.Second  // 指数退避起步：1s, 2s, 4s
)

// idleTimeout 可用 IDLE_TIMEOUT 环境变量覆盖（秒），默认 30s。
// 正常流式 chunk 间隔是亚秒级；本地弱网调试可调大，测试可调小。
func idleTimeout() time.Duration {
	if s := getenv("IDLE_TIMEOUT", ""); s != "" {
		if d, err := time.ParseDuration(s); err == nil {
			return d
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Duration(n) * time.Second
		}
	}
	return 30 * time.Second
}

var errIdleTimeout = errors.New("idle timeout: 流超过空闲上限无新数据")

func main() {
	baseURL := getenv("OPENAI_BASE_URL", "https://api.openai.com/v1")
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "缺少 OPENAI_API_KEY 环境变量")
		os.Exit(1)
	}

	messages := []Message{
		{Role: "system", Content: "你是一个简洁的中文技术助手。"},
		{Role: "user", Content: "用 3 句话解释 SSE 协议。"},
	}

	// 无状态重试：messages 不变，断了整体重发。LLM 调用的断线恢复就这么简单。
	var full strings.Builder
	chunks := 0
	totalStart := time.Now()

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		start := time.Now()
		var err error
		chunks, full, err = streamOnce(baseURL, apiKey, messages)

		if err == nil {
			fmt.Printf("\n\n<<< 成功（第 %d 次尝试）：共 %d 个增量 chunk，本次耗时 %v，全文 %d 字\n",
				attempt, chunks, time.Since(start), len(full.String()))
			fmt.Printf("<<< 含重试总耗时 %v\n", time.Since(totalStart))
			return
		}

		// 不可恢复错误：HTTP 4xx（参数/鉴权错），重试也不会好，直接失败
		var httpErr *httpStatusError
		if errors.As(err, &httpErr) && httpErr.StatusCode >= 400 && httpErr.StatusCode < 500 {
			fmt.Fprintf(os.Stderr, "\n不可恢复错误 HTTP %d，放弃重试:\n%s\n", httpErr.StatusCode, httpErr.Body)
			os.Exit(1)
		}

		fmt.Fprintf(os.Stderr, "\n[第 %d/%d 次尝试失败: %v]\n", attempt, maxAttempts, err)
		if attempt < maxAttempts {
			wait := retryBaseWait * time.Duration(1<<(attempt-1)) // 1s, 2s, 4s
			fmt.Fprintf(os.Stderr, "[退避 %v 后重试]\n", wait)
			time.Sleep(wait)
		}
	}
	fmt.Fprintln(os.Stderr, "重试耗尽，失败")
	os.Exit(1)
}

// httpStatusError 区分可重试（5xx/网络）与不可重试（4xx）的失败
type httpStatusError struct {
	StatusCode int
	Body       string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("HTTP %d", e.StatusCode)
}

// streamOnce 发起一次流式调用并完整读完（或中途断开）。
// full 返回本次已收到的部分内容（即使失败也返回，便于观察"半截回复"）。
func streamOnce(baseURL, apiKey string, messages []Message) (int, strings.Builder, error) {
	var full strings.Builder
	chunks := 0

	reqBody, _ := json.Marshal(ChatRequest{
		Model:    getenv("MODEL", "deepseek-flash"),
		Messages: messages,
		Stream:   true,
	})
	req, err := http.NewRequest(http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return 0, full, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	// context 承担整体生命周期（取消会中断阻塞中的 body 读取）
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ★ Idle timeout watchdog：每收到一行就 Reset；定时器先触发 = 流挂死，cancel 断开。
	// 正常流式的 chunk 间隔是亚秒级，30s 无数据必然异常。
	timer := time.AfterFunc(idleTimeout(), func() { cancel() })
	defer timer.Stop()

	start := time.Now()
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return 0, full, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, full, &httpStatusError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	fmt.Printf(">>> HTTP %d，开始流式接收（耗时 %v）\n<<< ", resp.StatusCode, time.Since(start))

	scanner := bufio.NewScanner(resp.Body)
	// SSE 单行可能很长，把 buffer 上限放宽到 1MB
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		timer.Reset(idleTimeout()) // ★ 收到数据，续命
		line := scanner.Text()
		// SSE 协议：以 "data: " 开头的行才有数据；空行是事件分隔符；"data: [DONE]" 是结束标记
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			return chunks, full, nil
		}

		var sr StreamResponse
		if err := json.Unmarshal([]byte(payload), &sr); err != nil {
			fmt.Fprintf(os.Stderr, "\n[跳过无法解析的 chunk: %s]\n", payload)
			continue
		}
		for _, c := range sr.Choices {
			if c.Delta.Content != "" {
				fmt.Print(c.Delta.Content) // 打字机效果：逐增量打印
				full.WriteString(c.Delta.Content)
				chunks++
			}
		}
	}
	if err := scanner.Err(); err != nil {
		// context 被 watchdog 取消时，阻塞中的 Read 会以这个错误醒来
		if errors.Is(err, context.Canceled) {
			return chunks, full, errIdleTimeout
		}
		return chunks, full, err // 网络错误（RST 等），可重试
	}

	// 扫描正常结束但没收到 [DONE]：服务端提前关连接，视作可重试失败
	return chunks, full, fmt.Errorf("流在收到 [DONE] 前结束（已收 %d 字）", len(full.String()))
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
