package deepagent

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/knowledge"
)

const knowledgeCacheVersion = 2

// Bump this when Go extraction prompts, merging, or rendering adaptation change.
// The contract hashes cover Python-side rules; they do not cover this pipeline.
const knowledgeExtractionRevision = "2026-09-27-explicit-route-topology"

// Use an explicit whitelist, never serialize LLMProviderConfig (it has APIKey).
type knowledgeModelIdentity struct {
	Provider        string
	Model           string
	EndpointHash    string
	Temperature     float64
	ThinkingEnabled bool
	ThinkingBudget  int
}

func knowledgeIdentity(provider *config.LLMProviderConfig) *knowledgeModelIdentity {
	if provider == nil || strings.TrimSpace(provider.Provider) == "" || strings.TrimSpace(provider.Model) == "" {
		return nil
	}
	return &knowledgeModelIdentity{
		Provider: provider.Provider, Model: provider.Model,
		EndpointHash: knowledgeDigest([]byte(strings.TrimSpace(provider.BaseURL))),
		Temperature:  provider.Temperature, ThinkingEnabled: provider.ThinkingEnabled,
		ThinkingBudget: provider.ThinkingBudget,
	}
}

type knowledgeCacheInfo struct {
	Status                  string `json:"status"`
	Key                     string `json:"key,omitempty"`
	Stored                  bool   `json:"stored,omitempty"`
	OriginTrace             string `json:"origin_trace,omitempty"`
	CreatedAt               string `json:"created_at,omitempty"`
	HistoricalModelRequests int    `json:"historical_model_requests,omitempty"`
	Note                    string `json:"note,omitempty"`
}

type knowledgeCacheEntry struct {
	Version      int             `json:"version"`
	Key          string          `json:"key"`
	CreatedAt    string          `json:"created_at"`
	OriginTrace  string          `json:"origin_trace"`
	ResultSHA256 string          `json:"result_sha256"`
	Result       json.RawMessage `json:"result"`
}

type knowledgeCache struct {
	dataDir  string
	key      string
	contract knowledge.Contract
}

func knowledgeDigest(value []byte) string { return fmt.Sprintf("%x", sha256.Sum256(value)) }

func newKnowledgeCache(identity *knowledgeModelIdentity, cfg knowledge.Config, contract knowledge.Contract) (*knowledgeCache, knowledgeCacheInfo) {
	info := knowledgeCacheInfo{Status: "disabled"}
	if identity == nil || contract.RuntimeSessionDir == "" {
		info.Note = "模型身份或会话目录不完整，本次正常抽取。"
		return nil, info
	}
	dataDir, err := config.ResolvePluginDataDir()
	if err != nil || !filepath.IsAbs(dataDir) {
		info.Note = "未配置有效的应用数据目录，本次正常抽取。"
		return nil, info
	}
	// Resolve the configured home, but reject symlinks inside the cache write path.
	home, err := filepath.EvalSymlinks(filepath.Dir(dataDir))
	if err != nil {
		info.Note = "应用数据目录不可用，本次正常抽取。"
		return nil, info
	}
	var schema any
	if err := json.Unmarshal(contract.JSONSchema, &schema); err != nil {
		info.Note = "抽取契约无法生成缓存标识，本次正常抽取。"
		return nil, info
	}
	fingerprint := struct {
		Version                                                  int
		PipelineRevision                                         string
		SourceSHA256                                             string
		ContractID                                               string
		Instructions                                             string
		Schema                                                   any
		Model                                                    knowledgeModelIdentity
		TargetChars, MaxChars, MaxOutputTokens, MaxContextTokens int
		ReviewAllRoutes                                          bool
	}{
		Version: knowledgeCacheVersion, PipelineRevision: knowledgeExtractionRevision,
		SourceSHA256: contract.SourceSHA256, ContractID: contract.ContractID,
		Instructions: contract.Instructions, Schema: schema, Model: *identity,
		TargetChars: cfg.TargetChars, MaxChars: cfg.MaxChars,
		MaxOutputTokens: cfg.MaxOutputTokens, MaxContextTokens: cfg.MaxContextTokens,
		ReviewAllRoutes: cfg.ReviewAllRoutes,
	}
	encoded, err := json.Marshal(fingerprint)
	if err != nil {
		info.Note = "模型配置无法生成缓存标识，本次正常抽取。"
		return nil, info
	}
	c := &knowledgeCache{dataDir: filepath.Join(home, "data"), key: knowledgeDigest(encoded), contract: contract}
	info.Status, info.Key = "miss", c.key
	return c, info
}

func (c *knowledgeCache) dir() string {
	return filepath.Join(c.dataDir, "knowledge_cache", fmt.Sprintf("v%d", knowledgeCacheVersion))
}

func (c *knowledgeCache) path() string { return filepath.Join(c.dir(), c.key+".json") }

func (c *knowledgeCache) checkDirs() error {
	for _, path := range []string{c.dataDir, filepath.Join(c.dataDir, "knowledge_cache"), c.dir()} {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("缓存目录不是普通目录: %s", path)
		}
	}
	return nil
}

func (c *knowledgeCache) load() (*knowledge.Result, *knowledgeCacheEntry, error) {
	if err := c.checkDirs(); err != nil {
		return nil, nil, err
	}
	info, err := os.Lstat(c.path())
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("缓存条目不是普通文件")
	}
	raw, err := os.ReadFile(c.path())
	if err != nil {
		return nil, nil, err
	}
	var entry knowledgeCacheEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, nil, err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, entry.Result); err != nil {
		return nil, nil, err
	}
	if entry.Version != knowledgeCacheVersion || entry.Key != c.key || knowledgeDigest(compact.Bytes()) != entry.ResultSHA256 {
		return nil, nil, fmt.Errorf("缓存版本、标识或完整性校验不一致")
	}
	var result knowledge.Result
	if err := json.Unmarshal(entry.Result, &result); err != nil {
		return nil, nil, err
	}
	if result.Status != "completed" || result.Error != "" || len(result.Graph) == 0 || len(result.SemanticGraph) == 0 ||
		result.SourceSHA256 != c.contract.SourceSHA256 || result.ContractID != c.contract.ContractID {
		return nil, nil, fmt.Errorf("缓存不是当前原文和契约下的完整成功结果")
	}
	return &result, &entry, nil
}

func (c *knowledgeCache) save(result *knowledge.Result, traceFile string) error {
	if result == nil || result.Status != "completed" || result.Error != "" || len(result.Graph) == 0 || len(result.SemanticGraph) == 0 ||
		result.SourceSHA256 != c.contract.SourceSHA256 || result.ContractID != c.contract.ContractID {
		return fmt.Errorf("只保存完整成功的抽取结果")
	}
	if err := c.checkDirs(); err != nil {
		return err
	}
	if err := os.MkdirAll(c.dir(), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return writeKnowledgeJSON(c.path(), knowledgeCacheEntry{
		Version: knowledgeCacheVersion, Key: c.key,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), OriginTrace: traceFile,
		ResultSHA256: knowledgeDigest(raw), Result: raw,
	})
}

// Keep the existing top-level trace shape; provenance is additional metadata.
// On hits Attempts is empty: cached requests were not made in this session.
func writeKnowledgeTrace(path string, result *knowledge.Result, cache knowledgeCacheInfo) error {
	return writeKnowledgeJSON(path, struct {
		*knowledge.Result
		Cache knowledgeCacheInfo `json:"extraction_cache"`
	}{result, cache})
}
