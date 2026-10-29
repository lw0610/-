package flow

import (
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type DataMemoryEntry struct {
	Guide     string    `json:"guide,omitempty" yaml:"guide,omitempty"`
	Name      string    `json:"name" yaml:"name"`
	Category  string    `json:"category,omitempty" yaml:"category,omitempty"`
	Extent    string    `json:"extent,omitempty" yaml:"extent,omitempty"`
	Path      string    `json:"path,omitempty" yaml:"path,omitempty"`
	SourceID  string    `json:"source_id,omitempty" yaml:"source_id,omitempty"`
	UpdatedAt time.Time `json:"updated_at" yaml:"updated_at"`
}
type dataMemoryDocument struct {
	APIVersion string            `yaml:"apiVersion"`
	Entries    []DataMemoryEntry `yaml:"entries"`
}
type DataMemory struct {
	Path string
	mu   *sync.Mutex
}

func NewDataMemory(path string) *DataMemory {
	path = filepath.Clean(path)
	lock, _ := inputMemoryLocks.LoadOrStore(path, &sync.Mutex{})
	return &DataMemory{Path: path, mu: lock.(*sync.Mutex)}
}
func DefaultDataMemory() (*DataMemory, error) {
	home, err := config.ResolveAutoCartoHome()
	if err != nil {
		return nil, err
	}
	return NewDataMemory(filepath.Join(home, "config", "data_memory.yaml")), nil
}
func (m *DataMemory) load() ([]DataMemoryEntry, error) {
	if err := bindings.RejectSymlinks(m.Path); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(m.Path)
	if os.IsNotExist(err) {
		return []DataMemoryEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var doc dataMemoryDocument
	if err = yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc.APIVersion != "data-memory/v1" {
		return nil, fmt.Errorf("数据记忆版本不支持")
	}
	return doc.Entries, nil
}
func (m *DataMemory) save(entries []DataMemoryEntry) error {
	raw, err := yaml.Marshal(dataMemoryDocument{APIVersion: "data-memory/v1", Entries: entries})
	if err != nil {
		return err
	}
	if err = bindings.RejectSymlinks(m.Path); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(m.Path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(m.Path), ".data-memory-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), m.Path)
}
func (m *DataMemory) List() ([]DataMemoryEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.load()
}
func memoryIdentity(e DataMemoryEntry) string {
	if e.Guide != "" {
		return "guide:" + e.Guide + "\x00" + e.Name
	}
	return "category:" + e.Category + "\x00" + e.Extent + "\x00" + e.Name
}
func (m *DataMemory) Remember(e DataMemoryEntry) error {
	if strings.TrimSpace(e.Name) == "" || (e.Guide == "" && e.Category == "") {
		return fmt.Errorf("数据记忆缺少指南/类别与数据名称")
	}
	if e.Guide != "" {
		if err := ValidateID(e.Guide); err != nil {
			return err
		}
	}
	if e.Path != "" {
		p, err := memoryValue(e.Path)
		if err != nil {
			return err
		}
		e.Path = p
	}
	if e.Path == "" && e.SourceID == "" {
		return fmt.Errorf("只记住用户路径或明确选择的来源")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entries, err := m.load()
	if err != nil {
		return err
	}
	e.UpdatedAt = time.Now().UTC()
	out := []DataMemoryEntry{}
	for _, old := range entries {
		if memoryIdentity(old) != memoryIdentity(e) {
			out = append(out, old)
		}
	}
	return m.save(append(out, e))
}
func (m *DataMemory) Forget(key DataMemoryEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries, err := m.load()
	if err != nil {
		return err
	}
	out := []DataMemoryEntry{}
	for _, e := range entries {
		if memoryIdentity(e) != memoryIdentity(key) {
			out = append(out, e)
		}
	}
	return m.save(out)
}

// MigrateLegacy is startup-only, preserves exact legacy bytes before publishing
// the new file, and is idempotent. The legacy file remains available read-only.
func (m *DataMemory) MigrateLegacy(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := os.Stat(m.Path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	old := NewInputMemory(path)
	old.mu.Lock()
	defer old.mu.Unlock()
	doc, raw, err := old.load()
	if err != nil || len(raw) == 0 {
		return err
	}
	if err = bindings.RejectSymlinks(path + ".v5.bak"); err != nil {
		return err
	}
	f, err := os.OpenFile(path+".v5.bak", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err == nil {
		_, err = f.Write(raw)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
	} else if os.IsExist(err) {
		err = nil
	}
	if err != nil {
		return err
	}
	entries := map[string]DataMemoryEntry{}
	for guide, values := range doc.Flows {
		for name, path := range values {
			e := DataMemoryEntry{Guide: guide, Name: name, Path: path}
			entries[memoryIdentity(e)] = e
		}
	}
	// Legacy source choices are migrated as explicit preferences, never as values.
	for guide, values := range doc.Choices {
		for name, choice := range values {
			e := DataMemoryEntry{Guide: guide, Name: name}
			e.Path, _ = choice["path"].(string)
			e.SourceID, _ = choice["source"].(string)
			if e.Path != "" || e.SourceID != "" {
				entries[memoryIdentity(e)] = e
			}
		}
	}
	keys := []string{}
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []DataMemoryEntry{}
	for _, k := range keys {
		out = append(out, entries[k])
	}
	return m.save(out)
}

// Snapshot returns a detached view for deterministic request processing.
func (m *DataMemory) Snapshot() ([]DataMemoryEntry, error) {
	entries, err := m.List()
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(entries)
	var out []DataMemoryEntry
	err = json.Unmarshal(raw, &out)
	return out, err
}
