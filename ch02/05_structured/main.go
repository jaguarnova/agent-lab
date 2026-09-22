// ch02/05_structured/main.go
// 阶段二练习 2：Structured Output——让模型输出符合 Go struct 的 JSON，
// 校验失败把错误喂回去重试，直到合法（最多 3 次）。
//
// 为什么需要结构化输出：Agent 的下游是代码不是人。SQL 结果要进图表库、
// 报告要进 API 响应，字段名和类型必须严格可控，"差不多像 JSON"不行。
//
// 三层防线（本例全实现）：
//  1. Prompt 约束：把 JSON Schema 贴进 prompt，明确"只输出 JSON"
//  2. 语法校验：json.Unmarshal 失败 → 重试
//  3. 语义校验：自定义业务规则（数值范围、必填字段）→ 带错误信息重试
//
// 用法：OPENAI_BASE_URL=... OPENAI_API_KEY=sk-xxx go run ./ch02/05_structured
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"
)

// ---------- 期望的输出结构 ----------

// SalesReport 是我们要让模型填充的结构。JSON Schema 同时用于 prompt 约束和校验。
type SalesReport struct {
	Region       string  `json:"region"        jsonschema:"string,销售额最高的区域名"`
	TotalAmount  float64 `json:"total_amount"  jsonschema:"number,该区域销售总额"`
	ProductKinds int     `json:"product_kinds" jsonschema:"integer,该区域商品种类数"`
	Summary      string  `json:"summary"       jsonschema:"string,一句话总结，不超过 50 字"`
}

// buildSchema：把 struct 的 jsonschema tag 翻译成 JSON Schema（与 04_tools 同思路）
func buildSchema(v any) map[string]any {
	props := map[string]any{}
	required := []string{}
	t := reflectTypeOf(v)
	for i := range t.NumField() {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		typ, desc := strings.Split(f.Tag.Get("jsonschema"), ",")[0], strings.SplitN(f.Tag.Get("jsonschema"), ",", 2)[1]
		var jt string
		switch typ {
		case "integer":
			jt = "integer"
		case "number":
			jt = "number"
		default:
			jt = "string"
		}
		props[name] = map[string]any{"type": jt, "description": desc}
		required = append(required, name)
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

// validate：语法之外的业务校验。返回的错误会原样喂回模型。
func validate(r SalesReport) error {
	if r.TotalAmount <= 0 {
		return fmt.Errorf("total_amount 必须为正数，得到 %v", r.TotalAmount)
	}
	if r.ProductKinds <= 0 {
		return fmt.Errorf("product_kinds 必须为正整数，得到 %v", r.ProductKinds)
	}
	if len(r.Summary) == 0 || len(r.Summary) > 100 {
		return fmt.Errorf("summary 必须为 1~100 字符，得到 %d 字", len(r.Summary))
	}
	return nil
}

const maxAttempts = 3

func main() {
	baseURL := getenv("OPENAI_BASE_URL", "https://api.openai.com/v1")
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "缺少 OPENAI_API_KEY")
		os.Exit(1)
	}

	schema := buildSchema(SalesReport{})
	schemaJSON, _ := json.MarshalIndent(schema, "", "  ")

	rawData := `2026 年 8-9 月销售明细（JSON）：
[{"product":"键盘","region":"华东","amount":1200.00},{"product":"键盘","region":"华南","amount":800.00},
 {"product":"鼠标","region":"华东","amount":600.50},{"product":"显示器","region":"华北","amount":4500.00},
 {"product":"显示器","region":"华东","amount":3900.00},{"product":"键盘","region":"华北","amount":950.00},
 {"product":"鼠标","region":"华南","amount":720.00},{"product":"显示器","region":"华南","amount":5100.00}]`

	// 第一轮 prompt：schema 进 prompt 是第一层防线
	system := "你是数据结构化助手。严格按给定的 JSON Schema 输出，只输出一个 JSON 对象，不要任何解释、不要 markdown 代码块。\nJSON Schema:\n" + string(schemaJSON)

	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	messages := []msg{
		{Role: "system", Content: system},
		{Role: "user", Content: "把下面的销售明细整理成销售报告 JSON：\n" + rawData},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := &http.Client{}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		reqBody, _ := json.Marshal(map[string]any{
			"model":       getenv("MODEL", "deepseek-flash"),
			"messages":    messages,
			"temperature": 0, // 结构化输出：确定性优先
			"max_tokens":  500,
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

		// 解析出文本（兼容模型把 JSON 包进 ```json 代码块的情况）
		var cr struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &cr); err != nil {
			panic(err)
		}
		content := cr.Choices[0].Message.Content
		content = strings.TrimSpace(content)
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)

		// 第二层防线：语法校验
		var report SalesReport
		if err := json.Unmarshal([]byte(content), &report); err != nil {
			fmt.Fprintf(os.Stderr, "[第 %d 次尝试] 语法校验失败: %v\n", attempt, err)
			messages = append(messages,
				msg{Role: "assistant", Content: content},
				msg{Role: "user", Content: fmt.Sprintf("你的输出不是合法 JSON 或不符合 schema：%v。请重新输出，只输出一个合法 JSON 对象。", err)},
			)
			continue
		}

		// 第三层防线：业务校验
		if err := validate(report); err != nil {
			fmt.Fprintf(os.Stderr, "[第 %d 次尝试] 业务校验失败: %v\n", attempt, err)
			messages = append(messages,
				msg{Role: "assistant", Content: content},
				msg{Role: "user", Content: fmt.Sprintf("输出不符合业务规则：%v。请修正后重新输出完整 JSON。", err)},
			)
			continue
		}

		// 全部通过
		pretty, _ := json.MarshalIndent(report, "", "  ")
		fmt.Printf("=== 成功（第 %d 次尝试）===\n%s\n", attempt, pretty)
		return
	}
	fmt.Fprintln(os.Stderr, "重试耗尽，模型始终无法输出合法结构")
	os.Exit(1)
}

func reflectTypeOf(v any) reflect.Type { return reflect.TypeOf(v) }

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
