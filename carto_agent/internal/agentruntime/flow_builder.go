package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/deepagent"
	"github.com/autocarto/carto_agent/internal/geeruntime"
	bridgepkg "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

var _ FlowBuilder = (*Factory)(nil)

// BuildRole reuses the ordinary runtime projection, tools, models and middleware.
func (f *Factory) BuildRole(ctx context.Context, activeConfigName string, activeConfig *config.AgentConfigFile, roleName string) (adk.Agent, error) {
	cfg, err := f.deepAgentConfig(activeConfigName, activeConfig)
	if err != nil {
		return nil, err
	}
	return deepagent.BuildRoleAgent(ctx, cfg, roleName)
}

// CallFlowJSON uses the selected profile's main model for a single tool-free call.
func (f *Factory) CallFlowJSON(ctx context.Context, activeConfig *config.AgentConfigFile, prompt string) (string, error) {
	if activeConfig == nil || activeConfig.Runtime == nil {
		return "", fmt.Errorf("流程模型调用缺少方案配置")
	}
	var mainRole *config.AgentDefData
	for _, role := range activeConfig.Runtime.Agents {
		if !role.IsMain || !role.Enabled {
			continue
		}
		if mainRole != nil {
			return "", fmt.Errorf("方案配置了多个主角色")
		}
		selected := role
		mainRole = &selected
	}
	if mainRole == nil {
		return "", fmt.Errorf("方案没有已启用的主角色")
	}
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("流程模型调用内容为空")
	}
	chatModel, err := f.resolveChatModel(mainRole.LLM.Provider)
	if err != nil {
		return "", fmt.Errorf("解析流程主角色模型: %w", err)
	}
	if chatModel == nil {
		return "", fmt.Errorf("流程主角色模型不可用")
	}
	// WithTools returns a separate binding, preserving the shared model used by agents.
	jsonModel, err := chatModel.WithTools([]*schema.ToolInfo{})
	if err != nil {
		return "", fmt.Errorf("禁用流程模型工具: %w", err)
	}
	if jsonModel == nil {
		return "", fmt.Errorf("无工具流程模型不可用")
	}
	result, err := jsonModel.Generate(ctx, []*schema.Message{
		schema.SystemMessage("仅输出有效 JSON，不附加 Markdown、解释或工具调用。"),
		schema.UserMessage(prompt),
	}, model.WithTemperature(0), model.WithTools(nil), model.WithToolChoice(schema.ToolChoiceForbidden))
	if err != nil {
		return "", err
	}
	if result == nil || len(result.ToolCalls) > 0 {
		return "", fmt.Errorf("流程模型未返回 JSON 文本")
	}
	content := strings.TrimSpace(result.Content)
	if !json.Valid([]byte(content)) {
		return "", fmt.Errorf("流程模型输出不是有效 JSON")
	}
	return content, nil
}

// InvokeFlowTool exposes the controller observations and exact-attempt layer cleanup.
// These system calls are not granted to a model through this channel.
func (f *Factory) InvokeFlowTool(ctx context.Context, name string, args map[string]any) (any, error) {
	name = normalizeToolName(name)
	if name != "get_layers_info" && name != "layout_info" && name != "layer_inspect" && name != "layer_remove_created" {
		return nil, fmt.Errorf("流程上下文不允许调用工具: %s", name)
	}
	return f.invokeSystemFlowTool(ctx, name, args)
}

// InvokeAcquisitionTool is a separate deterministic system channel, never exposed as a model tool.
func (f *Factory) InvokeAcquisitionTool(ctx context.Context, name string, args map[string]any) (any, error) {
	name = normalizeToolName(name)
	switch name {
	case "admin_boundary_extract", "archive_extract", "data_normalize", "data_derive", "data_load_vector", "data_load_raster":
	case "data_load_tiles":
		var err error
		ctx, err = bridgepkg.AuthorizeConfirmedTileLoad(ctx, args)
		if err != nil {
			return nil, err
		}
	case "gee_run_script":
		template, _ := args["template"].(string)
		params, _ := args["params"].(map[string]any)
		if len(args) != 2 || template == "" || params == nil {
			return nil, fmt.Errorf("获取通道仅允许系统 GEE 模板参数")
		}
		root, err := config.ResolveAssetsRoot()
		if err != nil {
			return nil, err
		}
		script, err := geeruntime.BuildAcquisitionScript(root, template, params)
		if err != nil {
			return nil, err
		}
		args = map[string]any{"code": script}
	default:
		return nil, fmt.Errorf("数据获取不允许调用工具: %s", name)
	}
	return f.invokeSystemFlowTool(ctx, name, args)
}

func (f *Factory) invokeSystemFlowTool(ctx context.Context, name string, args map[string]any) (any, error) {
	var selected tool.BaseTool
	for _, item := range f.config.BaseTools {
		info, err := item.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("读取流程工具信息: %w", err)
		}
		if info == nil || normalizeToolName(info.Name) != name {
			continue
		}
		if selected != nil {
			return nil, fmt.Errorf("流程工具重复注册: %s", name)
		}
		selected = item
	}
	if selected == nil {
		return nil, fmt.Errorf("流程工具未注册: %s", name)
	}
	if args == nil {
		args = map[string]any{}
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("编码流程工具参数: %w", err)
	}
	var content string
	if enhanced, ok := selected.(tool.EnhancedInvokableTool); ok {
		result, invokeErr := enhanced.InvokableRun(ctx, &schema.ToolArgument{Text: string(encoded)})
		if invokeErr != nil {
			return nil, invokeErr
		}
		if result != nil {
			texts := []string{}
			for _, part := range result.Parts {
				if part.Type == schema.ToolPartTypeText && strings.TrimSpace(part.Text) != "" {
					texts = append(texts, part.Text)
				}
			}
			content = strings.Join(texts, "\n")
		}
	} else if invokable, ok := selected.(tool.InvokableTool); ok {
		content, err = invokable.InvokableRun(ctx, string(encoded))
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("流程工具不可执行: %s", name)
	}
	var payload any
	if err := json.Unmarshal([]byte(content), &payload); err != nil || payload == nil {
		return nil, fmt.Errorf("流程工具 %s 未返回有效 JSON", name)
	}
	if envelope, ok := payload.(map[string]any); ok {
		if success, exists := envelope["success"]; exists {
			flag, valid := success.(bool)
			if !valid {
				return nil, fmt.Errorf("流程工具 %s success 字段不是布尔值", name)
			}
			if !flag {
				message := "工具执行失败"
				for _, key := range []string{"error", "info", "message"} {
					if text, ok := envelope[key].(string); ok && strings.TrimSpace(text) != "" {
						message = text
						break
					}
				}
				code, _ := envelope["code"].(string)
				retryable, _ := envelope["retryable"].(bool)
				diagnostics, _ := envelope["provider_diagnostics"].(map[string]any)
				if code != "" {
					return nil, &bridgepkg.ToolInvokeError{Code: code, Message: message, Retryable: retryable, ProviderDiagnostics: diagnostics}
				}
				return nil, fmt.Errorf("流程工具 %s: %s", name, message)
			}
		}
	}
	return payload, nil
}
