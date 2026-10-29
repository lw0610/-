package flow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow/legacy"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"gopkg.in/yaml.v3"
)

const InputMemoryAPIVersion = "flow-input-memory/v1"

var inputMemoryLocks sync.Map

type inputMemoryDocument struct {
	APIVersion string                               `yaml:"apiVersion"`
	Flows      map[string]map[string]string         `yaml:"flows"`
	Choices    map[string]map[string]map[string]any `yaml:"choices,omitempty"`
}

// InputMemory reads and migrates local choices from earlier releases. New runs use DataMemory.
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
	doc := inputMemoryDocument{APIVersion: InputMemoryAPIVersion, Flows: map[string]map[string]string{}, Choices: map[string]map[string]map[string]any{}}
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
	if doc.Choices == nil {
		doc.Choices = map[string]map[string]map[string]any{}
	}
	for id, choices := range doc.Choices {
		if err = ValidateID(id); err != nil {
			return doc, nil, err
		}
		for label, value := range choices {
			if err = memoryLabel(label); err != nil {
				return doc, nil, err
			}
			if _, err = memoryChoice(value); err != nil {
				return doc, nil, err
			}
		}
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
		if _, exists := doc.Choices[flowID][label]; exists {
			delete(doc.Choices[flowID], label)
			changed = true
		}
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
	if _, exists := doc.Flows[flowID]; !exists && len(doc.Choices[flowID]) == 0 {
		return nil
	}
	delete(doc.Choices, flowID)
	delete(doc.Flows, flowID)
	return m.save(doc)
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
			if _, exists := doc.Choices[flowID][label]; exists {
				continue
			}
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

// GetValues returns path choices and source-mode choices without rewriting old files.
func (m *InputMemory) GetValues(flowID string) (map[string]any, error) {
	if err := ValidateID(flowID); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, _, err := m.load()
	if err != nil {
		return nil, err
	}
	values := map[string]any{}
	for label, path := range doc.Flows[flowID] {
		values[label] = path
	}
	for label, choice := range doc.Choices[flowID] {
		normalized, err := memoryChoice(choice)
		if err != nil {
			return nil, err
		}
		values[label] = normalized
	}
	return values, nil
}
func memoryChoice(value any) (map[string]any, error) {
	choice, marked, err := legacy.ParseInputChoice(value)
	if err != nil {
		return nil, err
	}
	if !marked {
		return nil, fmt.Errorf("记忆的来源方式必须为自动获取或补齐")
	}
	if choice.Mode == "fill" {
		choice.Path, err = memoryValue(choice.Path)
		if err != nil {
			return nil, err
		}
	}
	for name, path := range choice.Files {
		choice.Files[name], err = memoryValue(path)
		if err != nil {
			return nil, err
		}
	}
	return choice.Value(), nil
}
