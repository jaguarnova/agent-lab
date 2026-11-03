package llm

import (
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// ValidateFinal 区分最终答案、仍待执行的工具请求和未完成的生成。
func ValidateFinal(message *schema.Message) error {
	if message == nil {
		return fmt.Errorf("模型未返回响应")
	}
	if len(message.ToolCalls) != 0 {
		return fmt.Errorf("模型仍请求工具执行，尚未返回最终回答")
	}
	if message.ResponseMeta == nil {
		return fmt.Errorf("模型响应缺少完成状态")
	}
	return ValidateCompletion(message.ResponseMeta.FinishReason, strings.TrimSpace(message.Content) != "")
}

// ValidateCompletion 也供流式调用使用，不要求保留已经输出的全部文本。
func ValidateCompletion(finishReason string, hasContent bool) error {
	if finishReason != "stop" {
		return fmt.Errorf("模型生成未完成: finish_reason=%q", finishReason)
	}
	if !hasContent {
		return fmt.Errorf("模型未返回非空最终回答")
	}
	return nil
}
