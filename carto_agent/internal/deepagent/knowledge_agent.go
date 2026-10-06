package deepagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

const (
	knowledgeContractTool = "marching_route_get_extraction_contract"
	knowledgeCompileTool  = "marching_route_compile_dataset"
)

// knowledgeSubAgent extracts with the configured business rules.
// Tool calls and final graph submission are programmatic, so neither
// the source nor the full merged graph needs to be regenerated as tool arguments.
type knowledgeSubAgent struct {
	name               string
	description        string
	instructions       string
	chatModel          model.BaseChatModel
	modelIdentity      *knowledgeModelIdentity
	config             knowledge.Config
	resolveCoordinates knowledge.CoordinateResolver
	tools              map[string]tool.BaseTool
}

func createKnowledgeSubAgent(name, description string, chatModel model.BaseChatModel, provider *config.LLMProviderConfig, tools []tool.BaseTool) *knowledgeSubAgent {
	if name != "knowledge_engineer" || chatModel == nil {
		return nil
	}
	indexed := indexToolsByName(tools)
	for _, name := range []string{knowledgeContractTool, knowledgeCompileTool} {
		if !knowledgeToolInvokable(indexed[name]) {
			return nil
		}
	}
	cfg := knowledge.DefaultConfig()
	// Preserve the unconfigured default, but never cap an explicit model limit.
	if provider != nil {
		if provider.MaxTokens > 0 {
			cfg.MaxOutputTokens = provider.MaxTokens
		}
		if provider.MaxContextTokens > 0 {
			cfg.MaxContextTokens = provider.MaxContextTokens
		}
	}
	return &knowledgeSubAgent{
		name: name, description: firstNonEmpty(description, "行军路线知识抽取"),
		chatModel: chatModel, modelIdentity: knowledgeIdentity(provider), config: cfg, tools: indexed,
	}
}

func knowledgeToolInvokable(t tool.BaseTool) bool {
	if t == nil {
		return false
	}
	if _, ok := t.(tool.EnhancedInvokableTool); ok {
		return true
	}
	_, ok := t.(tool.InvokableTool)
	return ok
}

func (a *knowledgeSubAgent) Name(context.Context) string        { return a.name }
func (a *knowledgeSubAgent) Description(context.Context) string { return a.description }

func (a *knowledgeSubAgent) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer gen.Close()
		send := func(text string) {
			gen.Send(namedEvent(a.name, adk.EventFromMessage(schema.AssistantMessage(text, nil), nil, schema.Assistant, "")))
		}
		payload, err := a.extractAndCompile(ctx, input, func(p knowledge.Progress) {
			message := schema.AssistantMessage(p.Text(), nil)
			message.Extra = map[string]any{knowledge.ProgressExtraKey: p.EventData()}
			gen.Send(namedEvent(a.name, adk.EventFromMessage(message, nil, schema.Assistant, "")))
		})
		if err != nil {
			var review *knowledgeReviewPause
			if errors.As(err, &review) {
				gen.Send(a.reviewInterrupt(ctx, review.review))
				return
			}
			gen.Send(&adk.AgentEvent{AgentName: a.name, Err: err})
			return
		}
		encoded, err := json.Marshal(map[string]any{"success": true, "data": payload})
		if err != nil {
			gen.Send(&adk.AgentEvent{AgentName: a.name, Err: err})
			return
		}
		send(string(encoded))
	}()
	return iter
}

// Explicit JSON is accepted for orchestrator invocations; free text is preserved
// byte-for-byte, including whitespace, because the contract hashes the source.
func resolveKnowledgeInput(input *adk.AgentInput) (string, map[string]any, error) {
	return resolveKnowledgeInputWithContext(context.Background(), input)
}

func resolveKnowledgeInputWithContext(ctx context.Context, input *adk.AgentInput) (string, map[string]any, error) {
	if input == nil {
		return "", nil, fmt.Errorf("知识抽取需要原文")
	}
	for i := len(input.Messages) - 1; i >= 0; i-- {
		message := input.Messages[i]
		if message == nil || message.Role != schema.User {
			continue
		}
		if strings.TrimSpace(message.Content) == "" {
			return "", nil, fmt.Errorf("知识抽取原文不能为空")
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal([]byte(message.Content), &fields) == nil {
			if ref, exists := fields["source_ref"]; exists {
				if _, conflicting := fields["source_text"]; conflicting {
					return "", nil, fmt.Errorf("source_ref 和 source_text 不能同时提供")
				}
				var name string
				if json.Unmarshal(ref, &name) != nil || name != "current_user_message" {
					return "", nil, fmt.Errorf("未知原文引用")
				}
				original, exists := knowledge.OriginalSource(ctx)
				if !exists || strings.TrimSpace(original) == "" {
					return "", nil, fmt.Errorf("当前运行没有保存原文，不能用摘要代替")
				}
				fields["source_text"], _ = json.Marshal(original)
			}
			if raw, exists := fields["source_text"]; exists {
				var source string
				if err := json.Unmarshal(raw, &source); err != nil || strings.TrimSpace(source) == "" {
					return "", nil, fmt.Errorf("source_text 必须是非空字符串")
				}
				options := map[string]any{}
				if value, exists := fields["force_refresh"]; exists {
					// This is extraction control, not original text or a compiler option.
					if string(value) != "true" && string(value) != "false" {
						return "", nil, fmt.Errorf("force_refresh 必须是布尔值")
					}
					options["force_refresh"] = string(value) == "true"
				}
				for _, key := range []string{"template_dataset_dir", "symbol_assets_dir", "output_dir"} {
					if value, exists := fields[key]; exists {
						var path string
						if err := json.Unmarshal(value, &path); err != nil {
							return "", nil, fmt.Errorf("%s 必须是字符串", key)
						}
						if strings.TrimSpace(path) != "" {
							options[key] = path
						}
					}
				}
				return source, options, nil
			}
		}
		return message.Content, map[string]any{}, nil
	}
	return "", nil, fmt.Errorf("知识抽取需要最新的用户原文消息")
}

func (a *knowledgeSubAgent) extractAndCompile(ctx context.Context, input *adk.AgentInput, progress func(knowledge.Progress)) (map[string]any, error) {
	source, compileArgs, err := resolveKnowledgeInputWithContext(ctx, input)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	forceRefresh, _ := compileArgs["force_refresh"].(bool)
	delete(compileArgs, "force_refresh")
	contractResult, err := invokeWorkflowTool(ctx, a.tools[knowledgeContractTool], knowledgeContractTool, map[string]any{"source_text": source, "compact": true})
	if err != nil {
		return nil, fmt.Errorf("获取抽取契约失败: %w", err)
	}
	contractPayload, err := knowledgeToolPayload(contractResult)
	if err != nil {
		return nil, fmt.Errorf("获取抽取契约失败: %w", err)
	}
	contractJSON, err := json.Marshal(contractPayload)
	if err != nil {
		return nil, err
	}
	var contract knowledge.Contract
	if err := json.Unmarshal(contractJSON, &contract); err != nil {
		return nil, fmt.Errorf("抽取契约格式错误: %w", err)
	}
	// Apply the actual configured extraction rules before checking caches or
	// creating the editable result.
	if a.instructions != "" {
		contract.Instructions = a.instructions
	}
	sourceHash := sha256.Sum256([]byte(source))
	if contract.ContractID == "" || contract.Instructions == "" || len(contract.JSONSchema) == 0 || contract.SourceSHA256 != hex.EncodeToString(sourceHash[:]) {
		return nil, fmt.Errorf("抽取契约不完整或与原文不一致；请确认插件已更新")
	}
	if knowledgeReviewStore(ctx) != nil && contract.ReviewProtocolVersion != 1 {
		return nil, fmt.Errorf("QGIS 插件尚不支持抽取结果核对，请更新插件并重新打开实例；本次尚未调用抽取模型")
	}
	traceFile, err := prepareKnowledgeTrace(contract.RuntimeSessionDir, source)
	if err != nil {
		return nil, fmt.Errorf("无法保存知识抽取过程: %w", err)
	}
	cfg := a.config
	cache, cacheInfo := newKnowledgeCache(a.modelIdentity, cfg, contract)
	var result *knowledge.Result
	var originalResult *knowledge.Result
	var reviewAssets []knowledge.ReviewAsset
	var reusedFrom *knowledge.Review
	if store := knowledgeReviewStore(ctx); store != nil && !forceRefresh {
		if confirmed, loadErr := store.FindConfirmed(knowledgeReviewProject(ctx), source, contract); loadErr == nil {
			originalResult = confirmed.OriginalResult
			reviewAssets = confirmed.Assets
			reusedFrom = confirmed
			result = cloneKnowledgeResult(confirmed.OriginalResult)
			// Reuse the accepted source, not its rendering projection. A projection
			// can omit point-only paths or split routes and must be derived anew.
			result.Graph = cloneKnowledgeGraph(confirmed.Graph)
			result.SemanticGraph = cloneKnowledgeGraph(confirmed.Graph)
			if result.Completion != nil {
				result.Completion.AdaptationComplete = true
			}
			result.Status, result.Error = "ready_to_compile", ""
			result.Attempts = []knowledge.Attempt{}
			cacheInfo.Status = "confirmed_hit"
			cacheInfo.Note = "复用同一项目、同一原文已确认的修改版；本次不重新抽取。"
			if progress != nil {
				progress(knowledge.Progress{Stage: "复用已确认的抽取结果", Current: 1, Total: 1})
			}
		} else if !errors.Is(loadErr, knowledge.ErrReviewNotFound) {
			return nil, fmt.Errorf("读取已确认结果失败（保留原结果，不自动覆盖）: %w", loadErr)
		}
	}
	if cache != nil && result == nil {
		if forceRefresh {
			cacheInfo.Status = "refresh"
		} else if saved, entry, loadErr := cache.load(); loadErr == nil {
			result = saved
			cacheInfo.Status = "hit"
			cacheInfo.OriginTrace, cacheInfo.CreatedAt = entry.OriginTrace, entry.CreatedAt
			cacheInfo.HistoricalModelRequests = len(result.Attempts)
			cacheInfo.Note = "复用已完成的抽取结果；本次未请求抽取模型，仍重新编译本会话的数据表。"
			result.Status, result.Error = "ready_to_compile", ""
			result.Attempts = []knowledge.Attempt{}
			if progress != nil {
				progress(knowledge.Progress{Stage: "复用已完成的抽取结果", Current: 1, Total: 1})
			}
		} else if !os.IsNotExist(loadErr) {
			cacheInfo.Status, cacheInfo.Note = "invalid", "缓存不可用，本次重新抽取。"
		}
	}
	if traceFile != "" {
		cfg.Checkpoint = func(result *knowledge.Result) error {
			return writeKnowledgeTrace(traceFile, result, cacheInfo)
		}
	}
	if result == nil {
		result, err = knowledge.NewPipeline(a.chatModel, cfg).Run(ctx, source, contract, progress)
	} else if cfg.Checkpoint != nil {
		err = cfg.Checkpoint(result)
	}
	if err != nil {
		var pending *knowledge.ExtractionNeedsReviewError
		if knowledgeReviewStore(ctx) != nil && errors.As(err, &pending) {
			if checkErr := knowledge.ReviewableExtraction(result, source); checkErr == nil {
				err = nil // A typed, complete semantic draft, never an ignored failure.
			} else {
				return nil, fmt.Errorf("抽取草稿无法读取（过程文件 %s）: %w", traceFile, checkErr)
			}
		}
	}
	if err != nil {
		if result != nil && result.Status == "compile_blocked" {
			return nil, fmt.Errorf("抽取已完成，但制图数据转换未完成（抽取事实与关系保存在过程文件 %s）: %w", traceFile, err)
		}
		if traceFile != "" {
			return nil, fmt.Errorf("知识抽取未完成（过程文件 %s）: %w", traceFile, err)
		}
		return nil, fmt.Errorf("知识抽取未完成: %w", err)
	}
	if result == nil {
		return nil, fmt.Errorf("知识抽取没有返回结果")
	}
	// Existing approved coordinates remain unchanged. A fresh draft may fill
	// missing background locations from an older approved graph as well.
	coordinateErr := knowledge.ValidateResolvedCoordinates(result.Graph)
	coordinatesAttempted := !knowledge.HasPartialExtraction(result) && (reusedFrom == nil || coordinateErr != nil)
	if coordinatesAttempted {
		coordinateErr = a.enrichCoordinates(ctx, result, source, progress)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if errors.Is(coordinateErr, context.Canceled) {
		return nil, coordinateErr
	}
	if coordinateErr == nil {
		coordinateErr = knowledge.ValidatePlaceCoordinates(result.Graph)
	}
	if coordinateErr != nil {
		result.Status, result.Error = "coordinate_failed", coordinateErr.Error()
		if cfg.Checkpoint != nil {
			if saveErr := cfg.Checkpoint(result); saveErr != nil {
				return nil, errors.Join(coordinateErr, fmt.Errorf("保存定位失败记录: %w", saveErr))
			}
		}
		if knowledgeReviewStore(ctx) == nil {
			return nil, coordinateErr
		}
		// Extraction remains usable even when an optional reference or a real
		// place cannot be located. Keep the transactional source graph and expose
		// the existing coordinate editor instead of discarding every route.
		if progress != nil {
			progress(knowledge.Progress{Stage: "抽取结果已保留，可在结果中补充缺失坐标"})
		}
	} else if coordinatesAttempted {
		if reusedFrom != nil {
			cacheInfo.Note = "复用已确认的抽取事实，并自动补齐旧稿中缺失的地点坐标。"
		}
		if cfg.Checkpoint != nil {
			if err := cfg.Checkpoint(result); err != nil {
				return nil, fmt.Errorf("保存定位结果失败: %w", err)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if store := knowledgeReviewStore(ctx); store != nil {
		if originalResult == nil {
			originalResult = result
		}
		review, err := createKnowledgeReview(ctx, store, source, contract, originalResult, result.Graph, compileArgs, traceFile, cacheInfo, reviewAssets, result, reusedFrom)
		if err != nil {
			return nil, fmt.Errorf("保存抽取结果失败: %w", err)
		}
		return nil, &knowledgeReviewPause{review: review}
	}
	return a.compileKnowledge(ctx, source, contract, result, compileArgs, traceFile, cache, cacheInfo)
}

// Compilation is deliberately separate from extraction: Resume must never call
// the model again or reinterpret the original source as a new request.
func (a *knowledgeSubAgent) compileKnowledge(ctx context.Context, source string, contract knowledge.Contract, result *knowledge.Result, compileArgs map[string]any, traceFile string, cache *knowledgeCache, cacheInfo knowledgeCacheInfo) (map[string]any, error) {
	// Use the same drawing projection as the review workflow. The extraction
	// remains factual; anonymous geometry controls exist only in this copy.
	prepared, preparationErrors := knowledge.PrepareReviewCompilation(&knowledge.Review{Graph: result.Graph})
	if len(preparationErrors) > 0 {
		return nil, fmt.Errorf("制图路径转换失败: %s", preparationErrors[0].Message)
	}
	compileArgs["source_text"] = source
	compileArgs["knowledge_json"] = prepared
	compileArgs["contract_id"] = contract.ContractID
	finish := func(status string, failure error) error {
		result.Status = status
		result.Error = ""
		if failure != nil {
			result.Error = failure.Error()
		}
		if traceFile != "" {
			if e := writeKnowledgeTrace(traceFile, result, cacheInfo); e != nil {
				return errors.Join(failure, fmt.Errorf("保存编译状态失败: %w", e))
			}
		}
		return failure
	}
	compiled, err := invokeWorkflowTool(ctx, a.tools[knowledgeCompileTool], knowledgeCompileTool, compileArgs)
	if err != nil {
		return nil, finish("blocked", fmt.Errorf("制图数据编译失败（过程文件 %s）: %w", traceFile, err))
	}
	payload, err := knowledgeToolPayload(compiled)
	if err != nil {
		return nil, finish("blocked", fmt.Errorf("制图数据编译失败（过程文件 %s）: %w", traceFile, err))
	}
	if dir, ok := payload["dataset_dir"].(string); !ok || strings.TrimSpace(dir) == "" {
		return nil, finish("blocked", fmt.Errorf("编译工具未返回 dataset_dir，不能交付本次数据"))
	}
	if err := ctx.Err(); err != nil {
		return nil, finish("blocked", err)
	}
	if traceFile != "" {
		payload["trace_file"] = traceFile
	}
	warnings, _ := payload["warnings"].([]any)
	if len(result.Issues) > 0 {
		payload["extraction_warnings"] = result.Issues
		for _, issue := range result.Issues {
			label := "知识抽取待定信息"
			if issue.Reason == "not_stated" {
				label = "原文未提供的细节（保留原文表述）"
			} else if issue.Kind == "time" {
				label = "知识抽取时间未定"
			}
			warnings = append(warnings, label+" ["+issue.ID+"]: "+issue.Message)
		}
	}
	for _, issue := range knowledge.ExtractionPendingIssues(result) {
		if !issue.Warning {
			continue
		}
		switch issue.Phase {
		case "boundary_review":
			warnings = append(warnings, "跨段关系未自动确认（已保留独立分段，不阻止制图）: "+issue.Message)
		case "route_review_failure":
			warnings = append(warnings, "路线关系未自动确认（已保留原路线，不阻止制图）: "+issue.Message)
		}
	}
	if len(warnings) > 0 {
		payload["warnings"] = warnings
		if handoff, ok := payload["handoff"].(map[string]any); ok {
			handoff["warnings"] = warnings
		}
	}
	if err := finish("completed", nil); err != nil {
		return nil, err
	}
	// Publish only after extraction, compilation, and the final checkpoint succeed.
	// Cache I/O is an optimization; it must never turn successful work into failure.
	if cache != nil && cacheInfo.Status != "hit" {
		if err := cache.save(result, traceFile); err != nil {
			cacheInfo.Note = "本次制图数据已生成，但新的抽取结果未能写入缓存。"
		} else {
			cacheInfo.Stored = true
		}
		if traceFile != "" {
			if err := writeKnowledgeTrace(traceFile, result, cacheInfo); err != nil {
				cacheInfo.Note += " 缓存说明未写入过程文件，已完成的结果不受影响。"
			}
		}
	}
	payload["extraction_cache"] = cacheInfo
	return payload, nil
}

// Both tool runtime implementations return success with data or output. Treat a
// missing/false success flag as failure rather than silently compiling bad data.
func knowledgeToolPayload(result *schema.ToolResult) (map[string]any, error) {
	var envelope struct {
		Success *bool           `json:"success"`
		Data    json.RawMessage `json:"data"`
		Output  json.RawMessage `json:"output"`
		Error   json.RawMessage `json:"error"`
		Info    string          `json:"info"`
	}
	if err := json.Unmarshal([]byte(workflowToolResultText(result)), &envelope); err != nil {
		return nil, fmt.Errorf("工具未返回有效 JSON: %w", err)
	}
	if envelope.Success == nil || !*envelope.Success {
		return nil, fmt.Errorf("工具执行失败: %s", firstNonEmpty(envelope.Info, string(envelope.Error), "未提供错误信息"))
	}
	for _, raw := range []json.RawMessage{envelope.Data, envelope.Output} {
		var payload map[string]any
		if json.Unmarshal(raw, &payload) == nil && payload != nil {
			return payload, nil
		}
	}
	return nil, fmt.Errorf("工具返回缺少 data/output 对象")
}

func prepareKnowledgeTrace(sessionDir, source string) (string, error) {
	if sessionDir == "" {
		return "", nil // Offline/direct calls must not leave artifacts in cwd.
	}
	root := filepath.Clean(sessionDir)
	if !filepath.IsAbs(root) || filepath.Dir(root) == root {
		return "", fmt.Errorf("运行时会话目录不是有效的绝对目录")
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	parent := filepath.Join(resolvedRoot, "intermediate", "route_knowledge_extraction")
	// Reject pre-existing symlinks in the write path. The caller only authorizes
	// artifacts beneath this task's injected session root.
	for _, path := range []string{filepath.Dir(parent), parent} {
		info, err := os.Lstat(path)
		if err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
			return "", fmt.Errorf("抽取输出目录不是普通目录: %s", path)
		}
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(parent, time.Now().UTC().Format("20060102T150405")+"-")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "source.txt"), []byte(source), 0o600); err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.json"), nil
}

func writeKnowledgeJSON(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".state-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(encoded); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
