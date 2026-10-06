package subagents

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cloudwego/eino/components/tool"
)

// NewUnknownToolsHandler returns an error observation to the model so it can
// correct an unavailable tool name within the normal agent iteration limit.
// It never executes, registers, or forwards the unknown call. Browser guidance
// is only supplied when the role's filtered catalog includes the wrapper.
func NewUnknownToolsHandler(ctx context.Context, tools []tool.BaseTool) func(context.Context, string, string) (string, error) {
	allowed := make(map[string]bool, len(tools))
	for _, t := range tools {
		info, err := t.Info(ctx)
		if err == nil && info != nil {
			allowed[info.Name] = true
		}
	}
	return func(ctx context.Context, name, _ string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		guidance := "此调用没有执行。请使用本轮提供的工具名称及参数 Schema，不要根据说明文字或工具返回值自行创建顶层工具名；没有所需工具时向任务编排说明能力限制。"
		if strings.HasPrefix(name, "browser_") && allowed["browser_call_tool"] {
			guidance = "此调用没有执行。浏览器 MCP 返回的是内部工具名，不能直接作为顶层工具调用。"
			if allowed["browser_list_tools"] {
				guidance += "请根据 browser_list_tools 返回的工具列表及 inputSchema 确认名称和参数（未获取列表时先调用 browser_list_tools）。"
			}
			guidance += "确认后调用顶层 browser_call_tool，将内部名称放入 tool_name，内部参数对象放入 args；不要重复直接调用内部名称。"
		}
		result := struct {
			Success  bool              `json:"success"`
			Executed bool              `json:"executed"`
			Error    map[string]string `json:"error"`
			Guidance string            `json:"guidance"`
		}{
			Error: map[string]string{
				"code": "TOOL_NOT_AVAILABLE", "message": "当前智能体没有注册此工具", "tool_name": name,
			},
			Guidance: guidance,
		}
		encoded, err := json.Marshal(result)
		return string(encoded), err
	}
}
