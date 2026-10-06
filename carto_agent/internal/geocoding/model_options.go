package geocoding

import (
	"strings"

	openaiModel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
)

// NonThinkingOptions explicitly disables Qwen's default hybrid thinking only
// for this resolver. Official compatibility API documentation requires the
// enable_thinking field at the top level, not inside an extra_body object:
// https://www.alibabacloud.com/help/en/model-studio/qwen-api-via-openai-chat-completions
// Thinking-only, instruct-only, coder and unrelated models are left unchanged.
func NonThinkingOptions(modelName string) []model.Option {
	name := strings.ToLower(strings.TrimSpace(modelName))
	if name != "qwen3" && !strings.HasPrefix(name, "qwen3.") && !strings.HasPrefix(name, "qwen3-") && !strings.HasPrefix(name, "qwen3:") {
		return nil
	}
	for _, unsupported := range []string{"thinking", "instruct", "coder"} {
		if strings.Contains(name, unsupported) {
			return nil
		}
	}
	return []model.Option{openaiModel.WithExtraFields(map[string]any{"enable_thinking": false})}
}
