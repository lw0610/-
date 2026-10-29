package plan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrVersionConflict = errors.New("PLAN_VERSION_CONFLICT: 计划已更新，请刷新后重试")
var storeLocks sync.Map

type Store struct {
	Dir string
	mu  *sync.Mutex
}

func NewStore(sessionDir string) (*Store, error) {
	if !filepath.IsAbs(sessionDir) {
		return nil, fmt.Errorf("会话目录必须为绝对路径")
	}
	dir := filepath.Join(filepath.Clean(sessionDir), "plans")
	lock, _ := storeLocks.LoadOrStore(dir, &sync.Mutex{})
	return &Store{Dir: dir, mu: lock.(*sync.Mutex)}, nil
}
func (s *Store) path(id string) (string, error) {
	if !Identifier.MatchString(id) {
		return "", fmt.Errorf("PLAN_SCHEMA_INVALID: 计划编号无效")
	}
	path := filepath.Join(s.Dir, id+".json")
	if err := rejectSymlink(s.Dir); err != nil {
		return "", err
	}
	if err := rejectSymlink(path); err != nil {
		return "", err
	}
	return path, nil
}
func (s *Store) Load(id string) (*Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(id)
}
func (s *Store) load(id string) (*Snapshot, error) {
	p, err := s.path(id)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var snapshot Snapshot
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		return nil, err
	}
	if snapshot.Plan == nil || snapshot.Plan.ID != id {
		return nil, fmt.Errorf("计划记录损坏")
	}
	return &snapshot, nil
}

// Save uses optimistic versioning. expected=0 creates, n replaces version n with n+1.
// State transitions use UpdateStatus and cannot rewrite the immutable plan history.
func (s *Store) Save(next *Snapshot, expected int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if next == nil || next.Plan == nil {
		return fmt.Errorf("计划为空")
	}
	path, err := s.path(next.Plan.ID)
	if err != nil {
		return err
	}
	prior, err := s.load(next.Plan.ID)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if (prior == nil && expected != 0) || (prior != nil && prior.Plan.Version != expected) || next.Plan.Version != expected+1 {
		return ErrVersionConflict
	}
	now := time.Now().UTC()
	next.PlanID = next.Plan.ID
	next.PlanVersion = next.Plan.Version
	next.UpdatedAt = now
	if prior == nil {
		next.CreatedAt = now
	} else {
		next.CreatedAt = prior.CreatedAt
	}
	if err = os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	history := filepath.Join(s.Dir, fmt.Sprintf("%s.v%d.json", next.Plan.ID, next.Plan.Version))
	// A version can only be written once. An orphan from an interrupted latest
	// pointer write may be recovered iff its exact plan is unchanged.
	if err = rejectSymlink(history); err != nil {
		return err
	}
	if existing, e := os.ReadFile(history); e == nil {
		var old Snapshot
		if json.Unmarshal(existing, &old) != nil || !samePlan(old.Plan, next.Plan) {
			return ErrVersionConflict
		}
	} else if !os.IsNotExist(e) {
		return e
	} else if e = writeAtomic(history, raw); e != nil {
		return e
	}
	return writeAtomic(path, raw)
}
func (s *Store) UpdateStatus(id string, version int, mutate func(*Snapshot) error) (*Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.load(id)
	if err != nil {
		return nil, err
	}
	if p.Plan.Version != version {
		return nil, ErrVersionConflict
	}
	before, _ := json.Marshal(p.Plan)
	if err = mutate(p); err != nil {
		return nil, err
	}
	after, _ := json.Marshal(p.Plan)
	if string(before) != string(after) {
		return nil, fmt.Errorf("状态更新不能修改计划")
	}
	p.UpdatedAt = time.Now().UTC()
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	path, _ := s.path(id)
	if err = writeAtomic(path, raw); err != nil {
		return nil, err
	}
	return p, nil
}
func (s *Store) List() ([]*Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.Dir)
	if os.IsNotExist(err) {
		return []*Snapshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := []*Snapshot{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		p, err := s.load(id)
		if err != nil {
			raw, e := os.ReadFile(filepath.Join(s.Dir, e.Name()))
			var historical Snapshot
			if e == nil && json.Unmarshal(raw, &historical) == nil && historical.Plan != nil && id == fmt.Sprintf("%s.v%d", historical.Plan.ID, historical.Plan.Version) {
				continue
			}
			return nil, err
		}
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, nil
}
func samePlan(a, b *Plan) bool {
	x, e := json.Marshal(a)
	y, f := json.Marshal(b)
	return e == nil && f == nil && string(x) == string(y)
}
func writeAtomic(path string, raw []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".plan-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

func rejectSymlink(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("计划存储不允许符号链接：%s", path)
	}
	return nil
}
