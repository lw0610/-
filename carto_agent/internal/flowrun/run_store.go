package flowrun

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var runIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{1,127}$`)

type FileRunStore struct{}

func RunPath(sessionDir, id string) (string, error) {
	if sessionDir == "" || !filepath.IsAbs(sessionDir) || !runIDPattern.MatchString(id) {
		return "", fmt.Errorf("流程运行目录或编号无效")
	}
	return filepath.Join(sessionDir, "flows", id, "run.json"), nil
}
func (FileRunStore) Save(ctx context.Context, run *RunState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if run == nil {
		return fmt.Errorf("流程记录为空")
	}
	path, err := RunPath(run.SessionDir, run.ID)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".run-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func (FileRunStore) Load(ctx context.Context, sessionDir, id string) (*RunState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := RunPath(sessionDir, id)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 32<<20 {
		return nil, fmt.Errorf("流程记录不是普通文件或超过32MB")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var run RunState
	if err = json.Unmarshal(raw, &run); err != nil {
		return nil, err
	}
	if run.APIVersion != APIVersion || run.ID != id || filepath.Clean(run.SessionDir) != filepath.Clean(sessionDir) || run.Plan == nil || run.Cursor < 0 || run.Cursor > len(run.Steps) || len(run.Steps) != len(run.Plan.Steps) {
		return nil, fmt.Errorf("流程记录结构或会话不匹配")
	}
	run.RunJSONPath = path
	return &run, nil
}

// MarkInterrupted is called for each discovered session during service startup.
func (s FileRunStore) MarkInterrupted(ctx context.Context, sessionDir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(sessionDir, "flows"))
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, entry := range entries {
		if !entry.IsDir() || !runIDPattern.MatchString(entry.Name()) {
			continue
		}
		run, err := s.Load(ctx, sessionDir, entry.Name())
		if err != nil {
			return ids, err
		}
		if run.Status != "running" {
			continue
		}
		run.Status = "interrupted"
		if run.Cursor < len(run.Steps) {
			run.Steps[run.Cursor].Status = "pending"
		}
		if err = s.Save(ctx, run); err != nil {
			return ids, err
		}
		ids = append(ids, run.ID)
	}
	return ids, nil
}

func cloneRun(run *RunState) (*RunState, error) {
	raw, err := json.Marshal(run)
	if err != nil {
		return nil, err
	}
	var result RunState
	err = json.Unmarshal(raw, &result)
	return &result, err
}
