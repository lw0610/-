package flow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"gopkg.in/yaml.v3"
)

const InputMemoryAPIVersion = "flow-input-memory/v1"

var inputMemoryLocks sync.Map

type inputMemoryDocument struct {
	APIVersion string                       `yaml:"apiVersion"`
	Flows      map[string]map[string]string `yaml:"flows"`
}

// InputMemory persists only local file/folder choices, never flow package data.
// Instances referring to the same file share a process-wide lock.
type InputMemory struct {
	path string
	mu   *sync.Mutex
}

func NewInputMemory(path string) *InputMemory {
	if absolute, err := config.ResolveProjectPath(path); err == nil {
		path = absolute
	}
	path = filepath.Clean(path)
	lock, _ := inputMemoryLocks.LoadOrStore(path, &sync.Mutex{})
	return &InputMemory{path: path, mu: lock.(*sync.Mutex)}
}

func DefaultInputMemory() (*InputMemory, error) {
	home, err := config.ResolveAutoCartoHome()
	if err != nil {
		return nil, err
	}
	return NewInputMemory(filepath.Join(home, "config", "flow_input_memory.yaml")), nil
}

func memoryLabel(label string) error {
	if label == "" || label != strings.TrimSpace(label) || strings.ContainsAny(label, "\x00\r\n") {
		return fmt.Errorf("输入名称无效")
	}
	return nil
}

func memoryValue(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if !filepath.IsAbs(value) || strings.ContainsAny(value, "\x00\r\n") {
		return "", fmt.Errorf("记住的输入值必须为绝对路径")
	}
	return filepath.Clean(value), nil
}

func (m *InputMemory) load() (inputMemoryDocument, []byte, error) {
	doc := inputMemoryDocument{APIVersion: InputMemoryAPIVersion, Flows: map[string]map[string]string{}}
	if err := bindings.RejectSymlinks(m.path); err != nil {
		return doc, nil, err
	}
	raw, err := os.ReadFile(m.path)
	if os.IsNotExist(err) {
		return doc, nil, nil
	}
	if err != nil {
		return doc, nil, err
	}
	doc.APIVersion = ""
	if err = yaml.Unmarshal(raw, &doc); err != nil {
		return doc, nil, fmt.Errorf("输入记忆配置无法解析：%w", err)
	}
	if doc.APIVersion != InputMemoryAPIVersion {
		return doc, nil, fmt.Errorf("不支持的输入记忆版本：%s", doc.APIVersion)
	}
	if doc.Flows == nil {
		doc.Flows = map[string]map[string]string{}
	}
	for id, values := range doc.Flows {
		if err = ValidateID(id); err != nil {
			return doc, nil, err
		}
		for label, value := range values {
			if err = memoryLabel(label); err != nil {
				return doc, nil, err
			}
			if _, err = memoryValue(value); err != nil {
				return doc, nil, err
			}
		}
	}
	return doc, raw, nil
}

func (m *InputMemory) save(doc inputMemoryDocument) error {
	raw, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	if err = bindings.RejectSymlinks(m.path); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(m.path), ".flow-input-memory-")
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
	return os.Rename(file.Name(), m.path)
}

func (m *InputMemory) Get(flowID string) (map[string]string, error) {
	if err := ValidateID(flowID); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, _, err := m.load()
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for label, value := range doc.Flows[flowID] {
		values[label] = value
	}
	return values, nil
}

func (m *InputMemory) Set(flowID, label, value string) error {
	return m.setValues(flowID, map[string]string{label: value})
}

func (m *InputMemory) setValues(flowID string, values map[string]string) error {
	if err := ValidateID(flowID); err != nil {
		return err
	}
	for label, value := range values {
		if err := memoryLabel(label); err != nil {
			return err
		}
		normalized, err := memoryValue(value)
		if err != nil {
			return err
		}
		values[label] = normalized
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, _, err := m.load()
	if err != nil {
		return err
	}
	changed := false
	for label, value := range values {
		if doc.Flows[flowID][label] == value {
			continue
		}
		if value == "" {
			delete(doc.Flows[flowID], label)
		} else {
			if doc.Flows[flowID] == nil {
				doc.Flows[flowID] = map[string]string{}
			}
			doc.Flows[flowID][label] = value
		}
		changed = true
	}
	if len(doc.Flows[flowID]) == 0 {
		delete(doc.Flows, flowID)
	}
	if !changed {
		return nil
	}
	return m.save(doc)
}

func (m *InputMemory) DeleteFlow(flowID string) error {
	if err := ValidateID(flowID); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, _, err := m.load()
	if err != nil {
		return err
	}
	if _, exists := doc.Flows[flowID]; !exists {
		return nil
	}
	delete(doc.Flows, flowID)
	return m.save(doc)
}

func (m *InputMemory) Remember(flowID string, inputs []InputPlan, values map[string]any) error {
	remember := map[string]string{}
	for _, input := range inputs {
		if input.Type != "file" && input.Type != "folder" {
			continue
		}
		label := input.Label
		if label == "" {
			label = input.Key
		}
		if !input.Remember || values[input.Key] == nil {
			remember[label] = ""
			continue
		}
		value, ok := values[input.Key].(string)
		if !ok {
			return fmt.Errorf("输入「%s」必须为文件或文件夹路径", label)
		}
		if strings.TrimSpace(value) == "" {
			remember[label] = ""
			continue
		}
		resolved, err := config.ResolveProjectPath(value)
		if err != nil {
			return err
		}
		remember[label] = resolved
	}
	return m.setValues(flowID, remember)
}

func (m *InputMemory) backup(raw []byte) error {
	if raw == nil {
		return nil // No previous file exists to back up.
	}
	dir := filepath.Join(filepath.Dir(m.path), "backups")
	if err := bindings.RejectSymlinks(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, "flow_input_memory-"+time.Now().UTC().Format("20060102T150405.000000000")+"-*.yaml")
	if err != nil {
		return err
	}
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(file.Name())
	}
	return err
}

// mergeMigrated never overwrites a newer user choice. It backs up the exact
// previous bytes before its first write; retrying after a crash is safe.
func (m *InputMemory) mergeMigrated(values map[string]map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, raw, err := m.load()
	if err != nil {
		return err
	}
	changed := false
	for flowID, inputs := range values {
		for label, value := range inputs {
			if existing, exists := doc.Flows[flowID][label]; exists && existing != "" {
				continue
			}
			if doc.Flows[flowID] == nil {
				doc.Flows[flowID] = map[string]string{}
			}
			doc.Flows[flowID][label] = value
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if err = m.backup(raw); err != nil {
		return err
	}
	return m.save(doc)
}
