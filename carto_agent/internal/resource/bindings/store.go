// Package bindings 统一读写流程和共享资源的数据基准目录绑定。
package bindings

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"gopkg.in/yaml.v3"
)

var locks sync.Map
var resourceID = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z0-9][a-z0-9.-]*$`)
var legacyID = regexp.MustCompile(`^legacy\.[a-z][a-z0-9_]*\.[a-f0-9]{10}$`)
var flowID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)

type Store struct {
	path      string
	mu        *sync.Mutex
	migration map[string][]string
}

func New(path string) *Store {
	if absolute, err := config.ResolveProjectPath(path); err == nil {
		path = absolute
	}
	key := filepath.Clean(path)
	lock, _ := locks.LoadOrStore(key, &sync.Mutex{})
	return &Store{path: key, mu: lock.(*sync.Mutex)}
}

const APIVersion = "resource-bindings/v2"

type Document struct {
	APIVersion string            `yaml:"apiVersion"`
	Bindings   map[string]string `yaml:"bindings"`
	Orphaned   map[string]string `yaml:"orphaned"`
}

func validResourceID(id string) bool {
	return len(id) <= 64 && (resourceID.MatchString(id) || legacyID.MatchString(id))
}

func ValidateKey(key string) error {
	if strings.TrimSpace(key) != key || strings.ContainsAny(key, "\\\x00\r\n") {
		return fmt.Errorf("资源绑定键无效")
	}
	if strings.HasPrefix(key, "library:") {
		if validResourceID(strings.TrimPrefix(key, "library:")) {
			return nil
		}
	} else if strings.HasPrefix(key, "flow:") {
		parts := strings.SplitN(strings.TrimPrefix(key, "flow:"), "/", 2)
		if len(parts) == 2 && flowID.MatchString(parts[0]) {
			scoped := parts[1]
			if validResourceID(scoped) {
				return nil
			}
			if strings.HasPrefix(scoped, "library:") && validResourceID(strings.TrimPrefix(scoped, "library:")) {
				return nil
			}
			if strings.HasPrefix(scoped, "path:") {
				relative := strings.TrimPrefix(scoped, "path:")
				if relative != "" && relative != "." && !strings.HasPrefix(relative, "/") && relative != ".." && !strings.HasPrefix(relative, "../") && !strings.Contains(relative, ":") && path.Clean(relative) == relative {
					return nil
				}
			}
		}
	}
	return fmt.Errorf("资源绑定键须为资源编号或流程内 resources/ 相对路径")
}

// load keeps the original bytes until the first successful write, so a v1
// read (including startup diagnostics) never mutates users' configuration.
func (s *Store) load() (Document, []byte, error) {
	document := Document{APIVersion: APIVersion, Bindings: map[string]string{}, Orphaned: map[string]string{}}
	if err := RejectSymlinks(s.path); err != nil {
		return document, nil, err
	}
	raw, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return document, nil, nil
	}
	if err != nil {
		return document, nil, err
	}
	document.APIVersion = ""
	if err = yaml.Unmarshal(raw, &document); err != nil {
		return document, nil, fmt.Errorf("资源绑定配置无法解析：%v", err)
	}
	if document.Bindings == nil {
		document.Bindings = map[string]string{}
	}
	if document.Orphaned == nil {
		document.Orphaned = map[string]string{}
	}
	if document.APIVersion == "" {
		migrated, err := migrateV1(document, s.migration)
		return migrated, raw, err
	}
	if document.APIVersion != APIVersion {
		return document, nil, fmt.Errorf("不支持的资源绑定版本：%s", document.APIVersion)
	}
	return document, nil, nil
}

func (s *Store) Snapshot() (Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	document, _, err := s.load()
	return document, err
}

func (s *Store) Get(key string) (string, bool, error) {
	if err := ValidateKey(key); err != nil {
		return "", false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	document, _, err := s.load()
	if err != nil {
		return "", false, err
	}
	value, found := document.Bindings[key]
	if !found || strings.TrimSpace(value) == "" {
		return "", false, nil
	}
	resolved, err := config.ResolveProjectPath(value)
	return resolved, true, err
}

func (s *Store) Set(key, baseDir string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	stored := ""
	if strings.TrimSpace(baseDir) != "" {
		resolved, err := config.ResolveProjectPath(baseDir)
		if err != nil {
			return err
		}
		if err = RejectSymlinks(resolved); err != nil {
			return err
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return fmt.Errorf("无法读取基准目录：%v", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("基准目录必须是文件夹")
		}
		stored, err = config.PreferProjectRelativePath(resolved)
		if err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	document, original, err := s.load()
	if err != nil {
		return err
	}
	if stored == "" {
		delete(document.Bindings, key)
	} else {
		document.Bindings[key] = stored
	}
	return s.save(document, original)
}

func (s *Store) save(document Document, original []byte) error {
	raw, err := yaml.Marshal(document)
	if err != nil {
		return err
	}
	if err = RejectSymlinks(filepath.Dir(s.path)); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	if err = RejectSymlinks(s.path); err != nil {
		return err
	}
	if original != nil {
		if err := s.backup(original); err != nil {
			return err
		}
	}
	file, err := os.CreateTemp(filepath.Dir(s.path), ".resource-bindings-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), s.path)
}

// backup uses an exclusive file name and never overwrites an earlier backup.
func (s *Store) backup(raw []byte) error {
	dir := filepath.Join(filepath.Dir(s.path), "backups")
	if err := RejectSymlinks(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, "resource_bindings-"+time.Now().UTC().Format("20060102T150405.000000000")+"-*.yaml")
	if err != nil {
		return err
	}
	name := file.Name()
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(name)
	}
	return err
}

// RejectSymlinks 同时检查路径和祖先目录；允许尚未创建的普通目录。
func RejectSymlinks(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("资源内部路径必须是绝对路径")
	}
	for {
		info, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 && !(runtime.GOOS == "darwin" && (path == "/var" || path == "/tmp")) {
			return fmt.Errorf("不接受符号链接：%s", path)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

// RenameKeys changes identities in one write and preserves an already explicit
// target binding. Values stay normalized and unreadable directories stay intact.
func (s *Store) RenameKeys(mapping map[string]string) error {
	for old, next := range mapping {
		if err := ValidateKey(old); err != nil {
			return err
		}
		if err := ValidateKey(next); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	document, original, err := s.load()
	if err != nil {
		return err
	}
	changed := false
	for old, next := range mapping {
		if old == next {
			continue
		}
		if value, ok := document.Bindings[old]; ok {
			if _, exists := document.Bindings[next]; !exists {
				document.Bindings[next] = value
			}
			delete(document.Bindings, old)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.save(document, original)
}
