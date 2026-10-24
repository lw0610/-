package governance

import (
	"github.com/autocarto/carto_agent/internal/skill"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
)

type GovernedRegistry interface {
	LoadToolRegistry() (*toolpkg.Registry, error)
	LoadSkillRegistry() (*skill.Registry, error)
	ResolveSkillPackage(packageName string) (skill.SkillPackage, bool, error)
}
