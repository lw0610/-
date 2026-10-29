package websocket

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func requireCrossDeviceDir(t *testing.T) string {
	t.Helper()
	otherRoot := "/dev/shm"
	if _, err := os.Stat(otherRoot); err != nil {
		t.Skipf("cross-device dir unavailable: %v", err)
	}
	workspaceRoot := t.TempDir()
	workspaceInfo, err := os.Stat(workspaceRoot)
	if err != nil {
		t.Fatalf("stat workspaceRoot failed: %v", err)
	}
	otherInfo, err := os.Stat(otherRoot)
	if err != nil {
		t.Fatalf("stat otherRoot failed: %v", err)
	}
	workspaceDev, ok := deviceIDFromFileInfo(workspaceInfo)
	if !ok {
		t.Skip("workspace stat does not expose device id")
	}
	otherDev, ok := deviceIDFromFileInfo(otherInfo)
	if !ok {
		t.Skip("cross-device stat does not expose device id")
	}
	if workspaceDev == otherDev {
		t.Skip("/dev/shm is not on a different device in this environment")
	}
	return otherRoot
}

func deviceIDFromFileInfo(info os.FileInfo) (uint64, bool) {
	if info == nil || info.Sys() == nil {
		return 0, false
	}
	value := reflect.ValueOf(info.Sys())
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return 0, false
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return 0, false
	}
	devField := value.FieldByName("Dev")
	if !devField.IsValid() {
		return 0, false
	}
	switch devField.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return devField.Uint(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		dev := devField.Int()
		if dev < 0 {
			return 0, false
		}
		return uint64(dev), true
	default:
		return 0, false
	}
}

// TestGetSessionsBaseDirProjectIsolation verifies that two projects in the same
// directory produce different sessionsDir paths.
func TestGetSessionsBaseDirProjectIsolation(t *testing.T) {
	server := &DeepServer{}
	projectRoot := t.TempDir()

	projectA := filepath.Join(projectRoot, "project_a.qgz")
	projectB := filepath.Join(projectRoot, "project_b.qgs")

	os.WriteFile(projectA, []byte{}, 0644)
	os.WriteFile(projectB, []byte{}, 0644)

	dirA, err := server.getSessionsBaseDir(projectA)
	if err != nil {
		t.Fatalf("getSessionsBaseDir(project_a) failed: %v", err)
	}
	dirB, err := server.getSessionsBaseDir(projectB)
	if err != nil {
		t.Fatalf("getSessionsBaseDir(project_b) failed: %v", err)
	}

	if dirA == dirB {
		t.Fatalf("expected different session dirs, got same: %s", dirA)
	}

	expectedA, err := resolveCanonicalSessionsBaseDir(projectA)
	if err != nil {
		t.Fatalf("resolveCanonicalSessionsBaseDir(project_a) failed: %v", err)
	}
	expectedB, err := resolveCanonicalSessionsBaseDir(projectB)
	if err != nil {
		t.Fatalf("resolveCanonicalSessionsBaseDir(project_b) failed: %v", err)
	}
	if dirA != expectedA {
		t.Fatalf("dirA mismatch: got=%s, want=%s", dirA, expectedA)
	}
	if dirB != expectedB {
		t.Fatalf("dirB mismatch: got=%s, want=%s", dirB, expectedB)
	}
}

func TestSessionReadsDoNotMigrateLegacyProjectFileDir(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	server := &DeepServer{}
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "demo.qgz")

	if err := os.WriteFile(projectPath, []byte{}, 0644); err != nil {
		t.Fatalf("write project file failed: %v", err)
	}

	legacySessionDir := filepath.Join(projectRoot, "autoc", "sessions", "demo.qgz", "sid-001")
	if err := os.MkdirAll(legacySessionDir, 0755); err != nil {
		t.Fatalf("mkdir legacy session dir failed: %v", err)
	}
	legacySessionFile := filepath.Join(legacySessionDir, "session.json")
	if err := os.WriteFile(legacySessionFile, []byte(`{"session_id":"sid-001"}`), 0644); err != nil {
		t.Fatalf("write legacy session.json failed: %v", err)
	}

	baseDir, err := server.getSessionsBaseDir(projectPath)
	if err != nil {
		t.Fatalf("getSessionsBaseDir failed: %v", err)
	}
	if !filepath.IsAbs(baseDir) {
		t.Fatalf("expected runtime-owned absolute session dir, got=%s", baseDir)
	}
	canonicalSessionFile := filepath.Join(baseDir, "sid-001", "session.json")
	if sessions, err := server.listSessions(projectPath); err != nil || len(sessions) != 0 {
		t.Fatalf("listing an uninitialized session root failed: %v %v", sessions, err)
	}
	if _, err := os.Stat(canonicalSessionFile); !os.IsNotExist(err) {
		t.Fatalf("a read migrated legacy session.json: %v", err)
	}
	if _, err := os.Stat(baseDir); !os.IsNotExist(err) {
		t.Fatalf("a read created the session root: %v", err)
	}
	if got, err := os.ReadFile(legacySessionFile); err != nil || string(got) != `{"session_id":"sid-001"}` {
		t.Fatalf("a read changed legacy session.json: %q (%v)", got, err)
	}
}

// TestListSessionsProjectIsolation verifies that sessions from different projects
// are not visible to each other.
func TestListSessionsProjectIsolation(t *testing.T) {
	server := &DeepServer{}
	projectRoot := t.TempDir()

	projectA := filepath.Join(projectRoot, "project_a.qgz")
	projectB := filepath.Join(projectRoot, "project_b.qgs")

	sessionA := map[string]any{
		"session_id":   "sid-a-001",
		"project_path": projectA,
		"messages":     []any{},
		"tool_calls":   []any{},
		"snapshots":    []any{},
	}
	if err := server.saveSessionToFile(sessionA, projectA, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile for project_a failed: %v", err)
	}

	sessionB := map[string]any{
		"session_id":   "sid-b-001",
		"project_path": projectB,
		"messages":     []any{},
		"tool_calls":   []any{},
		"snapshots":    []any{},
	}
	if err := server.saveSessionToFile(sessionB, projectB, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile for project_b failed: %v", err)
	}

	sessionsA, err := server.listSessions(projectA)
	if err != nil {
		t.Fatalf("listSessions(project_a) failed: %v", err)
	}
	if len(sessionsA) != 1 {
		t.Fatalf("expected 1 session for project_a, got %d", len(sessionsA))
	}
	if sid, _ := sessionsA[0]["session_id"].(string); sid != "sid-a-001" {
		t.Fatalf("expected session_id=sid-a-001, got=%s", sid)
	}

	sessionsB, err := server.listSessions(projectB)
	if err != nil {
		t.Fatalf("listSessions(project_b) failed: %v", err)
	}
	if len(sessionsB) != 1 {
		t.Fatalf("expected 1 session for project_b, got %d", len(sessionsB))
	}
	if sid, _ := sessionsB[0]["session_id"].(string); sid != "sid-b-001" {
		t.Fatalf("expected session_id=sid-b-001, got=%s", sid)
	}
}

// TestWorkspaceConfigReadWrite verifies workspace.yaml registration and persistence.
func TestWorkspaceConfigReadWrite(t *testing.T) {
	projectDir := t.TempDir()

	if err := registerProjectInWorkspace(projectDir, "project_a.qgz"); err != nil {
		t.Fatalf("registerProjectInWorkspace(project_a) failed: %v", err)
	}
	if err := registerProjectInWorkspace(projectDir, "project_b.qgs"); err != nil {
		t.Fatalf("registerProjectInWorkspace(project_b) failed: %v", err)
	}

	cfg, err := loadWorkspaceConfig(projectDir)
	if err != nil {
		t.Fatalf("loadWorkspaceConfig failed: %v", err)
	}

	if cfg.Version != 1 {
		t.Fatalf("version mismatch: got=%d, want=1", cfg.Version)
	}
	if len(cfg.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(cfg.Projects))
	}
	if cfg.Projects[0].File != "project_a.qgz" {
		t.Fatalf("first project should be project_a.qgz, got=%s", cfg.Projects[0].File)
	}
	if cfg.Projects[1].File != "project_b.qgs" {
		t.Fatalf("second project should be project_b.qgs, got=%s", cfg.Projects[1].File)
	}

	// Re-register — should update, not duplicate
	if err := registerProjectInWorkspace(projectDir, "project_a.qgz"); err != nil {
		t.Fatalf("re-register project_a failed: %v", err)
	}
	cfg2, err := loadWorkspaceConfig(projectDir)
	if err != nil {
		t.Fatalf("loadWorkspaceConfig after re-register failed: %v", err)
	}
	if len(cfg2.Projects) != 2 {
		t.Fatalf("expected 2 projects after re-register, got %d", len(cfg2.Projects))
	}
}

func TestMigrateLegacyWorkspaceConfigFallsBackAcrossDevices(t *testing.T) {
	projectDir := t.TempDir()
	crossDeviceRoot := requireCrossDeviceDir(t)
	legacyPath := legacyWorkspaceConfigPath(projectDir)
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("mkdir legacy dir failed: %v", err)
	}
	legacyContent := []byte("version: 1\nprojects:\n  - file: demo.qgz\n")
	if err := os.WriteFile(legacyPath, legacyContent, 0o644); err != nil {
		t.Fatalf("write legacy workspace failed: %v", err)
	}
	cfgPath := filepath.Join(crossDeviceRoot, "autocarto-workspace-test", filepath.Base(projectDir), workspaceConfigFileName)
	_ = os.RemoveAll(filepath.Dir(cfgPath))

	if err := migrateLegacyWorkspaceConfig(projectDir, cfgPath); err != nil {
		t.Fatalf("migrateLegacyWorkspaceConfig failed: %v", err)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy workspace config should be removed, err=%v", err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read migrated workspace failed: %v", err)
	}
	if string(data) != string(legacyContent) {
		t.Fatalf("migrated workspace content mismatch: got=%q want=%q", string(data), string(legacyContent))
	}
	_ = os.RemoveAll(filepath.Dir(cfgPath))
}
