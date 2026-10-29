package deepagent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/defaults"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/runtimecontrol"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"path/filepath"
	"sort"
	"strings"
)

const DataAcquisitionExpertName = "system_data_acquisition"

func acquisitionSettings(cfg *Config) config.AcquisitionConfig {
	if cfg != nil && cfg.Acquisition != nil {
		return cfg.Acquisition().WithDefaults()
	}
	return config.AcquisitionConfig{}.WithDefaults()
}
func acquisitionToolName(name string) bool {
	return strings.HasPrefix(name, "osm_") || strings.HasPrefix(name, "web_") || strings.HasPrefix(name, "browser_") || name == "admin_boundary_extract" || name == "file_downloader" || name == "data_get" || name == "data_register"
}
func filterAcquisitionTools(ctx context.Context, cfg *Config, tools []tool.BaseTool, main bool) []tool.BaseTool {
	out := []tool.BaseTool{}
	exclusive := acquisitionSettings(cfg).Exclusive()
	for _, t := range tools {
		info, err := t.Info(ctx)
		if err != nil || info == nil {
			continue
		}
		if info.Name == "data_get" || info.Name == "data_register" {
			continue
		}
		if exclusive && acquisitionToolName(info.Name) {
			continue
		}
		if exclusive && info.Name == "gee_run_script" {
			t = wrapAcquisitionGuard(&acquisitionGuard{BaseTool: t, name: info.Name, analysisOnly: true})
		}
		out = append(out, t)
	}
	return out
}
func createSystemExperts(ctx context.Context, cfg *Config) ([]adk.Agent, error) {
	if cfg.SkipSubAgents {
		return nil, nil
	}
	selected := []tool.BaseTool{}
	hasFind, hasGet := false, false
	for _, t := range cfg.Tools {
		info, err := t.Info(ctx)
		if err != nil || info == nil {
			continue
		}
		name := info.Name
		if name == "data_find" {
			hasFind = true
		}
		if name == "data_get" {
			hasGet = true
		}
		if dataservice.Handles(name) || acquisitionFileTool(name) || name == "geodata_inspect" || name == "reference_read" {
			selected = append(selected, t)
		} else if strings.HasPrefix(name, "osm_") || strings.HasPrefix(name, "web_") || strings.HasPrefix(name, "browser_") {
			selected = append(selected, wrapAcquisitionGuard(&acquisitionGuard{BaseTool: t, name: name}))
		}
	}
	result := []adk.Agent{}
	knowledgeTools := []tool.BaseTool{}
	for _, t := range cfg.Tools {
		info, _ := t.Info(ctx)
		if info != nil && (info.Name == knowledgeContractTool || info.Name == knowledgeCompileTool) {
			knowledgeTools = append(knowledgeTools, t)
		}
	}
	if knowledge := createKnowledgeSubAgent("system_route_knowledge", `系统专家：读取史料、抽取路线知识，暂停等待用户核对，用户确认后生成路线数据集。委派的 description 必须为 JSON，包含 source_ref（单个附件编号或路径）或 source_refs（多个附件引用），以及 route_style、symbols 两个制图预设名称。按用户要求或已读取指南选择预设；例如行军路线指南使用 {"source_ref":"附件编号","route_style":"红蓝对比","symbols":"军标"}，这只是示例，不覆盖用户或其他指南的选择。将本次指南的抽取规则通过 rules 一并传入。直接粘贴的原文可用 source_ref:"current_user_message" 或 source_text；程序读取完整正文，无需复制改写，不接受读取文件等操作描述代替史料。工具返回 success:false 时先处理明确原因，不宣称抽取或制图成功；若返回 knowledge_review 的 review_id/revision，表示原文与已确认结果仍在，只用 {"review_id":"返回的编号","revision":返回的版本,"route_style":"选定预设","symbols":"选定预设"} 修正制图参数并续编译，不重新抽取、不重复要求用户确认，不重复同一失败调用。`, cfg.ChatModel, cfg.DefaultProviderCfg, knowledgeTools); knowledge != nil {
		knowledge.resolveCoordinates = coordinateResolver(cfg.ChatModel, cfg.RuntimeConfigBaseDir, cfg.DefaultProviderCfg)
		result = append(result, knowledge)
	}
	if !hasFind || !hasGet {
		return result, nil
	}
	prompt, err := defaults.FS.ReadFile("system_experts/data_acquisition.md")
	if err != nil {
		return nil, err
	}
	instruction := string(prompt)
	assets, err := config.ResolveAssetsRoot()
	if err == nil {
		home, _ := config.ResolveAutoCartoHome()
		catalog, loadErr := datasource.Load(filepath.Join(assets, "tools", "system", "data-acquisition", "assets"), filepath.Join(home, "sources"))
		if loadErr == nil {
			instruction += "\n\n来源手册索引：\n" + catalog.HandbookIndex()
		}
	}
	chat := cfg.ChatModel
	settings := acquisitionSettings(cfg)
	if settings.AgentModel != "" {
		if cfg.ResolveAgentChatModel == nil {
			return nil, fmt.Errorf("数据获取智能体模型解析器不可用")
		}
		chat, err = cfg.ResolveAgentChatModel(settings.AgentModel)
		if err != nil {
			return nil, err
		}
	}
	manuals := DataAcquisitionSkillConfig(cfg.SkillRuntimeConfig)
	userSkillsDir := cfg.UserSkillsDir
	if userSkillsDir == "" {
		userSkillsDir = cfg.SkillsDir
	}
	skillDirs := config.ResolveRuntimeSkillDirsWithPlugin(cfg.PluginSkillsDir, cfg.BuiltinSkillsDir, userSkillsDir, cfg.RuntimeConfigBaseDir, cfg.ActiveConfigName)
	for i, t := range selected {
		info, _ := t.Info(ctx)
		if info != nil && info.Name == "reference_read" {
			selected[i] = wrapAcquisitionGuard(&acquisitionGuard{BaseTool: t, name: info.Name, referenceDirs: skillDirs})
		}
	}
	primary, err := resolveMiddlewareSkillDirs(DataAcquisitionExpertName, manuals, cfg.RegistryGovernance)
	if err != nil {
		return nil, fmt.Errorf("解析系统取数手册失败: %w", err)
	}
	skillMiddleware, err := subagents.LoadSkillMiddleware(ctx, DataAcquisitionExpertName, primary, skillDirs, manuals)
	if err != nil {
		return nil, fmt.Errorf("加载系统取数手册失败: %w", err)
	}
	middlewares := []adk.AgentMiddleware{}
	if skillMiddleware != nil {
		middlewares = append(middlewares, *skillMiddleware)
	}
	selected = subagents.WithAttachmentReadTool(ctx, selected, cfg.Tools)
	agent, err := subagents.NewGenericAgent(ctx, &subagents.GenericAgentConfig{Name: DataAcquisitionExpertName, DisplayName: "数据获取智能体", Description: "系统专家：统一查找、获取、核验和登记真实数据，所有数据获取委派给此智能体。", Instruction: instruction, ChatModel: chat, Tools: selected, MaxIteration: cfg.MaxIteration, Middlewares: middlewares, Handlers: withTaskMaterials(cfg.Handlers)})
	if err != nil {
		return nil, err
	}
	return append(result, agent), nil
}

// DataAcquisitionSkillConfig projects manuals from the installed acquisition
// tool packages. It never changes the user's profile or grants execution tools;
// createSystemExperts still selects and guards the actual callable tools.
func DataAcquisitionSkillConfig(base *subagents.RuntimeSkillConfig) *subagents.RuntimeSkillConfig {
	projected := &subagents.RuntimeSkillConfig{Agents: map[string]subagents.RuntimeSkillAgentConfig{}}
	if base == nil {
		return projected
	}
	projected.Mode = base.Mode
	projected.Global.PackageTools = base.Global.PackageTools
	projected.Global.PackageMetadata = base.Global.PackageMetadata
	packages := []string{}
	for id, names := range base.Global.PackageTools {
		for _, name := range names {
			if dataservice.Handles(name) || acquisitionFileTool(name) || strings.HasPrefix(name, "osm_") || strings.HasPrefix(name, "web_") || strings.HasPrefix(name, "browser_") {
				packages = append(packages, id)
				break
			}
		}
	}
	sort.Strings(packages)
	projected.Agents[DataAcquisitionExpertName] = subagents.RuntimeSkillAgentConfig{Kind: "agent", Enabled: true, ToolPackages: packages}
	return projected
}

func acquisitionFileTool(name string) bool {
	return name == "file_read" || name == "file_write" || name == "shell_execute"
}

// The GEE tool remains available for analysis. Download templates and explicit
// local-download primitives are rejected on ordinary roles, while the trusted
// acquisition adapter retains its separate template channel.
type acquisitionGuard struct {
	tool.BaseTool
	name          string
	referenceDirs []string
	analysisOnly  bool
}

func (g *acquisitionGuard) authorize(ctx context.Context, arguments string) (context.Context, error) {
	if g.name == "reference_read" {
		ctx = runtimecontrol.WithReferencePackageDirs(ctx, g.referenceDirs)
	} else if g.analysisOnly {
		var args map[string]any
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return ctx, err
		}
		if args["template"] != nil {
			return ctx, fmt.Errorf("GEE 数据下载模板请委派数据获取智能体")
		}
		code, _ := args["code"].(string)
		for _, pattern := range []string{"getDownloadURL", "getDownloadUrl", "download_ee_image", "ee_export_image", "ee_to_numpy", "download_file", "urlretrieve"} {
			if strings.Contains(code, pattern) {
				return ctx, fmt.Errorf("GEE 数据下载请通过数据服务；当前角色保留分析能力")
			}
		}
	} else {
		service := dataservice.FromContext(ctx)
		if service == nil {
			return ctx, fmt.Errorf("兜底取数需要当前会话的数据服务")
		}
		if err := service.AuthorizeFallback(ctx, g.name); err != nil {
			return ctx, err
		}
	}
	return ctx, nil
}

func (g *acquisitionGuard) InvokableRun(ctx context.Context, arguments string, options ...tool.Option) (string, error) {
	ctx, err := g.authorize(ctx, arguments)
	if err != nil {
		return "", err
	}
	if simple, ok := g.BaseTool.(tool.InvokableTool); ok {
		return simple.InvokableRun(ctx, arguments, options...)
	}
	return "", fmt.Errorf("数据工具不可执行")
}

// Preserve the underlying tool's modality instead of turning its result into
// text. Eino dispatches EnhancedInvokableTool results as real media messages.
func wrapAcquisitionGuard(guard *acquisitionGuard) tool.BaseTool {
	if enhanced, ok := guard.BaseTool.(tool.EnhancedInvokableTool); ok {
		return &enhancedAcquisitionGuard{acquisitionGuard: guard, inner: enhanced}
	}
	return guard
}

type enhancedAcquisitionGuard struct {
	*acquisitionGuard
	inner tool.EnhancedInvokableTool
}

func (g *enhancedAcquisitionGuard) InvokableRun(ctx context.Context, arguments *schema.ToolArgument, options ...tool.Option) (*schema.ToolResult, error) {
	text := ""
	if arguments != nil {
		text = arguments.Text
	}
	ctx, err := g.authorize(ctx, text)
	if err != nil {
		return nil, err
	}
	return g.inner.InvokableRun(ctx, arguments, options...)
}
