package flow

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const MaxFileBytes int64 = 200 << 20
const MaxPackageBytes int64 = 2 << 30

var (
	ErrNotFound   = errors.New("流程不存在")
	ErrReadOnly   = errors.New("内置流程只读，请复制为我的流程后修改")
	ErrConflict   = errors.New("FLOW_DUPLICATE_ID: 流程编号已存在，请覆盖或另存为新编号")
	flowIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)
	storeLocks    sync.Map
)

type StoredResource struct {
	Path   string `json:"path"`
	Type   string `json:"type"`
	Size   int64  `json:"size"`
	Exists bool   `json:"exists"`
}

type StoredFlow struct {
	Meta        FlowMetadata     `json:"meta"`
	FlowMD      string           `json:"flow_md"`
	Source      string           `json:"source"`
	Dir         string           `json:"-"`
	UpdatedAt   time.Time        `json:"updated_at"`
	Resources   []StoredResource `json:"resources"`
	Plan        any              `json:"plan"`
	Diagnostics []Diagnostic     `json:"diagnostics"`
}

type FlowSummary struct {
	PlanStatus         string         `json:"plan_status"`
	ID                 string         `json:"id"`
	Name               string         `json:"name"`
	Description        string         `json:"description"`
	Version            string         `json:"version"`
	Source             string         `json:"source"`
	UpdatedAt          time.Time      `json:"updated_at"`
	Enabled            bool           `json:"enabled"`
	DiagnosticsSummary map[string]int `json:"diagnostics_summary"`
}

// Store owns immutable bundled flows and user packages. Roots sharing a user
// directory share a lock, including independently assembled HTTP handlers.
type Store struct {
	BuiltinDir string
	UserDir    string
	Bindings   *bindings.Store
	mu         *sync.Mutex
}

func NewStore(builtinDir, userDir string, bindingStore *bindings.Store) *Store {
	key, _ := filepath.Abs(userDir)
	lock, _ := storeLocks.LoadOrStore(key, &sync.Mutex{})
	return &Store{BuiltinDir: builtinDir, UserDir: userDir, Bindings: bindingStore, mu: lock.(*sync.Mutex)}
}

func ValidateID(id string) error {
	if !flowIDPattern.MatchString(id) {
		return fmt.Errorf("流程编号须为 2–64 位小写字母、数字或短横线")
	}
	return nil
}

func PackageID(raw, directory, override string) (string, error) {
	meta, diagnostics := ParseMetadata(raw)
	for _, d := range diagnostics {
		if d.Level == "error" {
			return "", fmt.Errorf("%s: %s", d.Code, d.Message)
		}
	}
	id := strings.TrimSpace(override)
	if id == "" {
		id = strings.TrimSpace(meta.ID)
	}
	if id == "" {
		id = directory
		if !flowIDPattern.MatchString(id) {
			sum := sha256.Sum256([]byte(directory))
			id = "flow-" + hex.EncodeToString(sum[:4])
		}
	}
	return id, ValidateID(id)
}

func (s *Store) List() ([]FlowSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []FlowSummary{}
	seen := map[string]bool{}
	for _, root := range []string{s.BuiltinDir, s.UserDir} {
		if root == "" {
			continue
		}
		entries, err := os.ReadDir(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			id := entry.Name()
			if !entry.IsDir() || ValidateID(id) != nil || seen[id] {
				continue
			}
			item, err := s.get(id)
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			seen[id] = true
			items = append(items, FlowSummary{ID: id, Name: item.Meta.Name, Description: item.Meta.Description, Version: item.Meta.Version, Source: item.Source, UpdatedAt: item.UpdatedAt, Enabled: true, DiagnosticsSummary: map[string]int{"errors": 0, "warnings": 0, "infos": 0}})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (s *Store) Get(id string) (*StoredFlow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(id)
}

func (s *Store) get(id string) (*StoredFlow, error) {
	if err := ValidateID(id); err != nil {
		return nil, err
	}
	for _, candidate := range []struct{ root, source string }{{s.BuiltinDir, "builtin"}, {s.UserDir, "user"}} {
		if candidate.root == "" {
			continue
		}
		dir := filepath.Join(candidate.root, id)
		if err := rejectSymlinks(dir); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		path := filepath.Join(dir, "FLOW.md")
		raw, err := readBounded(path, MaxFileBytes)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		meta, _ := ParseMetadata(string(raw))
		meta.ID = id
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		resources, err := listResources(dir)
		if err != nil {
			return nil, err
		}
		return &StoredFlow{Meta: meta, FlowMD: string(raw), Source: candidate.source, Dir: dir, UpdatedAt: info.ModTime(), Resources: resources, Diagnostics: []Diagnostic{}}, nil
	}
	return nil, ErrNotFound
}

func (s *Store) Create(raw, id string) (*StoredFlow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		m, _ := ParseMetadata(raw)
		id = m.ID
		if id == "" {
			sum := sha256.Sum256([]byte(m.Name))
			id = "flow-" + hex.EncodeToString(sum[:4])
		}
	}
	return s.write(raw, id, "", false)
}

func (s *Store) Save(id, raw string) (*StoredFlow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.get(id)
	if err != nil {
		return nil, err
	}
	if current.Source == "builtin" {
		return nil, ErrReadOnly
	}
	return s.write(raw, id, current.Dir, true)
}

func (s *Store) Copy(id, newID, newName string) (*StoredFlow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.get(id)
	if err != nil {
		return nil, err
	}
	raw := current.FlowMD
	if newName != "" {
		raw = rewriteMetadata(raw, newID, newName)
	} else {
		raw = rewriteMetadata(raw, newID, current.Meta.Name)
	}
	return s.write(raw, newID, current.Dir, false)
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.get(id)
	if err != nil {
		return err
	}
	if item.Source == "builtin" {
		return ErrReadOnly
	}
	return os.RemoveAll(item.Dir)
}

func (s *Store) write(raw, id, source string, overwrite bool) (*StoredFlow, error) {
	if err := ValidateID(id); err != nil {
		return nil, err
	}
	meta, ds := ParseMetadata(raw)
	if strings.TrimSpace(meta.Name) == "" {
		return nil, fmt.Errorf("FLOW_NO_NAME: 请填写流程名称")
	}
	for _, d := range ds {
		if d.Level == "error" {
			return nil, fmt.Errorf("%s: %s", d.Code, d.Message)
		}
	}
	if int64(len(raw)) > MaxFileBytes {
		return nil, fmt.Errorf("流程说明超过文件大小上限")
	}
	if s.UserDir == "" {
		return nil, fmt.Errorf("用户流程目录未配置")
	}
	if s.BuiltinDir != "" {
		if _, err := os.Lstat(filepath.Join(s.BuiltinDir, id)); err == nil {
			return nil, ErrReadOnly
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	target := filepath.Join(s.UserDir, id)
	_, statErr := os.Lstat(target)
	exists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return nil, statErr
	}
	if exists && !overwrite {
		return nil, ErrConflict
	}
	if err := os.MkdirAll(s.UserDir, 0700); err != nil {
		return nil, err
	}
	if err := rejectSymlinks(s.UserDir); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(s.UserDir, ".flow-stage-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	if source != "" {
		if err = copyTree(source, stage, false); err != nil {
			return nil, err
		}
	}
	if err = os.WriteFile(filepath.Join(stage, "FLOW.md"), []byte(rewriteMetadata(raw, id, meta.Name)), 0600); err != nil {
		return nil, err
	}
	lock, err := ReadLock(stage)
	if err != nil {
		return nil, err
	}
	if err = writeLock(stage, retainedLock(lock, Parse(raw))); err != nil {
		return nil, err
	}
	if exists {
		if err = rejectSymlinks(target); err != nil {
			return nil, err
		}
		history := filepath.Join(stage, ".history")
		if _, e := os.Stat(filepath.Join(target, ".history")); e == nil {
			if err = copyTree(filepath.Join(target, ".history"), history, true); err != nil {
				return nil, err
			}
		}
		version := time.Now().UTC().Format("20060102T150405.000000000")
		if err = copyTree(target, filepath.Join(history, version), false); err != nil {
			return nil, err
		}
		hold, err := os.MkdirTemp(s.UserDir, ".flow-backup-")
		if err != nil {
			return nil, err
		}
		os.Remove(hold)
		if err = os.Rename(target, hold); err != nil {
			return nil, err
		}
		if err = os.Rename(stage, target); err != nil {
			_ = os.Rename(hold, target)
			return nil, err
		}
		_ = os.RemoveAll(hold)
	} else if err = os.Rename(stage, target); err != nil {
		return nil, err
	}
	return s.get(id)
}

func rewriteMetadata(raw, id, name string) string {
	meta, _ := ParseMetadata(raw)
	meta.ID = id
	meta.Name = name
	// Preserve the body exactly; changing identity must not alter user prose.
	body := strings.ReplaceAll(raw, "\r\n", "\n")
	if strings.HasPrefix(body, "---\n") {
		if at := strings.Index(body[4:], "\n---"); at >= 0 {
			body = strings.TrimPrefix(body[4+at+4:], "\n")
		}
	}
	return metadataYAML(meta) + body
}

func readBounded(path string, limit int64) ([]byte, error) {
	if err := rejectSymlinks(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	i, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !i.Mode().IsRegular() {
		return nil, fmt.Errorf("只接受普通文件")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("文件超过大小上限")
	}
	return b, nil
}

func rejectSymlinks(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for {
		i, e := os.Lstat(path)
		if e != nil {
			return e
		}
		if i.Mode()&os.ModeSymlink != 0 && !(runtime.GOOS == "darwin" && (path == "/var" || path == "/tmp")) {
			return fmt.Errorf("不接受符号链接: %s", path)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

func safeRelative(path string) (string, error) {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, "\\") {
		return "", fmt.Errorf("资源路径必须是包内相对路径")
	}
	for _, part := range strings.Split(path, "/") {
		if part == ".." || part == ".history" {
			return "", fmt.Errorf("资源路径不能越界或访问历史目录")
		}
	}
	clean := filepath.Clean(path)
	if clean == "." {
		return "", fmt.Errorf("资源路径不能为空")
	}
	return clean, nil
}

func copyTree(source, target string, history bool) error {
	if err := rejectSymlinks(source); err != nil {
		return err
	}
	var total int64
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if !history && rel != "." && entry.Name() == ".history" && entry.IsDir() {
			return filepath.SkipDir
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("不接受符号链接: %s", rel)
		}
		dest := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		i, err := entry.Info()
		if err != nil {
			return err
		}
		if !i.Mode().IsRegular() {
			return fmt.Errorf("不接受特殊文件: %s", rel)
		}
		if i.Size() > MaxFileBytes {
			return fmt.Errorf("文件超过 200 MB: %s", rel)
		}
		total += i.Size()
		if total > MaxPackageBytes {
			return fmt.Errorf("流程包超过 2 GB")
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			in.Close()
			return err
		}
		n, copyErr := io.Copy(out, io.LimitReader(in, MaxFileBytes+1))
		inErr := in.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if inErr != nil {
			return inErr
		}
		if n > MaxFileBytes {
			return fmt.Errorf("文件超过 200 MB: %s", rel)
		}
		return closeErr
	})
}

func listResources(root string) ([]StoredResource, error) {
	result := []StoredResource{}
	base := filepath.Join(root, "resources")
	err := filepath.WalkDir(base, func(path string, e fs.DirEntry, err error) error {
		if os.IsNotExist(err) && path == base {
			return nil
		}
		if err != nil {
			return err
		}
		if e.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("不接受资源符号链接")
		}
		if e.IsDir() {
			return nil
		}
		i, err := e.Info()
		if err != nil {
			return err
		}
		if !i.Mode().IsRegular() {
			return fmt.Errorf("不接受特殊资源文件")
		}
		rel, _ := filepath.Rel(root, path)
		result = append(result, StoredResource{Path: filepath.ToSlash(rel), Type: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."), Size: i.Size(), Exists: true})
		return nil
	})
	return result, err
}
