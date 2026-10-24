package flow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const enrichmentAPIVersion = "flow-enrichment/v1"

// DefaultEnrichmentRetryAfter 是非强制编译对失败增强的重试间隔，避免反复冲击模型。
const DefaultEnrichmentRetryAfter = 10 * time.Minute

const enrichmentKeepLimit = 200

// enrichmentRecord 是一次流程级或步骤级模型增强的结果；失败同样记录，供界面说明原因。
type enrichmentRecord struct {
	APIVersion      string          `json:"api_version"`
	Kind            string          `json:"kind"`
	Key             string          `json:"key"`
	FlowID          string          `json:"flow_id"`
	StepID          string          `json:"step_id,omitempty"`
	StepTitle       string          `json:"step_title,omitempty"`
	CompilerVersion string          `json:"compiler_version"`
	Model           string          `json:"model"`
	CreatedAt       time.Time       `json:"created_at"`
	DurationMS      int64           `json:"duration_ms"`
	Attempts        int             `json:"attempts"`
	Failed          bool            `json:"failed,omitempty"`
	Reason          string          `json:"reason,omitempty"`
	Result          json.RawMessage `json:"result,omitempty"`
}

type enrichmentMemo struct {
	size     int64
	modified time.Time
	record   *enrichmentRecord
}

// enrichmentUnit 是一项可单独缓存的增强：流程级一项，每个步骤一项。
// 内容键就是发给模型的完整请求加编译器版本与模型名，请求不变则结果可以复用。
type enrichmentUnit struct {
	Kind      string // flow | step
	Index     int    // doc.Steps 下标；流程级为 -1
	StepID    string
	StepTitle string
	Key       string
	Request   ModelRequest
	validate  func(string) (any, error)
}

var enrichLocks sync.Map

func (c *Compiler) cacheRoot() string {
	if c.options.CacheDir != "" {
		return c.options.CacheDir
	}
	return filepath.Join(filepath.Dir(c.options.Store.UserDir), "data", "flow_cache")
}

// enrichmentDir 不按方案分目录：内容键已包含角色、工具与候选能力。
func (c *Compiler) enrichmentDir(flowID string) string {
	return filepath.Join(c.cacheRoot(), flowID, "enrich")
}

// lockFlow 串行化同一流程的模型增强，使并发请求复用先完成者写入的结果。
func (c *Compiler) lockFlow(flowID string) func() {
	value, _ := enrichLocks.LoadOrStore(c.enrichmentDir(flowID), &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	return lock.Unlock
}

func enrichmentFile(dir, kind, key string) string {
	hexKey := strings.TrimPrefix(key, "sha256:")
	if len(hexKey) > 16 {
		hexKey = hexKey[:16]
	}
	return filepath.Join(dir, kind+"-"+hexKey+".json")
}

func (c *Compiler) enrichmentKey(env Environment, request ModelRequest) string {
	return hashKey(struct {
		Version, Model string
		Request        ModelRequest
	}{c.options.Version, env.ModelName, request})
}

// enrichmentUnits 列出当前流程的全部增强项。只依赖流程原文、基础输入、资源的标签与类型、
// 绑定角色和能力目录；config.yaml 修订号、方案名、启用流程、数据与绑定都不进入内容键。
func (c *Compiler) enrichmentUnits(source *compilationSource) []enrichmentUnit {
	doc, env := source.doc, source.env
	inputs := baseInputs(doc.Inputs)
	resources := make([]ResourcePlan, 0, len(source.resources))
	for _, item := range source.resources {
		resources = append(resources, resourceDeclaration(item))
	}
	request := flowEnrichmentRequest(doc, inputs)
	units := []enrichmentUnit{{Kind: "flow", Index: -1, Key: c.enrichmentKey(env, request), Request: request, validate: func(raw string) (any, error) {
		return validateFlowEnrichment(doc, inputs, raw)
	}}}
	catalog := newCapabilityIndex(env.Capabilities)
	for index, step := range doc.Steps {
		role, _ := bindRole(step, env.Roles)
		var owned []string
		if role != nil {
			owned = role.Tools
		}
		candidates := catalog.candidates(step.Title+"\n"+step.Instructions+"\n"+step.Completion, owned, 20)
		request := stepEnrichmentRequest(doc, step, role, inputs, resources, candidates)
		declaration := step
		units = append(units, enrichmentUnit{Kind: "step", Index: index, StepID: step.ID, StepTitle: step.Title, Key: c.enrichmentKey(env, request), Request: request, validate: func(raw string) (any, error) {
			return validateStepEnrichment(doc, declaration, inputs, candidates, raw)
		}})
	}
	c.enrichMu.Lock()
	id := source.stored.Meta.ID
	if c.activeKeys[id] == nil {
		c.activeKeys[id] = map[string][]string{}
	}
	keys := make([]string, 0, len(units))
	for _, unit := range units {
		keys = append(keys, enrichmentFile(c.enrichmentDir(id), unit.Kind, unit.Key))
	}
	c.activeKeys[id][source.env.Scheme] = keys
	c.enrichMu.Unlock()
	return units
}

// runEnrichment 调用模型完成一项增强。调用方上下文结束时返回错误且不产生记录，
// 以免把取消误记为失败；其余失败生成失败记录。
func (c *Compiler) runEnrichment(ctx context.Context, source *compilationSource, unit enrichmentUnit) (*enrichmentRecord, error) {
	started := time.Now()
	var result any
	outcome, err := modelJSON(ctx, source.env.Model, unit.Request, func(raw string) error {
		value, err := unit.validate(raw)
		if err == nil {
			result = value
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	record := &enrichmentRecord{Kind: unit.Kind, Key: unit.Key, FlowID: source.stored.Meta.ID, StepID: unit.StepID, StepTitle: unit.StepTitle, CompilerVersion: c.options.Version, Model: source.env.ModelName, CreatedAt: time.Now().UTC(), DurationMS: time.Since(started).Milliseconds(), Attempts: outcome.Attempts}
	if outcome.OK {
		record.Result, err = json.Marshal(result)
		if err != nil {
			return nil, err
		}
	} else {
		record.Failed, record.Reason = true, outcome.Reason
	}
	return record, nil
}

// loadEnrichment 读取一条增强记录；内容键不一致、格式不对或文件损坏时视为缺失。
func (c *Compiler) loadEnrichment(flowID string, unit enrichmentUnit) *enrichmentRecord {
	path := enrichmentFile(c.enrichmentDir(flowID), unit.Kind, unit.Key)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return nil
	}
	c.enrichMu.Lock()
	memo, ok := c.enrichMemo[path]
	c.enrichMu.Unlock()
	if ok && memo.record.Key == unit.Key && memo.size == info.Size() && memo.modified.Equal(info.ModTime()) {
		return memo.record
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var record enrichmentRecord
	if json.Unmarshal(raw, &record) != nil || record.APIVersion != enrichmentAPIVersion || record.Kind != unit.Kind || record.Key != unit.Key || record.FlowID != flowID {
		return nil
	}
	// 缓存内容与模型输出走同一套校验，被改坏的记录不会进入执行计划。
	if !record.Failed {
		if _, err := unit.validate(string(record.Result)); err != nil {
			return nil
		}
	}
	c.enrichMu.Lock()
	c.enrichMemo[path] = enrichmentMemo{size: info.Size(), modified: info.ModTime(), record: &record}
	c.enrichMu.Unlock()
	return &record
}

func (c *Compiler) storeEnrichment(record *enrichmentRecord) error {
	dir := c.enrichmentDir(record.FlowID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	record.APIVersion = enrichmentAPIVersion
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".enrich-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(raw)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), enrichmentFile(dir, record.Kind, record.Key))
}

// needsEnrichment：没有记录或强制时重做；失败记录在重试间隔内不重试。
func (c *Compiler) needsEnrichment(record *enrichmentRecord, force bool) bool {
	if force || record == nil {
		return true
	}
	if !record.Failed {
		return false
	}
	retryAfter := c.options.RetryAfter
	if retryAfter <= 0 {
		retryAfter = DefaultEnrichmentRetryAfter
	}
	return time.Since(record.CreatedAt) >= retryAfter
}

// pruneEnrichments 保留当前计划引用的记录，其余只留最近修改的若干个。
func (c *Compiler) pruneEnrichments(flowID string, keep map[string]bool) error {
	c.enrichMu.Lock()
	for _, keys := range c.activeKeys[flowID] {
		for _, key := range keys {
			keep[key] = true
		}
	}
	c.enrichMu.Unlock()
	dir := c.enrichmentDir(flowID)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	type file struct {
		path     string
		modified time.Time
	}
	files := []file{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, file{filepath.Join(dir, entry.Name()), info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modified.After(files[j].modified) })
	kept := 0
	for _, item := range files {
		if keep[item.path] {
			continue
		}
		if kept < enrichmentKeepLimit {
			kept++
			continue
		}
		if err := os.Remove(item.path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// removeLegacyPlanCache 删除 v2 按方案保存的整份计划缓存（flow_cache/<流程>/<方案>/*.plan.json），
// 每个进程只做一次；这些文件不再被读取。
func (c *Compiler) removeLegacyPlanCache() {
	c.legacyOnce.Do(func() {
		root := c.cacheRoot()
		paths, _ := filepath.Glob(filepath.Join(root, "*", "*", "*.plan.json"))
		dirs := map[string]bool{}
		for _, path := range paths {
			if os.Remove(path) == nil {
				dirs[filepath.Dir(path)] = true
			}
		}
		for dir := range dirs {
			_ = os.Remove(dir) // 只在目录已空时成功
		}
	})
}
