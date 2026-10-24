package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"gopkg.in/yaml.v3"
)

type MigrationWarning struct {
	Code    string `json:"code" yaml:"code"`
	Path    string `json:"path" yaml:"path"`
	Package string `json:"package,omitempty" yaml:"package,omitempty"`
	Message string `json:"message" yaml:"message"`
}

type AgentConfigMigrationResult struct {
	Config   *AgentConfigData   `json:"config"`
	Warnings []MigrationWarning `json:"warnings"`
	Changed  bool               `json:"changed"`
	DryRun   bool               `json:"dry_run"`
}

func normalizeRoleKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		return "agent"
	}
	return kind
}

func cloneFlowConfig(flows *FlowConfig) *FlowConfig {
	if flows == nil {
		return nil
	}
	return &FlowConfig{Enabled: normalizeStringSlice(flows.Enabled)}
}

// migrateAgentConfigV1 returns an independent candidate and never writes files.
// Explicit v2 fields win; legacy input is consumed once and then cleared.
func migrateAgentConfigV1(source *AgentConfigFile, installed map[string]bool) (*AgentConfigFile, []MigrationWarning) {
	cloned, err := cloneConfigFile(source)
	if err != nil {
		return source, []MigrationWarning{{Code: "MIGRATION_CLONE_FAILED", Message: err.Error()}}
	}
	cloned.MigrationWarnings = append([]MigrationWarning(nil), source.MigrationWarnings...)
	warnings := []MigrationWarning{}
	flowIDs := []string{}
	mapPackages := func(ids []string, path string) (tools, unresolved []string) {
		for _, id := range normalizeStringSlice(ids) {
			target, ok := legacyPackageMap[id]
			if !ok && installed[id] {
				target = legacyPackageTarget{Tools: []string{id}}
				ok = true
			}
			if !ok {
				unresolved = append(unresolved, id)
				warnings = append(warnings, MigrationWarning{Code: "LEGACY_PACKAGE_UNRESOLVED", Path: path, Package: id, Message: "无法识别旧工具包，请选择替代工具包。"})
				continue
			}
			tools = append(tools, target.Tools...)
			if target.Flow != "" {
				flowIDs = append(flowIDs, target.Flow)
			}
		}
		return normalizeStringSlice(tools), normalizeStringSlice(unresolved)
	}
	if cloned.Runtime != nil {
		r := cloned.Runtime
		shared, unresolved := mapPackages(append(append([]string(nil), r.Global.SkillPackages...), r.Global.ToolPackages...), "runtime.global.skill_packages")
		r.SharedToolPackages = normalizeStringSlice(append(r.SharedToolPackages, shared...))
		r.LegacyUnresolvedPackages = normalizeStringSlice(append(r.LegacyUnresolvedPackages, append(r.Global.LegacyUnresolvedPackages, unresolved...)...))
		r.Global = GlobalAgentConfig{}
		names := sortedRuntimeAgentNames(r.Agents)
		for _, name := range names {
			a := r.Agents[name]
			legacy := len(a.SkillPackages) > 0 || (source.Version != AgentConfigVersionV20 && a.Kind == "")
			tools, unresolved := mapPackages(a.SkillPackages, "runtime.agents."+name+".skill_packages")
			if a.ToolPackages == nil {
				a.ToolPackages = tools
			} else {
				a.ToolPackages = normalizeStringSlice(a.ToolPackages)
			}
			a.LegacyUnresolvedPackages = normalizeStringSlice(append(a.LegacyUnresolvedPackages, unresolved...))
			// This is the sole legacy role-name inference. Runtime dispatch uses kind/executor.
			if legacy && (name == "knowledge_engineer" || containsNormalized(a.SkillPackages, "marching-route-knowledge-tools")) {
				a.Kind, a.Executor, a.FlowOnly, a.ToolPackages = "executor", "route_knowledge", true, []string{}
			} else {
				a.Kind = normalizeRoleKind(a.Kind)
			}
			if strings.TrimSpace(a.legacyWorkflowPackage) != "" {
				warnings = append(warnings, MigrationWarning{Code: "LEGACY_WORKFLOW_RETIRED", Path: "runtime.agents." + name + ".workflow_package", Package: a.legacyWorkflowPackage, Message: "旧串行 workflow 试点已下线；请在流程中心配置流程。"})
			}
			a.SkillPackages, a.legacyWorkflowPackage = nil, ""
			r.Agents[name] = a
		}
	}
	if cloned.Plan != nil {
		p := cloned.Plan
		tools, unresolved := mapPackages(p.Global.SkillPackages, "plan.global.skill_packages")
		if p.Global.ToolPackages == nil {
			p.Global.ToolPackages = tools
		}
		p.Global.LegacyUnresolvedPackages = normalizeStringSlice(append(p.Global.LegacyUnresolvedPackages, unresolved...))
		p.Global.SkillPackages = nil
		names := make([]string, 0, len(p.Agents))
		for name := range p.Agents {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			a := p.Agents[name]
			tools, unresolved := mapPackages(a.SkillPackages, "plan.agents."+name+".skill_packages")
			if a.ToolPackages == nil {
				a.ToolPackages = tools
			}
			a.Kind, a.SkillPackages = normalizeRoleKind(a.Kind), nil
			a.LegacyUnresolvedPackages = normalizeStringSlice(append(a.LegacyUnresolvedPackages, unresolved...))
			p.Agents[name] = a
		}
	}
	if cloned.Flows == nil && len(flowIDs) > 0 {
		cloned.Flows = &FlowConfig{Enabled: normalizeStringSlice(flowIDs)}
	}
	cloned.Version = AgentConfigVersionV20
	appendUnresolved := func(path string, ids []string) {
		for _, id := range ids {
			warnings = append(warnings, MigrationWarning{Code: "LEGACY_PACKAGE_UNRESOLVED", Path: path, Package: id, Message: "无法识别旧工具包，请选择替代工具包。"})
		}
	}
	if cloned.Runtime != nil {
		appendUnresolved("runtime.shared_tool_packages", cloned.Runtime.LegacyUnresolvedPackages)
		for _, name := range sortedRuntimeAgentNames(cloned.Runtime.Agents) {
			appendUnresolved("runtime.agents."+name+".skill_packages", cloned.Runtime.Agents[name].LegacyUnresolvedPackages)
		}
	}
	if cloned.Plan != nil {
		appendUnresolved("plan.global.skill_packages", cloned.Plan.Global.LegacyUnresolvedPackages)
		names := make([]string, 0, len(cloned.Plan.Agents))
		for name := range cloned.Plan.Agents {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			appendUnresolved("plan.agents."+name+".skill_packages", cloned.Plan.Agents[name].LegacyUnresolvedPackages)
		}
	}
	return cloned, uniqueMigrationWarnings(warnings)
}

func sortedRuntimeAgentNames(agents map[string]AgentDefData) []string {
	names := make([]string, 0, len(agents))
	for name := range agents {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (acm *AgentConfigManager) knownToolPackageIDs(configName ...string) map[string]bool {
	name := acm.activeConfig
	if len(configName) > 0 {
		name = configName[0]
	}
	catalog, err := subagents.LoadPackageCatalogMultiDir(ResolveSystemSkillsDir(), ResolveBuiltinSkillsDir(), ResolveUserSkillsDir(), ResolveGlobalSkillsDir(acm.ConfigBaseDir()), acm.ExclusiveSkillsDir(name))
	known := map[string]bool{}
	if err == nil {
		for id := range catalog.Tools {
			known[id] = true
		}
	}
	return known
}

// MigrateConfig previews the effective v2 configuration. A caller that wants to
// persist it must use the normal settings mutation service, including run guards.
func (acm *AgentConfigManager) PreviewMigration(name string) (*AgentConfigMigrationResult, error) {
	data, err := acm.GetConfigData(name)
	if err != nil {
		return nil, err
	}
	acm.mu.RLock()
	path := acm.configFilePath(name)
	if _, err := os.Stat(path); os.IsNotExist(err) && acm.legacyPaths[name] != "" {
		path = acm.legacyPaths[name]
	}
	acm.mu.RUnlock()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var header struct {
		Version string `yaml:"version"`
	}
	if err := yaml.Unmarshal(raw, &header); err != nil {
		return nil, err
	}
	return &AgentConfigMigrationResult{Config: data, Warnings: append([]MigrationWarning{}, data.MigrationWarnings...), Changed: header.Version != AgentConfigVersionV20, DryRun: true}, nil
}

func backupV1ConfigOnce(path string) error {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return backupV1ConfigBytes(path, raw)
}

func backupV1ConfigBytes(path string, raw []byte) error {
	var header struct {
		Version string `yaml:"version"`
	}
	if err := yaml.Unmarshal(raw, &header); err != nil {
		return fmt.Errorf("检查旧方案版本失败: %w", err)
	}
	if header.Version == AgentConfigVersionV20 {
		return nil
	}
	backup := path + ".v1.bak"
	f, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		info, statErr := os.Lstat(backup)
		if statErr != nil {
			return statErr
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("旧方案备份必须是普通文件: %s", backup)
		}
		return nil
	}
	if err != nil {
		return err
	}
	_, writeErr := f.Write(raw)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil {
		_ = os.Remove(backup)
		return writeErr
	}
	if syncErr != nil {
		_ = os.Remove(backup)
		return syncErr
	}
	return closeErr
}

func atomicWriteAgentConfig(path string, raw []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".agent-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func uniqueMigrationWarnings(items []MigrationWarning) []MigrationWarning {
	result := []MigrationWarning{}
	seen := map[string]bool{}
	for _, item := range items {
		key := item.Code + "|" + item.Path + "|" + item.Package
		if !seen[key] {
			result = append(result, item)
			seen[key] = true
		}
	}
	return result
}

func validateConfigInputVersion(version string) error {
	switch strings.TrimSpace(version) {
	case "", AgentConfigVersionV10, AgentConfigVersionV20:
		return nil
	default:
		return fmt.Errorf("不支持的智能体方案版本: %s", version)
	}
}
