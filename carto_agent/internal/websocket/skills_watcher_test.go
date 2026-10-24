package websocket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/autocarto/carto_agent/internal/testutil"
)

type snapshotListingOnlyCatalog struct {
	*settings.FileSkillService
	detailCalls  int
	listingCalls atomic.Int32
}

func (s *snapshotListingOnlyCatalog) ListSkillPackages() ([]settings.SkillPackageSummary, error) {
	s.listingCalls.Add(1)
	return s.FileSkillService.ListSkillPackages()
}

func (s *snapshotListingOnlyCatalog) GetSkillPackage(name string) (settings.SkillPackageDetail, error) {
	s.detailCalls++
	return settings.SkillPackageDetail{}, fmt.Errorf("snapshot must reuse listed path for %s", name)
}

func TestCollectSkillsSnapshotReusesListedPathsAndSeesPackageAddRemove(t *testing.T) {
	skillsDir := t.TempDir()
	writeWatcherFile(t, filepath.Join(skillsDir, "first", "SKILL.md"), "---\nname: first\ndescription: first\nkind: category\n---\n")
	catalog := &snapshotListingOnlyCatalog{FileSkillService: settings.NewFileSkillService(skillsDir, nil)}
	server := &DeepServer{skillCatalogStore: catalog}
	before, err := server.collectSkillsSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	assertSnapshotKeyContains(t, before, "first/SKILL.md")
	writeWatcherFile(t, filepath.Join(skillsDir, "second", "SKILL.md"), "---\nname: second\ndescription: second\nkind: category\n---\n")
	if err := os.RemoveAll(filepath.Join(skillsDir, "first")); err != nil {
		t.Fatal(err)
	}
	after, err := server.collectSkillsSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	assertSnapshotKeyContains(t, after, "second/SKILL.md")
	if _, ok := after["first/SKILL.md"]; ok || skillsSnapshotEqual(before, after) {
		t.Fatalf("package addition/removal not reflected: before=%#v after=%#v", before, after)
	}
	if catalog.detailCalls != 0 {
		t.Fatalf("unexpected %d per-package lookups", catalog.detailCalls)
	}
}

func TestWatchSkillsDirectorySkipsUnchangedCatalogAndInvalidatesOnToolYAML(t *testing.T) {
	t.Setenv("AUTOCARTO_ASSETS_ROOT", t.TempDir())
	skillsDir, toolsDir := t.TempDir(), t.TempDir()
	writeWatcherFile(t, filepath.Join(skillsDir, "demo", "SKILL.md"), "---\nname: demo\ndescription: demo\nkind: category\n---\n")
	catalog := &snapshotListingOnlyCatalog{FileSkillService: settings.NewFileSkillService(skillsDir, nil)}
	catalog.RuntimeToolDirs = []string{toolsDir}
	refresher := &stubCapabilityRefreshRequester{}
	server := &DeepServer{userSkillsDir: skillsDir, skillCatalogStore: catalog, capabilityRefresher: refresher}
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		server.watchSkillsDirectory(stop)
	}()
	t.Cleanup(func() { close(stop); <-done })
	waitForWatcherCondition(t, 5*time.Second, func() bool { return len(server.getSkillsSnapshot()) > 0 })
	time.Sleep(3 * skillsWatcherPollInterval)
	if got := catalog.listingCalls.Load(); got != 1 {
		t.Fatalf("unchanged files caused %d full catalog loads, want 1", got)
	}
	writeWatcherFile(t, filepath.Join(skillsDir, "demo", "config.yaml"), "enabled: false\n")
	waitForWatcherCondition(t, 5*time.Second, func() bool { return len(refresher.Reasons()) > 0 })
	if got := catalog.listingCalls.Load(); got != 2 {
		t.Fatalf("skill change caused %d full loads, want 2 total", got)
	}
	writeWatcherFile(t, filepath.Join(toolsDir, "nested", "tool.yml"), "description: changed tool definition\n")
	waitForWatcherCondition(t, 5*time.Second, func() bool { return catalog.listingCalls.Load() >= 3 })
	time.Sleep(2 * skillsWatcherPollInterval)
	if got := catalog.listingCalls.Load(); got != 3 {
		t.Fatalf("tool YAML should invalidate once, got %d full loads", got)
	}
}

func TestSkillsWatchInputsDetectOverlayOrderChange(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	roots := []string{first, second}
	catalog := settings.NewFileSkillServiceWithConfig(settings.FileSkillServiceConfig{
		ResolveRuntimeSkillDirs: func() []string { return roots },
	}, nil)
	server := &DeepServer{skillCatalogStore: catalog}
	before, err := server.collectSkillsWatchInputs()
	if err != nil {
		t.Fatal(err)
	}
	roots = []string{second, first}
	after, err := server.collectSkillsWatchInputs()
	if err != nil {
		t.Fatal(err)
	}
	if skillsSnapshotEqual(before, after) {
		t.Fatal("overlay precedence change must invalidate the catalog")
	}
}

func TestCollectSkillsSnapshotTracksPackageInternals(t *testing.T) {
	enterIsolatedWorkingDir(t)
	skillsDir := t.TempDir()
	writeWatcherFile(t, filepath.Join(skillsDir, "demo-package", "SKILL.md"), "---\nname: demo-package\ndescription: demo\nkind: category\n---\n")
	writeWatcherFile(t, filepath.Join(skillsDir, "demo-package", "config.yaml"), "enabled: true\n")
	writeWatcherFile(t, filepath.Join(skillsDir, "demo-package", "notes.txt"), "ignored\n")

	server := &DeepServer{userSkillsDir: skillsDir, skillsDir: skillsDir}
	snapshot, err := server.collectSkillsSnapshot()
	if err != nil {
		t.Fatalf("collectSkillsSnapshot error: %v", err)
	}
	for _, key := range []string{"demo-package/SKILL.md", "demo-package/config.yaml"} {
		assertSnapshotKeyContains(t, snapshot, key)
	}
	for key := range snapshot {
		if strings.HasSuffix(key, "demo-package/notes.txt") {
			t.Fatalf("unexpected non-package file tracked: %#v", snapshot)
		}
	}

	time.Sleep(5 * time.Millisecond)
	writeWatcherFile(t, filepath.Join(skillsDir, "demo-package", "config.yaml"), "enabled: false\n")
	updated, err := server.collectSkillsSnapshot()
	if err != nil {
		t.Fatalf("collectSkillsSnapshot error: %v", err)
	}
	if skillsSnapshotEqual(snapshot, updated) {
		t.Fatalf("expected snapshot change after tool yaml update")
	}
}

func TestWatchSkillsDirectoryStopsWhenSkillsDirMissing(t *testing.T) {
	enterIsolatedWorkingDir(t)
	// An inherited desktop assets root would add a live overlay even though
	// this test's user directory is missing.
	t.Setenv("AUTOCARTO_ASSETS_ROOT", t.TempDir())
	missingDir := filepath.Join(t.TempDir(), "missing-skills")
	server := &DeepServer{userSkillsDir: missingDir, skillsDir: missingDir}
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})

	go func() {
		defer close(doneCh)
		server.watchSkillsDirectory(stopCh)
	}()

	select {
	case <-doneCh:
		return
	case <-time.After(300 * time.Millisecond):
		close(stopCh)
		t.Fatal("expected skills watcher to stop after missing directory is detected")
	}
}

func TestWatchSkillsDirectoryKeepsRunningWhenPrimarySkillsDirMissingButOverlayRootExists(t *testing.T) {
	enterIsolatedWorkingDir(t)
	missingDir := filepath.Join(t.TempDir(), "missing-skills")
	pluginDir := t.TempDir()
	writeWatcherFile(t, filepath.Join(pluginDir, "plugin-package", "SKILL.md"), "---\nname: plugin-package\ndescription: plugin\nkind: category\n---\n")

	server := NewDeepServer(0, nil)
	server.userSkillsDir = missingDir
	server.skillsDir = missingDir
	server.agentRebuildConfig = &AgentRebuildConfig{PluginSkillsDir: pluginDir}
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})

	go func() {
		defer close(doneCh)
		server.watchSkillsDirectory(stopCh)
	}()

	select {
	case <-doneCh:
		t.Fatal("expected skills watcher to keep running while overlay roots remain available")
	case <-time.After(300 * time.Millisecond):
	}

	close(stopCh)
	select {
	case <-doneCh:
	case <-time.After(time.Second):
		t.Fatal("expected skills watcher to stop after stop channel closes")
	}
}

func TestStartSkillsWatcherResetsStateAfterMissingDir(t *testing.T) {
	enterIsolatedWorkingDir(t)
	t.Setenv("AUTOCARTO_ASSETS_ROOT", t.TempDir())
	missingDir := filepath.Join(t.TempDir(), "missing-skills")
	server := &DeepServer{userSkillsDir: missingDir, skillsDir: missingDir}

	server.startSkillsWatcher()
	waitForWatcherCondition(t, time.Second, func() bool {
		server.mu.Lock()
		defer server.mu.Unlock()
		return !server.skillsWatchStarted && server.skillsWatchStopCh == nil
	})
}

func TestDeepServerShutdownStopsSkillsWatcherAndClearsState(t *testing.T) {
	enterIsolatedWorkingDir(t)
	skillsDir := t.TempDir()
	writeWatcherFile(t, filepath.Join(skillsDir, "demo-package", "SKILL.md"), "---\nname: demo-package\ndescription: demo\nkind: category\n---\n")
	server := &DeepServer{userSkillsDir: skillsDir, skillsDir: skillsDir}

	server.startSkillsWatcher()
	if !server.skillsWatchStarted {
		t.Fatal("expected watcher to be marked started")
	}

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	if server.skillsWatchStarted {
		t.Fatal("expected watcher to be stopped after shutdown")
	}
	if server.skillsWatchStopCh != nil {
		t.Fatal("expected shutdown to clear watcher stop channel")
	}
}

func TestDeepServerStartAndShutdownReturnsServerClosed(t *testing.T) {
	enterIsolatedWorkingDir(t)
	skillsDir := t.TempDir()
	writeWatcherFile(t, filepath.Join(skillsDir, "demo-package", "SKILL.md"), "---\nname: demo-package\ndescription: demo\nkind: category\n---\n")
	server := NewDeepServer(testutil.ReserveFreePort(t), nil)
	server.SetSkillsDir(skillsDir)
	errCh := make(chan error, 1)

	go func() {
		errCh <- server.Start()
	}()

	waitForWatcherCondition(t, time.Second, func() bool {
		server.mu.Lock()
		defer server.mu.Unlock()
		return server.httpServer != nil && server.skillsWatchStarted
	})

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("expected http.ErrServerClosed, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected server start loop to return after shutdown")
	}
}

func TestWatchSkillsDirectoryRequestsCapabilityRefreshAfterDebouncedChange(t *testing.T) {
	enterIsolatedWorkingDir(t)
	t.Setenv("AUTOCARTO_ASSETS_ROOT", t.TempDir())
	skillsDir := t.TempDir()
	writeWatcherFile(t, filepath.Join(skillsDir, "demo-package", "SKILL.md"), "---\nname: demo-package\ndescription: demo\nkind: category\n---\n")
	server := NewDeepServer(0, nil)
	server.userSkillsDir = skillsDir
	server.skillsDir = skillsDir
	refresher := &stubCapabilityRefreshRequester{}
	server.capabilityRefresher = refresher
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})

	go func() {
		defer close(doneCh)
		server.watchSkillsDirectory(stopCh)
	}()

	waitForWatcherCondition(t, 5*time.Second, func() bool { return len(server.getSkillsSnapshot()) > 0 })
	writeWatcherFile(t, filepath.Join(skillsDir, "demo-package", "config.yaml"), "enabled: false\n")

	waitForWatcherCondition(t, 5*time.Second, func() bool {
		return len(refresher.Reasons()) > 0
	})
	close(stopCh)
	<-doneCh
	if reasons := refresher.Reasons(); len(reasons) == 0 || reasons[len(reasons)-1] != "skills_watcher" {
		t.Fatalf("expected skills watcher capability refresh, got %#v", reasons)
	}
}

func TestCollectSkillsSnapshotTracksOverlayRootsAndConfigFiles(t *testing.T) {
	enterIsolatedWorkingDir(t)
	globalDir := t.TempDir()
	userDir := t.TempDir()

	writeWatcherFile(t, filepath.Join(globalDir, "global-package", "SKILL.md"), "---\nname: global-package\ndescription: global\nkind: category\n---\n")
	writeWatcherFile(t, filepath.Join(userDir, "overlay-package", "SKILL.md"), "---\nname: overlay-package\ndescription: workflow\nkind: category\n---\n")
	writeWatcherFile(t, filepath.Join(userDir, "overlay-package", "config.yaml"), "enabled: true\n")

	server := &DeepServer{builtinSkillsDir: globalDir, userSkillsDir: userDir, skillsDir: userDir}
	snapshot, err := server.collectSkillsSnapshot()
	if err != nil {
		t.Fatalf("collectSkillsSnapshot error: %v", err)
	}
	assertSnapshotKeyContains(t, snapshot, filepath.ToSlash(filepath.Join("global-package", "SKILL.md")))
	assertSnapshotKeyContains(t, snapshot, filepath.ToSlash(filepath.Join("overlay-package", "config.yaml")))
}

func TestWatchSkillsDirectoryRequestsCapabilityRefreshAfterExclusiveOverlayChange(t *testing.T) {
	enterIsolatedWorkingDir(t)
	t.Setenv("AUTOCARTO_ASSETS_ROOT", t.TempDir())
	globalDir := t.TempDir()
	userDir := t.TempDir()

	writeWatcherFile(t, filepath.Join(globalDir, "global-package", "SKILL.md"), "---\nname: global-package\ndescription: global\nkind: category\n---\n")
	writeWatcherFile(t, filepath.Join(userDir, "overlay-package", "SKILL.md"), "---\nname: overlay-package\ndescription: workflow\nkind: category\n---\n")
	writeWatcherFile(t, filepath.Join(userDir, "overlay-package", "config.yaml"), "enabled: true\n")

	server := NewDeepServer(0, nil)
	server.builtinSkillsDir = globalDir
	server.userSkillsDir = userDir
	server.skillsDir = userDir
	refresher := &stubCapabilityRefreshRequester{}
	server.capabilityRefresher = refresher
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})

	go func() {
		defer close(doneCh)
		server.watchSkillsDirectory(stopCh)
	}()

	waitForWatcherCondition(t, 5*time.Second, func() bool { return len(server.getSkillsSnapshot()) > 0 })
	writeWatcherFile(t, filepath.Join(userDir, "overlay-package", "config.yaml"), "enabled: false\n")

	waitForWatcherCondition(t, 2*time.Second, func() bool {
		return len(refresher.Reasons()) > 0
	})
	close(stopCh)
	<-doneCh
	if reasons := refresher.Reasons(); len(reasons) == 0 || reasons[len(reasons)-1] != "skills_watcher" {
		t.Fatalf("expected overlay watcher capability refresh, got %#v", reasons)
	}
}

func TestWatchSkillsDirectoryIgnoresHiddenLowerLayerChanges(t *testing.T) {
	t.Setenv("AUTOCARTO_ASSETS_ROOT", t.TempDir())
	globalDir := t.TempDir()
	userDir := t.TempDir()

	writeWatcherFile(t, filepath.Join(globalDir, "shared-package", "SKILL.md"), "---\nname: shared-package\ndescription: global\nkind: category\n---\n")
	writeWatcherFile(t, filepath.Join(userDir, "shared-package", "SKILL.md"), "---\nname: shared-package\ndescription: user\nkind: category\n---\n")

	server := NewDeepServer(0, nil)
	server.builtinSkillsDir = globalDir
	server.userSkillsDir = userDir
	server.skillsDir = userDir
	refresher := &stubCapabilityRefreshRequester{}
	server.capabilityRefresher = refresher
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})

	go func() {
		defer close(doneCh)
		server.watchSkillsDirectory(stopCh)
	}()

	waitForWatcherCondition(t, 5*time.Second, func() bool { return len(server.getSkillsSnapshot()) > 0 })
	writeWatcherFile(t, filepath.Join(globalDir, "shared-package", "SKILL.md"), "---\nname: shared-package\ndescription: global changed\nkind: category\n---\n")
	time.Sleep(skillsWatcherPollInterval + skillsWatcherDebounce + 150*time.Millisecond)
	close(stopCh)
	<-doneCh
	if reasons := refresher.Reasons(); len(reasons) != 0 {
		t.Fatalf("expected hidden lower-layer change not to trigger refresh, got %#v", reasons)
	}
}

func TestWatchSkillsDirectoryRefreshesWhenVisibleOverlaySourceChangesWithSameFileMetadata(t *testing.T) {
	enterIsolatedWorkingDir(t)
	globalDir := t.TempDir()
	userDir := t.TempDir()
	raw := "---\nname: shared-package\ndescription: same package\nkind: category\n---\n"
	globalSkillPath := filepath.Join(globalDir, "shared-package", "SKILL.md")
	writeWatcherFile(t, globalSkillPath, raw)
	fixedTime := time.Unix(1711111111, 0)
	if err := os.Chtimes(globalSkillPath, fixedTime, fixedTime); err != nil {
		t.Fatalf("chtimes global skill error: %v", err)
	}

	server := NewDeepServer(0, nil)
	server.builtinSkillsDir = globalDir
	server.userSkillsDir = userDir
	server.skillsDir = userDir
	initialSnapshot, err := server.collectSkillsSnapshot()
	if err != nil {
		t.Fatalf("collectSkillsSnapshot initial error: %v", err)
	}
	exclusiveSkillPath := filepath.Join(userDir, "shared-package", "SKILL.md")
	writeWatcherFile(t, exclusiveSkillPath, raw)
	if err := os.Chtimes(exclusiveSkillPath, fixedTime, fixedTime); err != nil {
		t.Fatalf("chtimes exclusive skill error: %v", err)
	}
	updatedSnapshot, err := server.collectSkillsSnapshot()
	if err != nil {
		t.Fatalf("collectSkillsSnapshot updated error: %v", err)
	}
	if skillsSnapshotEqual(initialSnapshot, updatedSnapshot) {
		t.Fatalf("expected snapshot change when visible package source switches, initial=%#v updated=%#v", initialSnapshot, updatedSnapshot)
	}
}

func TestIsSkillsDirMissingErrorRecognizesWrappedPathError(t *testing.T) {
	skillsDir := "/tmp/demo/config/skills"
	err := fmt.Errorf("walk snapshot: %w", &os.PathError{Op: "open", Path: skillsDir, Err: os.ErrNotExist})
	if !isSkillsDirMissingError(err, skillsDir) {
		t.Fatal("expected wrapped missing-dir error to be recognized")
	}
}

func TestIsSkillsDirMissingErrorRecognizesGenericNoSuchFileMessage(t *testing.T) {
	err := fmt.Errorf("open /tmp/other/path/config/skills: no such file or directory")
	if !isSkillsDirMissingError(err, "/tmp/demo/config/skills") {
		t.Fatal("expected generic no-such-file watcher error to be recognized")
	}
}

func TestSkillsSnapshotAccessorsCloneMaps(t *testing.T) {
	server := NewDeepServer(0, nil)
	original := map[string]string{"demo-package/SKILL.md": "source|global||1:1"}

	server.updateSkillsSnapshot(original)
	original["demo-package/SKILL.md"] = "mutated|global||2:2"

	snapshot := server.getSkillsSnapshot()
	if got := snapshot["demo-package/SKILL.md"]; got != "source|global||1:1" {
		t.Fatalf("expected stored snapshot to ignore caller mutation, got %q", got)
	}

	snapshot["demo-package/SKILL.md"] = "changed-by-caller"
	again := server.getSkillsSnapshot()
	if got := again["demo-package/SKILL.md"]; got != "source|global||1:1" {
		t.Fatalf("expected returned snapshot to be cloned, got %q", got)
	}
}

func waitForWatcherCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("watcher condition was not satisfied before timeout")
}

func writeWatcherFile(t *testing.T, path string, raw string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir error: %v", err)
	}
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("write file error: %v", err)
	}
}

func assertSnapshotKeyContains(t *testing.T, snapshot map[string]string, suffix string) {
	t.Helper()
	for key := range snapshot {
		if strings.HasSuffix(key, suffix) {
			return
		}
	}
	t.Fatalf("expected snapshot key with suffix %s in %#v", suffix, snapshot)
}
