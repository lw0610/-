package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/defaults"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/logger"
	"gopkg.in/yaml.v3"
)

var retiredEmbeddedAgentConfigs = []string{
	"high-performance-routing.yaml",
	"pyqgis-scripting.yaml",
}

func seedRuntimeAssets(cfg *config.Config) {
	if cfg == nil {
		return
	}

	configBaseDir := strings.TrimSpace(cfg.ConfigBaseDir)
	if configBaseDir == "" {
		logger.Warn("ConfigBaseDir 为空，跳过内置资源初始化")
		return
	}
	if err := os.MkdirAll(configBaseDir, 0o755); err != nil {
		logger.Warn("创建用户配置目录失败，跳过内置资源初始化: %v", err)
		return
	}

	agentCopied, agentReplaced, agentErr := seedAgentConfigsFromEmbedded(filepath.Join(configBaseDir, "agent_configs"))
	if agentErr != nil {
		logger.Warn("初始化 agent 配置失败: %v", agentErr)
	} else if agentCopied > 0 || agentReplaced > 0 {
		logger.Info("Agent 配置初始化完成: 新增=%d 替换=%d", agentCopied, agentReplaced)
	}
}

func seedAgentConfigsFromEmbedded(targetDir string) (copied int, replaced int, err error) {
	entries, err := fs.ReadDir(defaults.FS, "agent_configs")
	if err != nil {
		return 0, 0, fmt.Errorf("读取内置 agent_configs 目录失败: %w", err)
	}

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return 0, 0, fmt.Errorf("创建目标 agent_configs 目录失败: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		configName := strings.TrimSpace(entry.Name())
		if configName == "" {
			continue
		}
		srcDir := filepath.Join("agent_configs", configName)
		srcAgentPath := filepath.Join(srcDir, "agent.yaml")
		if _, statErr := fs.Stat(defaults.FS, srcAgentPath); statErr != nil {
			continue
		}

		legacyPath := filepath.Join(targetDir, configName+".yaml")
		dstDir := filepath.Join(targetDir, configName)
		dstAgentPath := filepath.Join(dstDir, "agent.yaml")

		if _, statErr := os.Stat(legacyPath); statErr == nil {
			if configName == "default" && shouldReplaceEmbeddedDefaultAgentConfig(legacyPath) {
				if err := os.RemoveAll(dstDir); err != nil {
					return copied, replaced, fmt.Errorf("清理旧默认配置目录失败 %s: %w", dstDir, err)
				}
				if err := os.Remove(legacyPath); err != nil && !os.IsNotExist(err) {
					return copied, replaced, fmt.Errorf("删除旧默认配置失败 %s: %w", legacyPath, err)
				}
				if _, err := copyMissingFilesFromEmbedded(srcDir, dstDir); err != nil {
					return copied, replaced, err
				}
				replaced++
				continue
			}
			if err := moveLocalFile(legacyPath, dstAgentPath); err != nil {
				return copied, replaced, fmt.Errorf("迁移旧配置文件失败 %s -> %s: %w", legacyPath, dstAgentPath, err)
			}
			if _, err := copyMissingFilesFromEmbedded(srcDir, dstDir); err != nil {
				return copied, replaced, err
			}
			continue
		} else if statErr != nil && !os.IsNotExist(statErr) {
			return copied, replaced, fmt.Errorf("读取目标配置文件失败 %s: %w", legacyPath, statErr)
		}

		if _, statErr := os.Stat(dstAgentPath); os.IsNotExist(statErr) {
			if _, err := copyMissingFilesFromEmbedded(srcDir, dstDir); err != nil {
				return copied, replaced, err
			}
			copied++
			continue
		} else if statErr != nil {
			return copied, replaced, fmt.Errorf("读取目标配置文件失败 %s: %w", dstAgentPath, statErr)
		}

		if configName == "default" && shouldReplaceEmbeddedDefaultAgentConfig(dstAgentPath) {
			if err := os.RemoveAll(dstDir); err != nil {
				return copied, replaced, fmt.Errorf("清理默认配置目录失败 %s: %w", dstDir, err)
			}
			if _, err := copyMissingFilesFromEmbedded(srcDir, dstDir); err != nil {
				return copied, replaced, err
			}
			replaced++
			continue
		}

		if _, err := copyMissingFilesFromEmbedded(srcDir, dstDir); err != nil {
			return copied, replaced, err
		}
	}

	for _, retiredName := range retiredEmbeddedAgentConfigs {
		retiredPath := filepath.Join(targetDir, retiredName)
		if err := os.Remove(retiredPath); err != nil && !os.IsNotExist(err) {
			return copied, replaced, fmt.Errorf("删除退役内置 agent 配置失败 %s: %w", retiredPath, err)
		}
		retiredDir := filepath.Join(targetDir, strings.TrimSuffix(retiredName, ".yaml"))
		if err := os.RemoveAll(retiredDir); err != nil {
			return copied, replaced, fmt.Errorf("删除退役内置 agent 配置目录失败 %s: %w", retiredDir, err)
		}
	}

	return copied, replaced, nil
}

func moveLocalFile(sourcePath, targetPath string) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	if err := os.Rename(sourcePath, targetPath); err == nil {
		return nil
	}
	raw, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(targetPath, raw, 0o644); err != nil {
		return err
	}
	return os.Remove(sourcePath)
}

func copyMissingFilesFromEmbedded(sourceDir, targetDir string) (int, error) {
	copied := 0
	err := fs.WalkDir(defaults.FS, sourceDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, relErr := filepath.Rel(sourceDir, path)
		if relErr != nil {
			return relErr
		}
		targetPath := filepath.Join(targetDir, rel)

		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		if _, statErr := os.Stat(targetPath); statErr == nil {
			return nil
		} else if !os.IsNotExist(statErr) {
			return statErr
		}

		if copyErr := copyFileFromEmbedded(path, targetPath); copyErr != nil {
			return copyErr
		}
		copied++
		return nil
	})
	if err != nil {
		return copied, fmt.Errorf("复制目录失败 %s -> %s: %w", sourceDir, targetDir, err)
	}
	return copied, nil
}

func copyFileFromEmbedded(srcPath, dstPath string) error {
	srcData, err := defaults.FS.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("读取内置文件失败 %s: %w", srcPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return fmt.Errorf("创建目标目录失败 %s: %w", filepath.Dir(dstPath), err)
	}
	if err := os.WriteFile(dstPath, srcData, 0o644); err != nil {
		return fmt.Errorf("写入目标文件失败 %s: %w", dstPath, err)
	}
	return nil
}

func shouldReplaceEmbeddedDefaultAgentConfig(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	var cfg config.AgentConfigFile
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return false
	}
	runtimeCfg := cfg.Runtime
	if runtimeCfg == nil {
		return true
	}

	if strings.TrimSpace(cfg.Meta.Description) == "系统默认智能体方案（4+1 优化架构，职责清晰，消除冗余）" {
		return true
	}

	if len(runtimeCfg.Global.SkillPackages) > 0 {
		return false
	}
	if len(runtimeCfg.Agents) != 1 {
		return false
	}
	orch, ok := runtimeCfg.Agents["orchestrator"]
	if !ok {
		return false
	}
	return len(orch.SkillPackages) == 0
}
