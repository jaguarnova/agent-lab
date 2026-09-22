// ch02/04_tools/main.go
// 阶段二练习 1：手写 Tool Calling 循环，不用 SDK。
//
// 协议要点：
//  1. 请求体里用 `tools` 声明工具：name / description / parameters(JSON Schema)
//  2. 模型不执行工具！它只返回 tool_calls（工具名 + JSON 字符串参数）
//  3. 你执行后，把结果作为 role=tool 的消息回传（带 tool_call_id）
//  4. 模型看到工具结果继续生成：要么再发起 tool_calls（继续循环），要么给最终回答
//
// 工具输入用 Go struct 描述，经反射生成 JSON Schema（手写迷你版，无依赖）。
// 另外记录 Tool Success Rate（阶段二验收项）。
//
// 前置：本地 PostgreSQL（pg-agent 容器）已建 sales 表。
// 用法：OPENAI_BASE_URL=... OPENAI_API_KEY=sk-xxx MODEL=deepseek-flash go run ./ch02/04_tools
package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql 驱动
)

// ---------- 工具参数 struct：用反射生成 JSON Schema ----------

type SQLQueryArgs struct {
	SQL string `json:"sql" desc:"要执行的只读 SQL 查询（仅允许 SELECT）"`
}

type ReadFileArgs struct {
	Path string `json:"path" desc:"要读取的文件路径"`
}

type HTTPGetArgs struct {
	URL string `json:"url" desc:"要请求的 HTTP/HTTPS 地址"`
}

// schemaFromStruct 把 struct 反射成 JSON Schema。
// 约定：json tag 作属性名，desc tag 作描述。支持 string / number / boolean。
// ponytail: 手写迷你版只覆盖基础类型；嵌套对象/数组等复杂场景换 invopop/jsonschema
func schemaFromStruct(v any) map[string]any {
	props := map[string]any{}
	var required []string
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	for i := range t.NumField() {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		prop := map[string]any{}
		switch f.Type.Kind() {
		case reflect.String:
			prop["type"] = "string"
		case reflect.Int, reflect.Int64, reflect.Float64:
			prop["type"] = "number"
		case reflect.Bool:
			prop["type"] = "boolean"
		default:
			prop["type"] = "string" // ponytail: 未知类型兜底为 string
		}
		if d := f.Tag.Get("desc"); d != "" {
			prop["description"] = d
		}
		props[name] = prop
		required = append(required, name) // 练习里所有字段必填
	}
	return map[string]any{"type": "object", "properties": props, "required": required}
}

// ---------- 工具定义与执行 ----------

type Tool struct {
	Name        string
	Description string
	Args        any                                                    // 参数 struct（用于生成 schema）
	Execute     func(ctx context.Context, args string) (string, error) // args 是模型给的 JSON 字符串
}

func mustArgs[T any](raw string) T {
	var a T
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		panic(fmt.Sprintf("模型给的参数不是合法 JSON: %v\n原始: %s", err, raw))
	}
	return a
}

func buildTools(db *sql.DB) []Tool {
	return []Tool{
		{
			Name:        "sql_query",
			Description: "在业务数据库上执行只读 SQL 查询（仅 SELECT），返回 JSON 行。表结构：sales(id, product, region, amount, sold_at)",
			Args:        SQLQueryArgs{},
			Execute: func(ctx context.Context, args string) (string, error) {
				a := mustArgs[SQLQueryArgs](args)
				// 安全边界：只读校验是工具自己的责任，不指望模型自觉
				if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(a.SQL)), "SELECT") {
					return "", fmt.Errorf("拒绝执行：只允许 SELECT 查询")
				}
				rows, err := db.QueryContext(ctx, a.SQL)
				if err != nil {
					return "", err // 错误原样返回给模型，让它自己纠正
				}
				defer rows.Close()
				cols, _ := rows.Columns()
				var out []map[string]any
				for rows.Next() {
					vals := make([]any, len(cols))
					ptrs := make([]any, len(cols))
					for i := range vals {
						ptrs[i] = &vals[i]
					}
					if err := rows.Scan(ptrs...); err != nil {
						return "", err
					}
					row := map[string]any{}
					for i, c := range cols {
						v := vals[i]
						if b, ok := v.([]byte); ok {
							v = string(b) // pgx 把 TEXT 返回为 []byte
						}
						row[c] = v
					}
					out = append(out, row)
				}
				j, _ := json.Marshal(out)
				return string(j), nil
			},
		},
		{
			Name:        "read_file",
			Description: "读取本地文本文件内容",
			Args:        ReadFileArgs{},
			Execute: func(ctx context.Context, args string) (string, error) {
				a := mustArgs[ReadFileArgs](args)
				b, err := os.ReadFile(a.Path)
				if err != nil {
					return "", err
				}
				if len(b) > 8*1024 {
					b = b[:8*1024] // ponytail: 截断 8KB，防止大文件撑爆上下文
				}
				return string(b), nil
			},
		},
		{
			Name:        "http_get",
			Description: "发起 GET 请求并返回响应体（截断前 2KB）",
			Args:        HTTPGetArgs{},
			Execute: func(ctx context.Context, args string) (string, error) {
				a := mustArgs[HTTPGetArgs](args)
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
				if err != nil {
					return "", err
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					return "", err
				}
				defer resp.Body.Close()
				b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
				return fmt.Sprintf("HTTP %d\n%s", resp.StatusCode, b), nil
			},
		},
	}
}

// ---------- OpenAI 兼容协议结构 ----------

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type ToolDef struct {
	Type     string         `json:"type"` // 固定 "function"
	Function map[string]any `json:"function"`
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	Temperature float64   `json:"temperature,omitempty"` // 工具调用决策：低随机
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

type Choice struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type ChatResponse struct {
	Choices []Choice `json:"choices"`
	Usage   struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// ---------- 主流程：Agent 循环雏形 ----------

const maxIterations = 6 // 最大轮数：防止模型无限发起工具调用

func main() {
	baseURL := getenv("OPENAI_BASE_URL", "https://api.openai.com/v1")
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "缺少 OPENAI_API_KEY")
		os.Exit(1)
	}
	db, err := sql.Open("pgx", getenv("DATABASE_URL", "postgres://postgres:agent123@localhost:5432/agent?sslmode=disable"))
	if err != nil {
		panic(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tools := buildTools(db)

	// 把工具定义翻译成协议格式
	var toolDefs []ToolDef
	for _, t := range tools {
		toolDefs = append(toolDefs, ToolDef{
			Type: "function",
			Function: map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  schemaFromStruct(t.Args),
			},
		})
	}

	messages := []Message{
		{Role: "system", Content: "你是数据分析助手。需要数据时调用工具查询，不要编造数字。回答用中文，简洁。"},
		{Role: "user", Content: "数据库里销售额最高的区域是哪个？该区域卖了多少种商品、总额多少？另外读一下 go.mod 的前几行告诉我项目叫什么。"},
	}

	// Tool Success Rate 统计（阶段二验收项）
	var toolCalls, toolOK int

	// ★ Agent 循环：模型发起工具调用就执行回传，直到它给出最终回答
	client := &http.Client{}
	for iter := 1; iter <= maxIterations; iter++ {
		reqBody, _ := json.Marshal(ChatRequest{
			Model:       getenv("MODEL", "deepseek-flash"),
			Messages:    messages,
			Tools:       toolDefs,
			Temperature: 0.2,
			MaxTokens:   1024,
		})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			panic(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, body))
		}

		var cr ChatResponse
		if err := json.Unmarshal(body, &cr); err != nil {
			panic(err)
		}
		msg := cr.Choices[0].Message
		messages = append(messages, msg) // assistant 消息（含 tool_calls）必须回传给服务端

		if len(msg.ToolCalls) == 0 {
			// 没有工具调用 = 最终回答，循环结束
			fmt.Printf("\n=== 最终回答（第 %d 轮）===\n%s\n", iter, msg.Content)
			fmt.Printf("=== 工具调用 %d 次，成功 %d 次，成功率 %.0f%% | tokens=%d ===\n",
				toolCalls, toolOK, pct(toolOK, toolCalls), cr.Usage.TotalTokens)
			return
		}

		fmt.Printf("--- 第 %d 轮：模型请求 %d 个工具调用 ---\n", iter, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			toolCalls++
			tool := findTool(tools, tc.Function.Name)
			var result string
			if tool == nil {
				result = fmt.Sprintf("错误：未知工具 %q", tc.Function.Name)
			} else {
				out, err := tool.Execute(ctx, tc.Function.Arguments)
				if err != nil {
					result = "工具执行出错: " + err.Error() // 错误喂回给模型自纠（阶段三的策略）
				} else {
					toolOK++
					result = out
				}
			}
			fmt.Printf("  [%s] 参数 %s → %d 字节结果\n", tc.Function.Name, tc.Function.Arguments, len(result))
			messages = append(messages, Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    result,
			})
		}
	}
	fmt.Fprintln(os.Stderr, "达到最大轮数仍未给出最终回答")
	os.Exit(1)
}

func findTool(tools []Tool, name string) *Tool {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b) * 100
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
