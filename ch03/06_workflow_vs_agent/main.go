// ch03/06_workflow_vs_agent/main.go
// 阶段三练习：同一个任务，用两种范式各实现一遍，亲眼看到边界在哪。
//
// 任务：分析 sales 表，输出"销售额最高的区域 + 一句话结论"。
//
//	Workflow 版（固定管线）：LLM 生成 SQL → 程序执行 → LLM 总结。三步写死，无循环。
//	Agent 版（自主循环）：  模型带工具自己决定查什么、查几轮，直到认为完成。
//
// 对照维度：tokens、延迟、可靠性（schema/SQL 出错时各自的表现）。
//
// 用法：OPENAI_BASE_URL=... OPENAI_API_KEY=sk-xxx go run ./ch03/06_workflow_vs_agent [workflow|agent|both]
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
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []any     `json:"tools,omitempty"`
	// Workflow 版需要强制输出 JSON；Agent 版不需要
	Temperature float64 `json:"temperature,omitempty"`
	MaxTokens   int     `json:"max_tokens,omitempty"`
}

type ChatResponse struct {
	Choices []struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

var baseURL, apiKey, model string

func chat(ctx context.Context, messages []Message, tools []any) (ChatResponse, error) {
	reqBody, _ := json.Marshal(ChatRequest{
		Model: model, Messages: messages, Tools: tools,
		Temperature: 0.2, MaxTokens: 1024,
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	var cr ChatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return ChatResponse{}, err
	}
	return cr, nil
}

func sqlQuery(db *sql.DB, q string) (string, error) {
	rows, err := db.Query(q)
	if err != nil {
		return "", err
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
				v = string(b)
			}
			row[c] = v
		}
		out = append(out, row)
	}
	j, _ := json.Marshal(out)
	return string(j), nil
}

// ---------- 范式一：固定 Workflow ----------

func runWorkflow(ctx context.Context, db *sql.DB) (string, Stats) {
	var st Stats
	start := time.Now()

	// 步骤 1（写死）：让 LLM 生成一条 SQL
	step1 := []Message{
		{Role: "system", Content: "你是 SQL 生成器。表结构：sales(id, product, region, amount, sold_at)。只输出一条 SQL 语句本身，不要解释、不要 markdown。任务：统计各区域销售总额并找出最高区域。"},
		{Role: "user", Content: "生成 SQL"},
	}
	cr, err := chat(ctx, step1, nil)
	if err != nil {
		return "workflow 失败: " + err.Error(), st
	}
	st.Tokens += cr.Usage.TotalTokens
	sqlText := cr.Choices[0].Message.Content
	if i := strings.Index(sqlText, "```"); i >= 0 { // 剥离 markdown 代码块
		sqlText = strings.Trim(strings.ReplaceAll(sqlText, "```", ""), "sql \n")
	}
	sqlText = strings.TrimSpace(sqlText)
	if sqlText == "" {
		return fmt.Sprintf("workflow 失败于步骤1：模型未输出 SQL（思考型模型可能把输出放在 reasoning 中）。%v", cr.Usage), st
	}
	fmt.Printf("[workflow 步骤1] 生成 SQL: %s\n", sqlText)

	// 步骤 2（写死）：程序执行 SQL。生成错了就在这里失败——没有自纠机会。
	data, err := sqlQuery(db, sqlText)
	if err != nil {
		return fmt.Sprintf("workflow 失败于 SQL 执行（无重试机制）: %v\n生成的是: %s", err, sqlText), st
	}
	fmt.Printf("[workflow 步骤2] 查询结果: %s\n", data)

	// 步骤 3（写死）：LLM 总结
	step3 := []Message{
		{Role: "system", Content: "你是数据分析师。根据数据给出一句话结论。"},
		{Role: "user", Content: "数据：" + data},
	}
	cr2, err := chat(ctx, step3, nil)
	if err != nil {
		return "workflow 失败: " + err.Error(), st
	}
	st.Tokens += cr2.Usage.TotalTokens

	st.Latency = time.Since(start)
	return cr2.Choices[0].Message.Content, st
}

// ---------- 范式二：自主 Agent ----------

var sqlToolSchema = []any{
	map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        "sql_query",
			"description": "在 sales 表上执行只读 SQL（仅 SELECT）。表结构：sales(id, product, region, amount, sold_at)",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"sql": map[string]any{"type": "string", "description": "只读 SQL"},
				},
				"required": []string{"sql"},
			},
		},
	},
}

func runAgent(ctx context.Context, db *sql.DB) (string, Stats) {
	var st Stats
	start := time.Now()

	messages := []Message{
		{Role: "system", Content: "你是数据分析助手。需要数据时调用工具查询，不要编造数字。回答用中文一句话。"},
		{Role: "user", Content: "统计各区域销售总额，告诉我最高的是哪个区域。"},
	}

	for round := range 6 { // 最大迭代：Agent 特有的护栏
		cr, err := chat(ctx, messages, sqlToolSchema)
		if err != nil {
			return "agent 失败: " + err.Error(), st
		}
		st.Tokens += cr.Usage.TotalTokens
		msg := cr.Choices[0].Message

		if cr.Choices[0].FinishReason != "tool_calls" {
			st.Latency = time.Since(start)
			return msg.Content, st
		}

		messages = append(messages, Message{Role: "assistant"})
		last := &messages[len(messages)-1]
		for _, tc := range msg.ToolCalls {
			fmt.Printf("[agent 第%d轮] 调用 %s(%s)\n", round+1, tc.Function.Name, tc.Function.Arguments)
			var args struct {
				SQL string `json:"sql"`
			}
			result := ""
			var execErr error
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				execErr = fmt.Errorf("参数解析失败: %w", err)
			} else {
				result, execErr = sqlQuery(db, args.SQL)
			}
			if execErr != nil {
				result = "SQL 出错: " + execErr.Error() // ★ 关键差异：错误回传，模型自纠
			}
			st.ToolCalls++
			tc.Type = "function"
			last.ToolCalls = append(last.ToolCalls, tc)
			messages = append(messages, Message{Role: "tool", ToolCallID: tc.ID, Content: result})
		}
	}
	return "agent 达到最大迭代", st
}

type Stats struct {
	Tokens    int
	Latency   time.Duration
	ToolCalls int
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "用法: go run ./ch03/06_workflow_vs_agent [workflow|agent|both]")
		os.Exit(1)
	}
	baseURL = getenv("OPENAI_BASE_URL", "https://api.openai.com/v1")
	apiKey = os.Getenv("OPENAI_API_KEY")
	model = getenv("MODEL", "deepseek-flash")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "缺少 OPENAI_API_KEY")
		os.Exit(1)
	}
	db, err := sql.Open("pgx", getenv("DATABASE_URL", "postgres://postgres:agent123@localhost:5432/agent?sslmode=disable"))
	if err != nil {
		panic(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	mode := os.Args[1]
	switch mode {
	case "workflow":
		ans, st := runWorkflow(ctx, db)
		printResult("Workflow", ans, st)
	case "agent":
		ans, st := runAgent(ctx, db)
		printResult("Agent", ans, st)
	case "both":
		wAns, wSt := runWorkflow(ctx, db)
		printResult("Workflow", wAns, wSt)
		fmt.Println()
		aAns, aSt := runAgent(ctx, db)
		printResult("Agent", aAns, aSt)
	}
}

func printResult(name string, ans string, st Stats) {
	fmt.Printf("\n=== %s 结果 ===\n%s\n[stats] tokens=%d latency=%v toolCalls=%d\n\n",
		name, ans, st.Tokens, st.Latency.Round(time.Millisecond), st.ToolCalls)
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
