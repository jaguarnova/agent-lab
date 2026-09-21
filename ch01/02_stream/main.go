// ch01/02_stream/main.go
// 阶段一练习 2：SSE 流式输出（"打字机"效果），仍然不用 SDK。
//
// SSE（Server-Sent Events）协议要点：
//   - HTTP 响应 Content-Type: text/event-stream，body 是一个持久的文本流
//   - 每条事件以空行分隔，形如:
//       data: {"id":"...","choices":[{"delta":{"content":"你"}}]}
//   - 流结束时服务端发送: data: [DONE]
//   - 与非流式的区别：choices[].delta（增量）替代 choices[].message（全量）
//
// 用法：OPENAI_BASE_URL=... OPENAI_API_KEY=sk-xxx go run ./ch01/02_stream
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

func main() {
	baseURL := getenv("OPENAI_BASE_URL", "https://api.openai.com/v1")
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "缺少 OPENAI_API_KEY 环境变量")
		os.Exit(1)
	}

	reqBody, _ := json.Marshal(ChatRequest{
		Model: getenv("MODEL", "deepseek-chat"),
		Messages: []Message{
			{Role: "system", Content: "你是一个简洁的中文技术助手。"},
			{Role: "user", Content: "用 3 句话解释 SSE 协议。"},
		},
		Stream: true,
	})

	req, _ := http.NewRequest(http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	// 流式响应不能用 client.Timeout 一刀切（总时长不可预估），
	// 正确做法：不设总超时，靠 context 控制生命周期，读操作有 bufio 的阻塞但服务端持续发数据。
	// ponytail: 生产环境应加 idle timeout（如 30s 无新数据则断开），练习版从简
	client := &http.Client{}
	start := time.Now()

	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "请求失败 HTTP %d: %s\n", resp.StatusCode, body)
		os.Exit(1)
	}

	fmt.Printf(">>> HTTP %d，开始流式接收（耗时 %v）\n<<< ", resp.StatusCode, time.Since(start))

	scanner := bufio.NewScanner(resp.Body)
	// SSE 单行可能很长，把 buffer 上限放宽到 1MB
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var full strings.Builder
	chunks := 0
	for scanner.Scan() {
		line := scanner.Text()
		// SSE 协议：以 "data: " 开头的行才有数据；空行是事件分隔符；"data: [DONE]" 是结束标记
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
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
		fmt.Fprintf(os.Stderr, "\n读取流出错: %v\n", err)
	}

	fmt.Printf("\n\n<<< 共 %d 个增量 chunk，总耗时 %v，全文 %d 字\n", chunks, time.Since(start), len(full.String()))
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
