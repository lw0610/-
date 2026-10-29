package deepagent

import (
	"context"
	"fmt"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/cloudwego/eino/adk/middlewares/skill"
	"strings"
)

// GuideBackend shares local flow instructions with the main agent and its delegates.
type GuideBackend struct {
	Store     *flow.GuideStore
	Resources *resource.Library
}

func GuideSkillName(id string) string { return "guide-" + id }
func (b *GuideBackend) List(ctx context.Context) ([]skill.FrontMatter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	items, err := b.Store.List()
	if err != nil {
		return nil, err
	}
	out := []skill.FrontMatter{}
	for _, item := range items {
		if item.Enabled {
			out = append(out, skill.FrontMatter{Name: GuideSkillName(item.ID), Description: item.Name + "：" + item.Description})
		}
	}
	return out, nil
}
func (b *GuideBackend) Get(ctx context.Context, name string) (skill.Skill, error) {
	if !strings.HasPrefix(name, "guide-") {
		return skill.Skill{}, fmt.Errorf("guide %q not found", name)
	}
	id := strings.TrimPrefix(name, "guide-")
	content, err := b.ReadContext(ctx, id)
	if err != nil {
		return skill.Skill{}, err
	}
	g, err := b.Store.Get(id)
	if err != nil {
		return skill.Skill{}, err
	}
	return skill.Skill{FrontMatter: skill.FrontMatter{Name: name, Description: g.Meta.Name + "：" + g.Meta.Description}, Content: content, BaseDirectory: g.Dir}, nil
}
func (b *GuideBackend) ReadContext(ctx context.Context, id string) (string, error) {
	return b.Store.ReadContext(ctx, id, b.Resources)
}
