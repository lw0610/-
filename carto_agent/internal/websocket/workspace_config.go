package websocket

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"gopkg.in/yaml.v3"
)

const workspaceConfigFileName = "workspace.yaml"

type workspaceConfig struct {
	Version  int                     `yaml:"version"`
	Projects []workspaceProjectEntry `yaml:"projects"`
}

type workspaceProjectEntry struct {
	File         string `yaml:"file"`
	RegisteredAt string `yaml:"registered_at"`
	LastAccessed string `yaml:"last_accessed"`
}

// workspaceConfigMu protects concurrent access to workspace.yaml files.
// Keyed by projectDir (the parent directory containing autoc/).
var workspaceConfigMu sync.Map // map[string]*sync.Mutex

func getWorkspaceConfigMu(projectDir string) *sync.Mutex {
	key := filepath.Clean(projectDir)
	val, _ := workspaceConfigMu.LoadOrStore(key, &sync.Mutex{})
	return val.(*sync.Mutex)
}

func workspaceConfigStorageKey(projectDir string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(projectDir)))
	return hex.EncodeToString(sum[:8])
}

func legacyWorkspaceConfigPath(projectDir string) string {
	return filepath.Join(projectDir, "autoc", workspaceConfigFileName)
}

func workspaceConfigPath(projectDir string) (string, error) {
	dataDir, err := config.ResolvePluginDataDir()
	if err != nil {
		return "", fmt.Errorf("解析 workspace runtime 目录失败: %w", err)
	}
	return filepath.Join(dataDir, "workspaces", workspaceConfigStorageKey(projectDir), workspaceConfigFileName), nil
}

func migrateLegacyWorkspaceConfig(projectDir, cfgPath string) error {
	legacyPath := legacyWorkspaceConfigPath(projectDir)
	if legacyPath == cfgPath {
		return nil
	}
	if _, err := os.Stat(legacyPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取 legacy workspace.yaml 失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return fmt.Errorf("创建 workspace runtime 目录失败: %w", err)
	}
	if err := movePathWithCrossDeviceFallback(legacyPath, cfgPath); err != nil {
		return fmt.Errorf("迁移 legacy workspace.yaml 失败: %w", err)
	}
	return nil
}

func loadWorkspaceConfig(projectDir string) (workspaceConfig, error) {
	cfgPath, err := workspaceConfigPath(projectDir)
	if err != nil {
		return workspaceConfig{}, err
	}
	if err := migrateLegacyWorkspaceConfig(projectDir, cfgPath); err != nil {
		return workspaceConfig{}, err
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return workspaceConfig{Version: 1}, nil
		}
		return workspaceConfig{}, fmt.Errorf("读取 workspace.yaml 失败: %w", err)
	}

	var cfg workspaceConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return workspaceConfig{}, fmt.Errorf("解析 workspace.yaml 失败: %w", err)
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	return cfg, nil
}

func saveWorkspaceConfig(projectDir string, cfg workspaceConfig) error {
	cfgPath, err := workspaceConfigPath(projectDir)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(cfgPath), 0755); err != nil {
		return fmt.Errorf("创建 workspace runtime 目录失败: %w", err)
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("序列化 workspace.yaml 失败: %w", err)
	}

	// atomic write via temp file + rename
	tmpPath := cfgPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("写入 workspace.yaml 临时文件失败: %w", err)
	}
	if err := os.Rename(tmpPath, cfgPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("替换 workspace.yaml 失败: %w", err)
	}
	legacyPath := legacyWorkspaceConfigPath(projectDir)
	if legacyPath != cfgPath {
		_ = os.Remove(legacyPath)
	}
	return nil
}

func registerProjectInWorkspace(projectDir, fileName string) error {
	if projectDir == "" || fileName == "" {
		return nil
	}

	mu := getWorkspaceConfigMu(projectDir)
	mu.Lock()
	defer mu.Unlock()

	cfg, err := loadWorkspaceConfig(projectDir)
	if err != nil {
		return err
	}

	nowText := time.Now().UTC().Format(time.RFC3339)
	found := false
	for i := range cfg.Projects {
		if strings.EqualFold(cfg.Projects[i].File, fileName) {
			cfg.Projects[i].File = fileName // preserve original casing
			cfg.Projects[i].LastAccessed = nowText
			found = true
			break
		}
	}
	if !found {
		cfg.Projects = append(cfg.Projects, workspaceProjectEntry{
			File:         fileName,
			RegisteredAt: nowText,
			LastAccessed: nowText,
		})
	}

	sort.Slice(cfg.Projects, func(i, j int) bool {
		return cfg.Projects[i].File < cfg.Projects[j].File
	})

	return saveWorkspaceConfig(projectDir, cfg)
}
