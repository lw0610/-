// Package sessionfiles maintains a local index of session artifacts.
package sessionfiles

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
)

type Origin struct {
	Type     string `json:"type"`
	Tool     string `json:"tool,omitempty"`
	Label    string `json:"label,omitempty"`
	Name     string `json:"name,omitempty"`
	CallID   string `json:"call_id,omitempty"`
	Agent    string `json:"agent,omitempty"`
	SourceID string `json:"source_id,omitempty"`
	License  string `json:"license,omitempty"`
}
type Item struct {
	ID           string    `json:"id"`
	Path         string    `json:"path"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind"`
	Group        string    `json:"group"`
	Members      []string  `json:"members"`
	Origin       Origin    `json:"origin"`
	FlowRunID    string    `json:"flow_run_id,omitempty"`
	StepID       string    `json:"step_id,omitempty"`
	RegisteredAt time.Time `json:"registered_at"`
	ModifiedAt   time.Time `json:"modified_at"`
	Size         int64     `json:"size"`
	Exists       bool      `json:"exists"`
}
type Change struct {
	SessionID string `json:"session_id"`
	Added     int    `json:"added"`
	Updated   int    `json:"updated"`
}
type RegistrationOption func(*Item)

func WithGroup(group string) RegistrationOption { return func(i *Item) { i.Group = group } }
func WithFlow(runID, stepID string) RegistrationOption {
	return func(i *Item) { i.FlowRunID = runID; i.StepID = stepID }
}

type index struct {
	Version int    `json:"version"`
	Items   []Item `json:"items"`
}
type sessionLock struct {
	sync.Mutex
	notify func(Change)
	timer  *time.Timer
	change Change
}

var locks sync.Map

type Store struct {
	SessionID, Dir string
	lock           *sessionLock
}

var ErrNotIndexed = errors.New("文件不在本会话文件索引中")

func New(projectPath, sessionID string) (*Store, error) {
	dir, err := config.ResolveSessionDir(projectPath, sessionID)
	if err != nil {
		return nil, err
	}
	return NewAt(sessionID, dir)
}

// NewAt opens the artifact index for a session directory.
func NewAt(sessionID, dir string) (*Store, error) {
	if _, err := config.ValidateSessionID(sessionID); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	value, _ := locks.LoadOrStore(absolute, &sessionLock{})
	return &Store{SessionID: sessionID, Dir: absolute, lock: value.(*sessionLock)}, nil
}
func (s *Store) OnChange(fn func(Change)) { s.lock.Lock(); defer s.lock.Unlock(); s.lock.notify = fn }
func (s *Store) changed(added, updated int) {
	s.lock.change.SessionID = s.SessionID
	s.lock.change.Added += added
	s.lock.change.Updated += updated
	if s.lock.timer != nil {
		s.lock.timer.Stop()
	}
	s.lock.timer = time.AfterFunc(500*time.Millisecond, func() {
		s.lock.Lock()
		event, fn := s.lock.change, s.lock.notify
		s.lock.change = Change{}
		s.lock.timer = nil
		s.lock.Unlock()
		if fn != nil {
			fn(event)
		}
	})
}
func pathID(path string) string {
	sum := sha256.Sum256([]byte(path))
	return "f_" + hex.EncodeToString(sum[:8])
}
func within(root, path string) bool {
	rel, e := filepath.Rel(root, path)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func (s *Store) ValidatePath(path string) (string, fs.FileInfo, error) {
	if !filepath.IsAbs(path) {
		return "", nil, fmt.Errorf("文件路径必须为绝对路径")
	}
	target, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", nil, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return "", nil, fmt.Errorf("只支持普通文件和目录")
	}
	return target, info, nil
}
func (s *Store) read() (index, error) {
	result := index{Version: 1, Items: []Item{}}
	filename := filepath.Join(s.Dir, "files.index.json")
	if _, err := os.Lstat(filename); os.IsNotExist(err) {
		return result, nil
	} else if err != nil {
		return result, err
	}
	target, info, err := s.ValidatePath(filename)
	if err != nil {
		return result, err
	}
	canonicalDir, e := filepath.EvalSymlinks(s.Dir)
	if e != nil || !within(canonicalDir, target) || info.Size() > 32<<20 {
		return result, fmt.Errorf("会话文件索引无效")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return result, err
	}
	if json.Unmarshal(data, &result) != nil || result.Version != 1 {
		backup := filename + ".corrupt-" + time.Now().UTC().Format("20060102T150405.000000000")
		if err = os.Rename(filename, backup); err != nil {
			return result, err
		}
		return index{Version: 1, Items: []Item{}}, nil
	}
	if result.Items == nil {
		result.Items = []Item{}
	}
	return result, nil
}
func (s *Store) write(data index) error {
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	// The index is application metadata, stored directly in the session directory.
	filename := filepath.Join(s.Dir, "files.index.json")
	if info, e := os.Lstat(filename); e == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("会话文件索引不能是符号链接")
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.Dir, ".files-index-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, filename)
}
func (s *Store) makeItem(path string, origin Origin) (Item, error) {
	canonical, info, err := s.ValidatePath(path)
	if err != nil {
		return Item{}, err
	}
	kind, group := s.classifyPath(canonical, info.IsDir())
	return Item{ID: pathID(canonical), Path: canonical, Name: displayName(info.Name(), origin), Kind: kind, Group: group, Members: []string{}, Origin: origin, RegisteredAt: info.ModTime().UTC(), ModifiedAt: info.ModTime().UTC(), Size: info.Size(), Exists: true}, nil
}

// Tool labels describe actions; only an explicit artifact name or a data-service
// row names the file's contents. Keep the real path and extension unchanged.
func displayName(filename string, origin Origin) string {
	name := strings.TrimSpace(origin.Name)
	if name == "" && origin.Type == "data_service" {
		name = strings.TrimSpace(origin.Label)
	}
	if name == "" {
		return filename
	}
	ext := filepath.Ext(filename)
	if !strings.EqualFold(filepath.Ext(name), ext) {
		name += ext
	}
	return name
}
func (s *Store) classifyPath(path string, directory bool) (string, string) {
	root := s.Dir
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return classify(path, root, directory)
}
func (s *Store) Register(path string, origin Origin, options ...RegistrationOption) (Item, error) {
	return s.register(path, origin, false, options...)
}

// registerMissing recovers an omitted artifact without replacing the provenance
// or stable identity of an already indexed (possibly since replaced) file.
func (s *Store) registerMissing(path string, origin Origin, options ...RegistrationOption) (Item, error) {
	return s.register(path, origin, true, options...)
}

func (s *Store) register(path string, origin Origin, missingOnly bool, options ...RegistrationOption) (Item, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	item, err := s.makeItem(path, origin)
	if err != nil {
		return item, err
	}
	item.RegisteredAt = time.Now().UTC()
	classifiedGroup := item.Group
	for _, option := range options {
		option(&item)
	}
	item.Group = preferredGroup(classifiedGroup, item.Group)
	data, err := s.read()
	if err != nil {
		return item, err
	}
	added, updated := 1, 0
	found := false
	for i, prior := range data.Items {
		if prior.ID == item.ID {
			if missingOnly {
				// Fill an old registration's missing business name from its own
				// receipt, without reverting a later replacement's provenance.
				if prior.Origin.Name == "" && item.Origin.Name != "" && prior.Origin.CallID == item.Origin.CallID && prior.Origin.Tool == item.Origin.Tool {
					prior.Origin.Name = item.Origin.Name
					prior.Name = displayName(filepath.Base(prior.Path), prior.Origin)
					data.Items[i] = prior
					if err := s.write(data); err != nil {
						return prior, err
					}
					s.changed(0, 1)
				}
				return prior, nil
			}
			data.Items[i] = item
			found = true
			added, updated = 0, 1
			break
		}
	}
	if !found {
		data.Items = append(data.Items, item)
	}
	if err = s.write(data); err != nil {
		return item, err
	}
	s.changed(added, updated)
	return item, nil
}
func excluded(name string) bool {
	return name == "session.json" || name == "runtime_snapshot.json" || name == "files.index.json" || strings.Contains(name, "event_sequence") || strings.HasSuffix(name, ".lock") || strings.HasPrefix(name, ".")
}
func (s *Store) List(all bool) ([]Item, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	data, err := s.read()
	if err != nil {
		return nil, err
	}
	items := map[string]Item{}
	for _, item := range data.Items {
		current, e := s.makeItem(item.Path, item.Origin)
		if e != nil {
			continue
		}
		current.RegisteredAt = item.RegisteredAt
		current.FlowRunID = item.FlowRunID
		current.StepID = item.StepID
		current.Group = preferredGroup(current.Group, item.Group)
		items[current.Path] = current
	}
	for _, subdir := range []string{"exports", "output", "intermediate", "flows", "logs", "temp"} {
		root := filepath.Join(s.Dir, subdir)
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if excluded(entry.Name()) || entry.Name() == "snapshots" {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			item, e := s.makeItem(path, Origin{Type: "scan", Label: "会话文件"})
			if e != nil {
				return nil
			}
			if _, exists := items[item.Path]; !exists {
				items[item.Path] = item
			}
			return nil
		})
	}
	// Retain the complete index, including sidecars and hidden-by-default groups.
	data.Items = make([]Item, 0, len(items))
	for _, item := range items {
		data.Items = append(data.Items, item)
	}
	sort.Slice(data.Items, func(i, j int) bool { return data.Items[i].Path < data.Items[j].Path })
	if err = s.write(data); err != nil {
		return nil, err
	}
	result := mergeMembers(data.Items)
	visible := make([]Item, 0, len(result))
	for _, item := range result {
		if all || item.Group != "other" {
			visible = append(visible, item)
		}
	}
	sort.Slice(visible, func(i, j int) bool { return visible[i].ModifiedAt.After(visible[j].ModifiedAt) })
	return visible, nil
}

// Get never scans: only an already indexed identity may be used by content/actions.
func (s *Store) Get(id string) (Item, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	data, err := s.read()
	if err != nil {
		return Item{}, err
	}
	for _, item := range data.Items {
		if item.ID != id {
			continue
		}
		path, info, e := s.ValidatePath(item.Path)
		if e != nil {
			return Item{}, e
		}
		if pathID(path) != id {
			return Item{}, fmt.Errorf("文件引用已变化，请刷新文件列表")
		}
		item.Path = path
		item.Name = displayName(info.Name(), item.Origin)
		item.Size = info.Size()
		item.ModifiedAt = info.ModTime()
		item.Exists = true
		_, group := s.classifyPath(path, info.IsDir())
		item.Group = preferredGroup(group, item.Group)
		return item, nil
	}
	return Item{}, ErrNotIndexed
}

// GetPath resolves an exact indexed file without registering it.
func (s *Store) GetPath(path string) (Item, error) {
	canonical, _, err := s.ValidatePath(path)
	if err != nil {
		return Item{}, err
	}
	return s.Get(pathID(canonical))
}
