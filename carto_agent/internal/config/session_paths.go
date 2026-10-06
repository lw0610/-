package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// ValidateSessionID validates an application session identity before it is used
// as a directory name. It never sanitizes distinct identities into the same path.
func ValidateSessionID(raw string) (string, error) {
	sessionID := strings.TrimSpace(raw)
	if sessionID == "" {
		return "", fmt.Errorf("会话ID不能为空")
	}
	if strings.Contains(sessionID, "..") || strings.ContainsAny(sessionID, `/\\`) {
		return "", fmt.Errorf("会话ID包含非法路径字符")
	}
	if !sessionIDPattern.MatchString(sessionID) {
		return "", fmt.Errorf("会话ID格式非法")
	}
	return sessionID, nil
}

// NormalizeSessionProjectPath retains the optional empty-path convention used
// by session requests. Resolving a storage location additionally requires a path.
func NormalizeSessionProjectPath(raw string) (string, error) {
	projectPath := strings.TrimSpace(raw)
	if projectPath == "" {
		return "", nil
	}
	cleaned := filepath.Clean(projectPath)
	if !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("project_path 必须是绝对路径")
	}
	if strings.ContainsRune(cleaned, '\x00') {
		return "", fmt.Errorf("project_path 包含非法路径字符")
	}
	return cleaned, nil
}

// SessionProjectDirName is also used to identify the legacy project-local
// directory when migrating existing sessions into application-owned storage.
func SessionProjectDirName(projectPath string) string {
	baseName := strings.TrimSpace(filepath.Base(projectPath))
	if ext := filepath.Ext(baseName); ext != "" {
		baseName = strings.TrimSuffix(baseName, ext)
	}
	baseName = strings.TrimSpace(baseName)
	if baseName == "" || baseName == "." {
		return "project"
	}
	return baseName
}

func sessionProjectStorageKey(projectPath string) string {
	sum := sha256.Sum256([]byte(projectPath))
	return fmt.Sprintf("%s-%s", SessionProjectDirName(projectPath), hex.EncodeToString(sum[:8]))
}

// ResolveSessionsBaseDir is the shared canonical directory resolver for session
// persistence and long-lived tool processes. It performs no migration or writes.
func ResolveSessionsBaseDir(projectPath string) (string, error) {
	projectPath, err := NormalizeSessionProjectPath(projectPath)
	if err != nil {
		return "", err
	}
	if projectPath == "" {
		return "", fmt.Errorf("project_path 不能为空")
	}
	if filepath.Dir(projectPath) == projectPath {
		return "", fmt.Errorf("project_path 不能是文件系统根目录")
	}
	dataDir, err := ResolvePluginDataDir()
	if err != nil {
		return "", fmt.Errorf("解析 runtime sessions 目录失败: %w", err)
	}
	// Existing runtime-home configuration accepts paths relative to the service
	// working directory. Preserve that location while giving child processes an
	// absolute path that remains valid after their own working directory changes.
	dataDir, err = filepath.Abs(dataDir)
	if err != nil {
		return "", fmt.Errorf("解析 runtime sessions 绝对路径失败: %w", err)
	}
	return filepath.Join(dataDir, "sessions", sessionProjectStorageKey(projectPath)), nil
}

// ResolveSessionDir returns the same application-owned location that persists
// session.json. A tool must not recreate the old project-local migration source.
func ResolveSessionDir(projectPath, sessionID string) (string, error) {
	sessionID, err := ValidateSessionID(sessionID)
	if err != nil {
		return "", err
	}
	baseDir, err := ResolveSessionsBaseDir(projectPath)
	if err != nil {
		return "", err
	}
	return filepath.Join(baseDir, sessionID), nil
}
