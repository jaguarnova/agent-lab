// Package addtool provides the shared integer addition tool for the examples.
package addtool

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

type input struct {
	A *int `json:"a" jsonschema:"description=第一个加数"`
	B *int `json:"b" jsonschema:"description=第二个加数"`
}

type output struct {
	Sum int `json:"sum"`
}

// New returns an inferred add tool that requires both operands, including zero.
func New() (tool.InvokableTool, error) {
	return utils.InferTool("add", "计算两个整数之和；需要算术时调用",
		func(_ context.Context, in input) (output, error) {
			if in.A == nil || in.B == nil {
				return output{}, fmt.Errorf("add requires non-null operands a and b")
			}
			return output{Sum: *in.A + *in.B}, nil
		})
}
