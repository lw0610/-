package deepagent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/internal/governance"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/deep"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/logger"
)

// Config DeepAgent 配置
type Config struct {
	Name         string
	Description  string
	ChatModel    model.ToolCallingChatModel
	Tools        []tool.BaseTool
	MaxIteration int
	Instruction  string

	SkillRuntimeConfig *subagents.RuntimeSkillConfig

	// TemplateEngine 提示词模板引擎（可选，用于动态生成提示词）
	TemplateEngine *subagents.TemplateEngine

	BuiltinSkillsDir string
	UserSkillsDir    string
	SkillsDir        string

	PluginSkillsDir string

	RegistryGovernance governance.GovernedRegistry

	// SkipSubAgents 为 true 时不加载子智能体。
	SkipSubAgents bool

	// AgentLLMs 子智能体独立 LLM 配置。
	AgentLLMs map[string]config.AgentLLMConfig

	// ResolveAgentChatModel 按 provider 解析子智能体模型。
	ResolveAgentChatModel func(provider string) (model.ToolCallingChatModel, error)

	// DefaultProviderCfg 为当前默认模型提供运行时压缩配置。
	DefaultProviderCfg *config.LLMProviderConfig

	// ActiveConfigName 当前运行时启用的智能体配置名称。
	ActiveConfigName string

	// RuntimeConfigBaseDir 运行时基础目录（通常为 autocarto/config）。
	RuntimeConfigBaseDir string

	// ResolveProviderCfg 按 provider 名称解析运行时压缩配置。
	ResolveProviderCfg func(provider string) *config.LLMProviderConfig

	// Handlers ChatModel 层中间件。
	Handlers []adk.ChatModelAgentMiddleware
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Name:         "orchestrator",
		Description:  "AutoCarto AI 智能GIS助手",
		MaxIteration: 50,
	}
}

// New 创建 DeepAgent 主智能体
func New(ctx context.Context, cfg *Config) (adk.ResumableAgent, error) {
	// Creator profiles use their own main-agent key. The existing main prompt
	// and skill machinery uses "orchestrator", so normalize only an in-memory
	// copy, consistently before prompts, middleware and tool filtering.
	if cfg.SkillRuntimeConfig != nil {
		if _, exists := cfg.SkillRuntimeConfig.Agents["orchestrator"]; !exists {
			mainName := ""
			for name, agent := range cfg.SkillRuntimeConfig.Agents {
				if !agent.IsMain {
					continue
				}
				if mainName != "" {
					return nil, fmt.Errorf("multiple main agents configured")
				}
				mainName = name
			}
			if mainName != "" {
				copied := *cfg
				runtimeConfig := *cfg.SkillRuntimeConfig
				runtimeConfig.Agents = make(map[string]subagents.RuntimeSkillAgentConfig, len(cfg.SkillRuntimeConfig.Agents))
				for name, agent := range cfg.SkillRuntimeConfig.Agents {
					if name == mainName {
						name = "orchestrator"
					}
					runtimeConfig.Agents[name] = agent
				}
				copied.SkillRuntimeConfig = &runtimeConfig
				copied.TemplateEngine = subagents.NewTemplateEngine(&runtimeConfig)
				cfg = &copied
			}
		}
	}
	// 初始化模板引擎
	templateEngine := cfg.TemplateEngine
	if templateEngine == nil && cfg.SkillRuntimeConfig != nil {
		templateEngine = subagents.NewTemplateEngine(cfg.SkillRuntimeConfig)
	}

	userSkillsDir := strings.TrimSpace(cfg.UserSkillsDir)
	if userSkillsDir == "" {
		userSkillsDir = strings.TrimSpace(cfg.SkillsDir)
	}
	skillDirs := config.ResolveRuntimeSkillDirsWithPlugin(cfg.PluginSkillsDir, cfg.BuiltinSkillsDir, userSkillsDir, cfg.RuntimeConfigBaseDir, cfg.ActiveConfigName)

	// 构建 Orchestrator 提示词
	instruction, err := buildOrchestratorPrompt(ctx, cfg, templateEngine)
	if err != nil {
		return nil, err
	}

	// 创建子智能体
	subAgentList := make([]adk.Agent, 0)
	if !cfg.SkipSubAgents {
		var err error
		subAgentList, err = createSubAgents(ctx, cfg, templateEngine, skillDirs)
		if err != nil {
			return nil, err
		}
	}

	// 配置工具
	toolsConfig := adk.ToolsConfig{}
	toolsConfig.Tools = cfg.Tools
	if cfg.SkillRuntimeConfig != nil {
		// Keep the full catalog above for specialist construction and capability
		// descriptions, but honor the main agent's own package assignments just
		// as createSubAgents does. Otherwise unassigned execution tools bypass
		// delegation and the specialist workflow's skill allowlist entirely.
		toolsConfig.Tools = subagents.CreateRuntimeSkillToolFilter("orchestrator", cfg.SkillRuntimeConfig).Filter(ctx, cfg.Tools)
	}
	toolsConfig.UnknownToolsHandler = subagents.NewUnknownToolsHandler(ctx, toolsConfig.Tools)
	// 启用子智能体内部事件发送，使得 task 工具调用子智能体时能获取中间过程
	toolsConfig.EmitInternalEvents = true

	// 防御性处理：确保 Description 非空（eino adk 要求必填）
	description := cfg.Description
	if description == "" {
		if cfg.Name != "" {
			description = cfg.Name
		} else {
			description = "AutoCarto AI Agent"
		}
	}

	// 加载 orchestrator 的 skill middleware
	var orchestratorMiddlewares []adk.AgentMiddleware
	if cfg.SkillRuntimeConfig != nil {
		middlewareSkillDirs, err := resolveMiddlewareSkillDirs("orchestrator", cfg.SkillRuntimeConfig, cfg.RegistryGovernance)
		if err != nil {
			return nil, fmt.Errorf("resolve orchestrator middleware packages: %w", err)
		}
		if len(middlewareSkillDirs) > 0 {
			skillMw, err := subagents.LoadSkillMiddleware(ctx, "orchestrator", middlewareSkillDirs, skillDirs, cfg.SkillRuntimeConfig)
			if err != nil {
				return nil, fmt.Errorf("load orchestrator skill middleware: %w", err)
			}
			if skillMw != nil {
				orchestratorMiddlewares = append(orchestratorMiddlewares, *skillMw)
			}
		}
	}

	orchestratorHandlers, err := buildRuntimeHandlers(ctx, cfg.Name, cfg.ChatModel, cloneProviderConfig(cfg.DefaultProviderCfg), runtimeHandlerOptions{
		ActiveConfigName: cfg.ActiveConfigName,
		ConfigBaseDir:    cfg.RuntimeConfigBaseDir,
	})
	if err != nil {
		return nil, err
	}
	orchestratorHandlers = append(orchestratorHandlers, cfg.Handlers...)

	// 创建 DeepAgent
	return deep.New(ctx, &deep.Config{
		Name:                   cfg.Name,
		Description:            description,
		ChatModel:              cfg.ChatModel,
		Instruction:            instruction,
		SubAgents:              subAgentList,
		ToolsConfig:            toolsConfig,
		MaxIteration:           cfg.MaxIteration,
		WithoutGeneralSubAgent: true,
		Middlewares:            orchestratorMiddlewares,
		Handlers:               orchestratorHandlers,
	})
}

// buildOrchestratorPrompt 构建 Orchestrator 提示词
func buildOrchestratorPrompt(
	ctx context.Context,
	cfg *Config,
	engine *subagents.TemplateEngine,
) (string, error) {
	// 如果有模板引擎，尝试使用动态生成
	if engine != nil && cfg.SkillRuntimeConfig != nil {
		agentConfig, ok := cfg.SkillRuntimeConfig.Agents["orchestrator"]
		if ok {
			// 优先使用静态指令模板（动态生成）
			if agentConfig.Prompts.StaticInstructions != "" {
				prompt, err := engine.BuildOrchestratorPrompt(ctx, cfg.Tools)
				if err != nil {
					return "", fmt.Errorf("构建 Orchestrator 动态提示词失败: %w", err)
				}
				return prompt, nil
			}
		}
	}

	// 最后使用传入的指令
	if cfg.Instruction != "" {
		return cfg.Instruction, nil
	}

	return "", fmt.Errorf("主智能体提示词未配置，请在智能体配置中配置 orchestrator 的 static_instructions")
}

// createSubAgents 创建所有子智能体（完全由配置文件驱动）
func createSubAgents(
	ctx context.Context,
	cfg *Config,
	engine *subagents.TemplateEngine,
	skillDirs []string,
) ([]adk.Agent, error) {
	var agents []adk.Agent

	// 如果没有配置文件，返回空列表
	if cfg.SkillRuntimeConfig == nil {
		return agents, nil
	}

	// 遍历配置文件中的所有智能体（跳过主智能体）
	for agentName, agentConfig := range cfg.SkillRuntimeConfig.Agents {
		// 兼容历史配置：主智能体名称默认 orchestrator
		if agentConfig.IsMain || agentName == "orchestrator" {
			continue
		}

		// 检查智能体是否启用
		if !agentConfig.Enabled {
			continue
		}

		if strings.TrimSpace(agentConfig.WorkflowPackage) != "" {
			workflowAgent, err := createWorkflowSubAgent(ctx, cfg, agentName, agentConfig, skillDirs)
			if err != nil {
				return nil, fmt.Errorf("创建 workflow 智能体 %s 失败: %w", agentName, err)
			}
			agents = append(agents, workflowAgent)
			continue
		}

		// 创建工具过滤器
		filter := subagents.CreateRuntimeSkillToolFilter(agentName, cfg.SkillRuntimeConfig)

		// 过滤工具
		agentTools := filter.Filter(ctx, cfg.Tools)

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
		if agentName == "knowledge_engineer" {
			instructions := strings.TrimSpace(agentConfig.Prompts.StaticInstructions)
			if instructions == "" {
				return nil, fmt.Errorf("知识抽取业务规则为空，请配置 knowledge_engineer 的 static_instructions")
			}
			knowledgeAgent := createKnowledgeSubAgent(agentName, agentConfig.Description, chatModel, providerCfg, agentTools)
			if knowledgeAgent == nil {
				return nil, fmt.Errorf("知识抽取需要模型和 marching-route-knowledge-tools 中的契约、编译工具，请检查配置")
			}
			knowledgeAgent.instructions = instructions
			knowledgeAgent.resolveCoordinates = coordinateResolver(chatModel, cfg.RuntimeConfigBaseDir, providerCfg)
			agents = append(agents, knowledgeAgent)
			continue
		}

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

		agents = append(agents, agent)
	}

	return agents, nil
}

func createWorkflowSubAgent(ctx context.Context, cfg *Config, agentName string, agentConfig subagents.RuntimeSkillAgentConfig, _ []string) (adk.Agent, error) {
	packageName := strings.TrimSpace(agentConfig.WorkflowPackage)
	if cfg != nil && cfg.RegistryGovernance != nil {
		pkg, ok, err := cfg.RegistryGovernance.ResolveWorkflowPackage(packageName)
		if err != nil {
			return nil, fmt.Errorf("resolve workflow package %q: %w", packageName, err)
		}
		if !ok {
			return nil, fmt.Errorf("workflow package %q not found", packageName)
		}
		return createWorkflowSubAgentFromPackage(ctx, cfg, agentName, agentConfig, pkg)
	}
	return nil, fmt.Errorf("registry governance is required for workflow package %s", packageName)
}

func resolveMiddlewareSkillDirs(agentName string, runtimeConfig *subagents.RuntimeSkillConfig, resolver governance.GovernedRegistry) ([]string, error) {
	if runtimeConfig == nil {
		return nil, nil
	}
	packageNames := runtimeConfig.GetAgentMiddlewareSkills(agentName)
	if len(packageNames) == 0 {
		return nil, nil
	}
	if resolver == nil {
		return nil, fmt.Errorf("registry governance is required for middleware package %s", packageNames[0])
	}
	seen := make(map[string]struct{}, len(packageNames))
	dirs := make([]string, 0, len(packageNames))
	for _, packageName := range packageNames {
		trimmedName := strings.TrimSpace(packageName)
		if trimmedName == "" {
			continue
		}
		pkg, ok, err := resolver.ResolveSkillPackage(trimmedName)
		if err != nil {
			return nil, fmt.Errorf("resolve middleware package %q: %w", trimmedName, err)
		}
		if !ok {
			return nil, fmt.Errorf("middleware package %q not found in governed registry", trimmedName)
		}
		packageDir := strings.TrimSpace(pkg.Dir)
		packageRoot := strings.TrimSpace(pkg.Source)
		if packageRoot == "" {
			packageRoot = packageDir
		}
		if packageRoot == "" {
			return nil, fmt.Errorf("middleware package %q missing governed package dir", trimmedName)
		}

		cleanedDir := filepath.Clean(packageRoot)
		// skill middleware backend expects BaseDir to be a skills root that
		// contains package directories, not the package directory itself.
		if strings.EqualFold(filepath.Base(cleanedDir), "SKILL.md") {
			cleanedDir = filepath.Dir(cleanedDir)
		}
		if strings.EqualFold(filepath.Base(cleanedDir), trimmedName) {
			cleanedDir = filepath.Dir(cleanedDir)
		}
		if _, exists := seen[cleanedDir]; exists {
			continue
		}
		seen[cleanedDir] = struct{}{}
		dirs = append(dirs, cleanedDir)
	}
	return dirs, nil
}

// buildSubAgentPrompt 构建子智能体提示词
func buildSubAgentPrompt(
	ctx context.Context,
	agentName string,
	agentConfig subagents.RuntimeSkillAgentConfig,
	tools []tool.BaseTool,
	engine *subagents.TemplateEngine,
) (string, error) {
	// 优先使用模板引擎动态生成
	if engine != nil && agentConfig.Prompts.StaticInstructions != "" {
		prompt, err := engine.BuildSubAgentPrompt(ctx, agentName, tools)
		if err != nil {
			return "", err
		}
		return prompt, nil
	}

	// 如果都没有，生成一个基础提示词
	return fmt.Sprintf("你是%s，专注于%s。",
		agentConfig.DisplayName, agentConfig.Description), nil
}

func cloneProviderConfig(cfg *config.LLMProviderConfig) *config.LLMProviderConfig {
	if cfg == nil {
		return nil
	}
	cp := *cfg
	return &cp
}
