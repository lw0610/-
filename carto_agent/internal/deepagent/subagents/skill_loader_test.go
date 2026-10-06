package subagents

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	einoskill "github.com/cloudwego/eino/adk/middlewares/skill"
)

type fakeSkillBackend struct {
	items map[string]einoskill.Skill
}

func (b *fakeSkillBackend) List(_ context.Context) ([]einoskill.FrontMatter, error) {
	result := make([]einoskill.FrontMatter, 0, len(b.items))
	for _, item := range b.items {
		result = append(result, item.FrontMatter)
	}
	return result, nil
}

func (b *fakeSkillBackend) Get(_ context.Context, name string) (einoskill.Skill, error) {
	item, ok := b.items[name]
	if !ok {
		return einoskill.Skill{}, fmt.Errorf("skill %s not found", name)
	}
	return item, nil
}

func TestMultiSkillBackendListMergesByLaterBackend(t *testing.T) {
	backend := NewMultiSkillBackend(
		&fakeSkillBackend{items: map[string]einoskill.Skill{
			"shared": {FrontMatter: einoskill.FrontMatter{Name: "shared", Description: "global"}, Content: "global"},
			"alpha":  {FrontMatter: einoskill.FrontMatter{Name: "alpha", Description: "alpha"}, Content: "alpha"},
		}},
		&fakeSkillBackend{items: map[string]einoskill.Skill{
			"shared": {FrontMatter: einoskill.FrontMatter{Name: "shared", Description: "exclusive"}, Content: "exclusive"},
			"beta":   {FrontMatter: einoskill.FrontMatter{Name: "beta", Description: "beta"}, Content: "beta"},
		}},
	)

	items, err := backend.List(context.Background())
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 merged skills, got %#v", items)
	}
	for _, item := range items {
		if item.Name == "shared" && item.Description != "exclusive" {
			t.Fatalf("expected later backend to override shared description, got %#v", item)
		}
	}
}

func TestMultiSkillBackendGetUsesLaterBackendOverride(t *testing.T) {
	backend := NewMultiSkillBackend(
		&fakeSkillBackend{items: map[string]einoskill.Skill{
			"shared": {FrontMatter: einoskill.FrontMatter{Name: "shared"}, Content: "global"},
		}},
		&fakeSkillBackend{items: map[string]einoskill.Skill{
			"shared": {FrontMatter: einoskill.FrontMatter{Name: "shared"}, Content: "third-party"},
		}},
	)

	item, err := backend.Get(context.Background(), "shared")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if item.Content != "third-party" {
		t.Fatalf("expected later backend content, got %#v", item)
	}
}

func TestNewFilesystemSkillBackendLoadsSkillsFromMultipleDirs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	baseDir := t.TempDir()
	overrideDir := t.TempDir()
	writeSkillFixture(t, baseDir, "base-only", "Base only", "base only content")
	writeSkillFixture(t, baseDir, "shared", "Base shared", "base shared content")
	writeSkillFixture(t, overrideDir, "override-only", "Override only", "override only content")
	writeSkillFixture(t, overrideDir, "shared", "Override shared", "override shared content")

	backend, err := newFilesystemSkillBackend(ctx, []string{baseDir, overrideDir})
	if err != nil {
		t.Fatalf("newFilesystemSkillBackend returned error: %v", err)
	}

	items, err := backend.List(ctx)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 skills from merged dirs, got %#v", items)
	}
	byName := map[string]einoskill.FrontMatter{}
	for _, item := range items {
		byName[item.Name] = item
	}
	if byName["shared"].Description != "Override shared" {
		t.Fatalf("expected later dir to override shared description, got %#v", byName["shared"])
	}
	shared, err := backend.Get(ctx, "shared")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if shared.Content != "override shared content" {
		t.Fatalf("expected later dir to override shared content, got %#v", shared)
	}
}

func TestNewFilesystemSkillBackendFilteredAllowlistUsesFinalOverride(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	baseDir := t.TempDir()
	overrideDir := t.TempDir()
	writeSkillFixture(t, baseDir, "shared", "Base shared", "base shared content")
	writeSkillFixture(t, baseDir, "ignored", "Ignored", "ignored content")
	writeSkillFixture(t, overrideDir, "shared", "Override shared", "override shared content")

	backend, err := newFilesystemSkillBackend(ctx, []string{baseDir, overrideDir})
	if err != nil {
		t.Fatalf("newFilesystemSkillBackend returned error: %v", err)
	}
	filtered := NewFilteredSkillBackend(backend, []string{"shared"})

	items, err := filtered.List(ctx)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(items) != 1 || items[0].Name != "shared" {
		t.Fatalf("expected filtered shared skill only, got %#v", items)
	}
	if items[0].Description != "Override shared" {
		t.Fatalf("expected filtered list to keep override metadata, got %#v", items[0])
	}
	shared, err := filtered.Get(ctx, "shared")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if shared.Content != "override shared content" {
		t.Fatalf("expected filtered backend to return override content, got %#v", shared)
	}
	if _, err := filtered.Get(ctx, "ignored"); err == nil {
		t.Fatal("expected ignored skill to be blocked by allowlist")
	}
}

func writeSkillFixture(t *testing.T, rootDir, name, description, content string) {
	t.Helper()
	skillDir := filepath.Join(rootDir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir failed: %v", err)
	}
	body := fmt.Sprintf("---\nname: %s\ndescription: %s\nkind: knowledge\n---\n\n%s\n", name, description, content)
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write SKILL.md failed: %v", err)
	}
}
