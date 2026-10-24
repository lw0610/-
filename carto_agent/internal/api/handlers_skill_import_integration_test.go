package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/settings"
)

// Exercise the real HTTP handler, mutation service, and filesystem importer
// together, with every source and asset directory isolated under t.TempDir.
func TestSkillImportHTTPPreservesReferencesAndHonorsOverwrite(t *testing.T) {
	globalDir, userDir, toolsDir := t.TempDir(), t.TempDir(), t.TempDir()
	sourceDir := filepath.Join(t.TempDir(), "source-package")
	const packageName = "reference-import-demo"
	writePackageVersion := func(version string) {
		t.Helper()
		writeSkillPackageTestFile(t, filepath.Join(sourceDir, "SKILL.md"), "---\nname: "+packageName+"\ndescription: imported workflow\nkind: category\ntools: [fixture_tool]\n---\n\n# "+version+"\n\nRead [guide](references/guide.md).\n")
		writeSkillPackageTestFile(t, filepath.Join(sourceDir, "references", "guide.md"), "guide "+version)
	}
	writePackageVersion("version one")

	governance := control.NewRegistryGovernance(control.RegistryGovernanceDeps{
		ResolveToolDirs:  func() []string { return []string{toolsDir} },
		ResolveSkillDirs: func() []string { return []string{globalDir, userDir} },
	})
	skillService := settings.NewFileSkillServiceWithConfig(settings.FileSkillServiceConfig{
		GlobalSkillsDir:                  globalDir,
		UserSkillsDir:                    userDir,
		UserToolsDir:                     toolsDir,
		RuntimeSkillDirs:                 []string{globalDir, userDir},
		RuntimeToolDirs:                  []string{toolsDir},
		ResolveRuntimeRegistryGovernance: func() settings.RuntimeRegistryGovernance { return governance },
	}, nil)
	mutations := control.NewSettingsMutationService(control.SettingsMutationServiceDeps{SkillCatalog: skillService})
	handler := NewAPIWithServices(nil, nil, nil, nil, nil, mutations, nil, control.NewNoopRealtimeHub())
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	type entry struct {
		Name   string `json:"name"`
		Source string `json:"source"`
		Target string `json:"target"`
		Reason string `json:"reason"`
	}
	type result struct {
		Action   string                          `json:"action"`
		Mode     string                          `json:"mode"`
		Imported []entry                         `json:"imported"`
		Skipped  []entry                         `json:"skipped"`
		Mutation settings.SettingsMutationResult `json:"mutation"`
	}
	postImport := func(overwrite bool) result {
		t.Helper()
		body, err := json.Marshal(map[string]any{"source_path": sourceDir, "overwrite": overwrite})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/skills/import?scope=global", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("POST import overwrite=%t status=%d: %s", overwrite, rr.Code, rr.Body.String())
		}
		var response struct {
			Success bool   `json:"success"`
			Data    result `json:"data"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if !response.Success || response.Data.Action != "import" || response.Data.Mode != "directory_single" {
			t.Fatalf("unexpected import response: %s", rr.Body.String())
		}
		if response.Data.Imported == nil || response.Data.Skipped == nil {
			t.Fatalf("imported/skipped must be JSON arrays: %s", rr.Body.String())
		}
		if response.Data.Mutation.Scope != "skill_import" {
			t.Fatalf("missing mutation envelope: %s", rr.Body.String())
		}
		return response.Data
	}
	targetDir := filepath.Join(globalDir, packageName)
	assertInstalledVersion := func(version string) {
		t.Helper()
		markdown, err := os.ReadFile(filepath.Join(targetDir, "SKILL.md"))
		if err != nil || !strings.Contains(string(markdown), "# "+version) {
			t.Fatalf("installed markdown = %q, %v; want %q", markdown, err, version)
		}
		reference, err := os.ReadFile(filepath.Join(targetDir, "references", "guide.md"))
		if err != nil || string(reference) != "guide "+version {
			t.Fatalf("installed reference = %q, %v; want %q", reference, err, version)
		}
	}
	assertWritten := func(imported result) {
		t.Helper()
		if len(imported.Imported) != 1 || len(imported.Skipped) != 0 || imported.Imported[0].Name != packageName {
			t.Fatalf("unexpected successful import entries: %#v", imported)
		}
		if imported.Imported[0].Target != filepath.Join(targetDir, "SKILL.md") {
			t.Fatalf("unexpected target path: %#v", imported.Imported[0])
		}
		if !imported.Mutation.Persisted || imported.Mutation.Idempotent || imported.Mutation.RuntimeApplied {
			t.Fatalf("unexpected successful import mutation: %#v", imported.Mutation)
		}
		if !slices.Contains(imported.Mutation.AffectedPaths, "skills."+packageName) {
			t.Fatalf("missing imported package in affected paths: %#v", imported.Mutation)
		}
	}

	first := postImport(false)
	assertWritten(first)
	assertInstalledVersion("version one")
	if _, err := os.Stat(filepath.Join(userDir, packageName)); !os.IsNotExist(err) {
		t.Fatalf("scope=global must not write to user layer: %v", err)
	}

	writePackageVersion("version two")
	skipped := postImport(false)
	if len(skipped.Imported) != 0 || len(skipped.Skipped) != 1 || skipped.Skipped[0].Name != packageName || skipped.Skipped[0].Reason == "" {
		t.Fatalf("unexpected skipped import entries: %#v", skipped)
	}
	if skipped.Mutation.Persisted || !skipped.Mutation.Idempotent || skipped.Mutation.RequiredAction != settings.MutationRequiredActionNone {
		t.Fatalf("a skipped import must report no persisted mutation: %#v", skipped.Mutation)
	}
	assertInstalledVersion("version one")

	updated := postImport(true)
	assertWritten(updated)
	assertInstalledVersion("version two")
}
