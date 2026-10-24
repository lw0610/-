package flow

import (
	"context"
	"fmt"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/resource"
)

// ResolveInputResources always clones the plan: an input choice belongs to one
// run and must never enter the shared linked-plan cache.
func ResolveInputResources(plan *FlowPlan, values map[string]any) *FlowPlan {
	result := cloneFlowPlan(plan)
	if result == nil {
		return nil
	}
	library := resource.NewLibrary("", "", nil)
	associated := false
	for i, item := range result.Resources {
		if item.BaseDirFromInput == "" {
			continue
		}
		associated = true
		value, _ := values[item.BaseDirFromInput].(string)
		if value == "" {
			continue
		}
		resolved := library.Resolve(context.Background(), resource.Ref{Type: item.Type, Name: item.Label, BaseDirFromInput: item.BaseDirFromInput, BaseDirOverride: value}, resource.Scope{FlowID: plan.FlowID, FlowDir: plan.FlowDir, Linked: []*resource.ResolvedResource{item.ResolvedResource()}})
		result.Resources[i] = ResourcePlanFromResolved(resolved)
		result.Resources[i].EntryCount = len(resolved.Entries)
	}
	if !associated {
		return result
	}
	result.LinkDiagnostics = []Diagnostic{}
	resources := []*resource.ResolvedResource{}
	for _, r := range result.Resources {
		resources = append(resources, r.ResolvedResource())
		result.LinkDiagnostics = append(result.LinkDiagnostics, r.Problems...)
	}
	if fingerprint, err := resourceLinkFingerprint(context.Background(), result.CompileKey, result.Lock, resources); err == nil {
		result.LinkFingerprint = fingerprint
	} else {
		result.LinkFingerprint = ""
		result.LinkDiagnostics = append(result.LinkDiagnostics, Diagnostic{Level: "error", Code: "FLOW_RESOURCE_CHECK_FAILED", Message: "无法确认资源文件状态：" + err.Error()})
	}
	result.LinkDiagnostics = append(result.LinkDiagnostics, executorResourceDiagnostics(result)...)
	result.Diagnostics = append(append([]Diagnostic{}, result.CompileDiagnostics...), result.LinkDiagnostics...)
	return result
}

func (s *Store) ConvertResourceToInput(id, key string, library *resource.Library, memory *InputMemory) (*StoredFlow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.get(id)
	if err != nil {
		return nil, err
	}
	if item.Source != "user" {
		return nil, ErrReadOnly
	}
	doc := Parse(item.FlowMD)
	lock, err := ReadLock(item.Dir)
	if err != nil {
		return nil, err
	}
	var selected *ResourceDoc
	var resolved *resource.ResolvedResource
	for i := range doc.Resources {
		d := &doc.Resources[i]
		if d.Type != "data_list" {
			continue
		}
		r := library.Resolve(context.Background(), resource.Ref{Type: d.Type, Name: d.Label, Location: d.Location}, resource.Scope{FlowID: id, FlowDir: item.Dir, Lock: lock})
		if r.Key == key || r.ID == key || d.Label == key {
			selected = d
			resolved = r
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("未找到数据清单声明")
	}
	label := selected.FolderInput
	updated := item
	if label == "" {
		label = selected.Label + "所在文件夹"
		for _, input := range doc.Inputs {
			if input.Label == label {
				return nil, fmt.Errorf("输入名称已存在，请手动选择关联的文件夹输入")
			}
		}
		raw := appendFolderInput(item.FlowMD, *selected, label)
		updated, err = s.write(raw, id, item.Dir, true)
		if err != nil {
			return nil, err
		}
	}
	// The FLOW.md edit is a durable migration intent. If the following local
	// migration fails, another repair (or startup migration) can safely finish it.
	validMemoryInput := false
	for _, input := range baseInputs(Parse(updated.FlowMD).Inputs) {
		if input.Label == label && input.Type == "folder" && input.Remember {
			validMemoryInput = true
			break
		}
	}
	if !validMemoryInput {
		return nil, fmt.Errorf("关联的输入必须是允许记住的文件夹输入")
	}
	if memory != nil && s.Bindings != nil && resolved.BindingKey != "" {
		snapshot, err := s.Bindings.Snapshot()
		if err != nil {
			return nil, err
		}
		value, found := snapshot.Bindings[resolved.BindingKey]
		if !found {
			value, found = snapshot.Orphaned[resolved.BindingKey]
		}
		if found {
			absolute, err := config.ResolveProjectPath(value)
			if err != nil {
				return nil, err
			}
			err = s.Bindings.Consume(map[string]string{resolved.BindingKey: value}, func() error {
				return memory.mergeMigrated(map[string]map[string]string{id: {label: absolute}})
			})
			if err != nil {
				return nil, err
			}
		}
	}
	return updated, nil
}

func appendFolderInput(raw string, declaration ResourceDoc, label string) string {
	lines := strings.Split(normalizeMarkdown(raw), "\n")
	lines[declaration.Line-1] += "（文件所在文件夹：「" + label + "」）"
	// Another accepted input heading need not be renamed. A fresh section is safe
	// when the flow does not declare any inputs yet.
	insert := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "## ") && sectionAliases[normalizeName(strings.TrimPrefix(line, "## "))] == "inputs" {
			insert = i + 1
			for insert < len(lines) && !strings.HasPrefix(lines[insert], "## ") {
				insert++
			}
			break
		}
	}
	inputLine := "- " + label + "（必填，文件夹）：" + declaration.Label + "的文件所在文件夹"
	if insert < 0 {
		lines = append(lines, "", "## 需要准备", inputLine)
	} else {
		tail := append([]string{}, lines[insert:]...)
		lines = append(lines[:insert], inputLine, "")
		lines = append(lines, tail...)
	}
	return strings.Join(lines, "\n")
}
