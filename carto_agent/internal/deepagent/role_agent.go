package deepagent

import (
	"context"
	"fmt"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/logger"
	"github.com/cloudwego/eino/adk"
)

// BuildRoleAgent constructs the same authorized role for delegation or a flow step.
// FlowOnly roles are intentionally allowed here and filtered only from delegation.
func BuildRoleAgent(ctx context.Context, cfg *Config, roleName string) (adk.Agent, error) {
	if cfg == nil || cfg.SkillRuntimeConfig == nil {
		return nil, fmt.Errorf("角色运行配置未提供")
	}
	agentName := strings.TrimSpace(roleName)
	if agentName == DataAcquisitionExpertName {
		// A planned acquisition uses the same system-owned expert as ordinary
		// delegation. Never synthesize a user role or bypass its guarded tools.
		experts, err := createSystemExperts(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("创建系统角色 %s: %w", agentName, err)
		}
		for _, expert := range experts {
			if expert.Name(ctx) == agentName {
				return expert, nil
			}
		}
		return nil, fmt.Errorf("系统角色 %s 不可用：需要执行模式及已注册的数据服务工具", agentName)
	}
	agentConfig, exists := cfg.SkillRuntimeConfig.Agents[agentName]
	if !exists || agentName == "" {
		return nil, fmt.Errorf("角色不存在: %s", roleName)
	}
	if !agentConfig.Enabled {
		return nil, fmt.Errorf("角色已禁用: %s", agentName)
	}
	kind := strings.TrimSpace(agentConfig.Kind)
	if kind == "" {
		kind = "agent"
	} // Internal v1 structs may predate schema migration.
	if kind != "agent" && kind != "executor" {
		return nil, fmt.Errorf("角色 %s 类型无效: %s", agentName, kind)
	}
	if kind == "agent" && strings.TrimSpace(agentConfig.Executor) != "" {
		return nil, fmt.Errorf("通用角色 %s 不能配置专用执行器", agentName)
	}
	engine := cfg.TemplateEngine
	if engine == nil {
		engine = subagents.NewTemplateEngine(cfg.SkillRuntimeConfig)
	}
	userSkillsDir := strings.TrimSpace(cfg.UserSkillsDir)
	if userSkillsDir == "" {
		userSkillsDir = strings.TrimSpace(cfg.SkillsDir)
	}
	skillDirs := config.ResolveRuntimeSkillDirsWithPlugin(cfg.PluginSkillsDir, cfg.BuiltinSkillsDir, userSkillsDir, cfg.RuntimeConfigBaseDir, cfg.ActiveConfigName)
	// 创建工具过滤器
	filter := subagents.CreateRuntimeSkillToolFilter(agentName, cfg.SkillRuntimeConfig)

	// 过滤工具
	agentTools := filter.Filter(ctx, cfg.Tools)
	agentTools = filterAcquisitionTools(ctx, cfg, agentTools, false)

	chatModel := cfg.ChatModel
	providerCfg := cloneProviderConfig(cfg.DefaultProviderCfg)
	if llmCfg, ok := cfg.AgentLLMs[agentName]; ok {
		provider := strings.TrimSpace(llmCfg.Provider)
		if provider != "" {
			if cfg.ResolveAgentChatModel == nil {
				logger.Warn("智能体 %s 配置了独立 provider=%s，但解析器未设置，回退全局模型", agentName, provider)
			} else if resolved, resolveErr := cfg.ResolveAgentChatModel(provider); resolveErr != nil {
				logger.Warn("解析智能体 %s 独立模型失败(provider=%s): %v，回退全局模型", agentName, provider, resolveErr)
			} else if resolved != nil {
				chatModel = resolved
				// Model identity and runtime budgets must follow the model actually
				// selected, including when an independent model falls back above.
				providerCfg = nil
				if cfg.ResolveProviderCfg != nil {
					providerCfg = cloneProviderConfig(cfg.ResolveProviderCfg(provider))
				}
			}
		}
	}

	// The knowledge runtime uses the editable business rules directly. Generic
	// tool-routing instructions do not belong in its structured model requests.
	if kind == "executor" {
		if strings.TrimSpace(agentConfig.Executor) != "route_knowledge" {
			return nil, fmt.Errorf("角色 %s 的执行器类型未注册: %s", agentName, agentConfig.Executor)
		}
		instructions := strings.TrimSpace(agentConfig.Prompts.StaticInstructions)
		knowledgeAgent := createKnowledgeSubAgent(agentName, agentConfig.Description, chatModel, providerCfg, agentTools)
		if knowledgeAgent == nil {
			return nil, fmt.Errorf("知识执行器需要模型、抽取契约与编译工具，请检查角色授权包")
		}
		knowledgeAgent.instructions = instructions
		knowledgeAgent.resolveCoordinates = coordinateResolver(chatModel, cfg.RuntimeConfigBaseDir, providerCfg)
		return knowledgeAgent, nil
	}

	agentTools = subagents.WithAttachmentReadTool(ctx, agentTools, cfg.Tools)
	instruction, err := buildSubAgentPrompt(ctx, agentName, agentConfig, agentTools, engine)
	if err != nil {
		return nil, fmt.Errorf("Runtime 智能体 %s 提示词失败: %w", agentName, err)
	}

	// 加载子智能体的 skill middleware
	var middlewares []adk.AgentMiddleware
	middlewareSkillDirs, err := resolveMiddlewareSkillDirs(agentName, cfg.SkillRuntimeConfig, cfg.RegistryGovernance)
	if err != nil {
		return nil, fmt.Errorf("resolve middleware packages for %s: %w", agentName, err)
	}
	if len(middlewareSkillDirs) > 0 {
		skillMw, err := subagents.LoadSkillMiddleware(ctx, agentName, middlewareSkillDirs, skillDirs, cfg.SkillRuntimeConfig)
		if err != nil {
			return nil, fmt.Errorf("load skill middleware for %s: %w", agentName, err)
		}
		if skillMw != nil {
			middlewares = append(middlewares, *skillMw)
		}
	}

	handlers, err := buildRuntimeHandlers(ctx, agentName, chatModel, providerCfg, runtimeHandlerOptions{
		ActiveConfigName: cfg.ActiveConfigName,
		ConfigBaseDir:    cfg.RuntimeConfigBaseDir,
	})
	if err != nil {
		return nil, fmt.Errorf("Runtime 智能体 %s 运行时 handlers 失败: %w", agentName, err)
	}
	handlers = append(handlers, cfg.Handlers...)

	// 使用通用工厂创建智能体
	agent, err := subagents.NewGenericAgent(ctx, &subagents.GenericAgentConfig{
		Name:         agentName,
		DisplayName:  agentConfig.DisplayName,
		Description:  agentConfig.Description,
		Instruction:  instruction,
		ChatModel:    chatModel,
		Tools:        agentTools,
		MaxIteration: cfg.MaxIteration,
		Middlewares:  middlewares,
		Handlers:     handlers,
	})
	if err != nil {
		return nil, fmt.Errorf("创建智能体 %s 失败: %w", agentName, err)
	}
	return agent, nil
}
