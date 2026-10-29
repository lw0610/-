package deepagent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/fileops"
	"github.com/autocarto/carto_agent/internal/runtimecontrol"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Supply document settings and reference roots without changing the role's
// tools. In particular, a document URL can only use an assigned downloader.
func newToolContextMiddleware(ctx context.Context, cfg *Config, provider *config.LLMProviderConfig, selected []tool.BaseTool, skillDirs []string) *toolContextMiddleware {
	settings := config.AcquisitionConfig{}.WithDefaults()
	if cfg.Acquisition != nil {
		settings = cfg.Acquisition().WithDefaults()
	}
	options := fileops.DocumentOptions{Config: settings, Vision: provider != nil && provider.Vision}
	for _, selectedTool := range selected {
		info, _ := selectedTool.Info(ctx)
		if info == nil || info.Name != "web_download_file" {
			continue
		}
		if download, ok := selectedTool.(tool.EnhancedInvokableTool); ok {
			options.Download = func(ctx context.Context, url string) (map[string]any, error) {
				args, _ := json.Marshal(map[string]string{"url": url})
				result, err := download.InvokableRun(ctx, &schema.ToolArgument{Text: string(args)})
				if err != nil {
					return nil, err
				}
				for _, part := range result.Parts {
					if part.Type != schema.ToolPartTypeText {
						continue
					}
					var payload map[string]any
					if err = json.Unmarshal([]byte(part.Text), &payload); err != nil {
						return nil, err
					}
					if _, ok := payload["path"].(string); ok {
						return payload, nil
					}
				}
				return nil, fmt.Errorf("下载工具未返回原件文件路径")
			}
		}
	}
	return &toolContextMiddleware{options: options, referenceDirs: skillDirs}
}

type toolContextMiddleware struct {
	adk.BaseChatModelAgentMiddleware
	options       fileops.DocumentOptions
	referenceDirs []string
}

func (m *toolContextMiddleware) BeforeAgent(ctx context.Context, run *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	ctx = runtimecontrol.WithReferencePackageDirs(ctx, m.referenceDirs)
	return fileops.WithDocumentOptions(ctx, m.options), run, nil
}
