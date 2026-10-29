package subagents

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/skill"

	"github.com/autocarto/carto_agent/internal/logger"
)

// FilteredSkillBackend keeps only allowed skill names.
type FilteredSkillBackend struct {
	inner      skill.Backend
	allowedSet map[string]bool
}

type MultiSkillBackend struct {
	backends []skill.Backend
}

func NewMultiSkillBackend(backends ...skill.Backend) *MultiSkillBackend {
	filtered := make([]skill.Backend, 0, len(backends))
	for _, backend := range backends {
		if backend != nil {
			filtered = append(filtered, backend)
		}
	}
	return &MultiSkillBackend{backends: filtered}
}

func (b *MultiSkillBackend) List(ctx context.Context) ([]skill.FrontMatter, error) {
	merged := make(map[string]skill.FrontMatter)
	order := make([]string, 0)
	for _, backend := range b.backends {
		items, err := backend.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if _, exists := merged[item.Name]; !exists {
				order = append(order, item.Name)
			}
			merged[item.Name] = item
		}
	}
	out := make([]skill.FrontMatter, 0, len(order))
	for _, name := range order {
		out = append(out, merged[name])
	}
	return out, nil
}

func (b *MultiSkillBackend) Get(ctx context.Context, name string) (skill.Skill, error) {
	for idx := len(b.backends) - 1; idx >= 0; idx-- {
		item, err := b.backends[idx].Get(ctx, name)
		if err == nil {
			return item, nil
		}
	}
	return skill.Skill{}, fmt.Errorf("skill %q not found", name)
}

func NewFilteredSkillBackend(inner skill.Backend, allowedSkills []string) *FilteredSkillBackend {
	allowedSet := make(map[string]bool, len(allowedSkills))
	for _, s := range allowedSkills {
		allowedSet[s] = true
	}
	return &FilteredSkillBackend{inner: inner, allowedSet: allowedSet}
}

func (b *FilteredSkillBackend) List(ctx context.Context) ([]skill.FrontMatter, error) {
	all, err := b.inner.List(ctx)
	if err != nil {
		return nil, err
	}
	var filtered []skill.FrontMatter
	for _, fm := range all {
		if b.allowedSet[fm.Name] {
			filtered = append(filtered, fm)
		}
	}
	return filtered, nil
}

func (b *FilteredSkillBackend) Get(ctx context.Context, name string) (skill.Skill, error) {
	if !b.allowedSet[name] {
		return skill.Skill{}, fmt.Errorf("skill %q 不在允许列表中", name)
	}
	return b.inner.Get(ctx, name)
}

func newFilesystemSkillBackend(ctx context.Context, skillDirs []string) (skill.Backend, error) {
	backends := make([]skill.Backend, 0, len(skillDirs))
	for _, rawDir := range skillDirs {
		skillDir := strings.TrimSpace(rawDir)
		if skillDir == "" {
			continue
		}
		backend, err := skill.NewBackendFromFilesystem(ctx, &skill.BackendFromFilesystemConfig{
			Backend: NewLocalFilesystemBackend(),
			BaseDir: skillDir,
		})
		if err != nil {
			return nil, fmt.Errorf("创建 LocalBackend 失败(%s): %w", skillDir, err)
		}
		backends = append(backends, backend)
	}
	if len(backends) == 0 {
		return nil, fmt.Errorf("未配置可用的 skills 目录")
	}
	return NewMultiSkillBackend(backends...), nil
}

func mergeSkillDirs(primary []string, fallback []string) []string {
	seen := make(map[string]struct{}, len(primary)+len(fallback))
	merged := make([]string, 0, len(primary)+len(fallback))
	appendDir := func(rawDir string) {
		skillDir := strings.TrimSpace(rawDir)
		if skillDir == "" {
			return
		}
		if _, exists := seen[skillDir]; exists {
			return
		}
		seen[skillDir] = struct{}{}
		merged = append(merged, skillDir)
	}
	for _, dir := range primary {
		appendDir(dir)
	}
	for _, dir := range fallback {
		appendDir(dir)
	}
	return merged
}

// LoadSkillMiddleware loads skill middleware for one runtime agent.
// primarySkillDirs are the governed package roots; fallbackSkillDirs are the full runtime skill roots.
func LoadSkillMiddleware(
	ctx context.Context, agentName string, primarySkillDirs []string, fallbackSkillDirs []string, runtimeConfig *RuntimeSkillConfig, additional ...skill.Backend,
) (*adk.AgentMiddleware, error) {
	var backends []skill.Backend
	var skills []string
	if runtimeConfig != nil {
		skills = runtimeConfig.GetAgentMiddlewareSkills(agentName)
	}
	if len(skills) > 0 {
		logger.Info("为智能体 %s 加载 %d 个 skills: %v", agentName, len(skills), skills)
		local, err := newFilesystemSkillBackend(ctx, mergeSkillDirs(primarySkillDirs, fallbackSkillDirs))
		if err != nil {
			return nil, err
		}
		backends = append(backends, NewFilteredSkillBackend(local, skills))
	}
	for _, backend := range additional {
		if backend != nil {
			backends = append(backends, backend)
		}
	}
	if len(backends) == 0 {
		return nil, nil
	}
	var backend skill.Backend = NewMultiSkillBackend(backends...)
	if len(backends) == 1 {
		backend = backends[0]
	}
	mw, err := skill.New(ctx, &skill.Config{Backend: backend})
	if err != nil {
		return nil, fmt.Errorf("创建 skill middleware 失败: %w", err)
	}
	return &mw, nil
}
