package deepagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/toolsession"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Review state is a disk reference, not a second copy of a potentially large
// graph in the agent checkpoint or the chat/model context.
type knowledgeReviewState struct {
	SessionID string
	ReviewID  string
}

type knowledgeReviewContextKey struct{}

func init() {
	schema.Register[knowledgeReviewState]()
	// Progress message extras and the small review card are retained by ADK's
	// checkpoint; their concrete interface values also need serialization types.
	schema.Register[map[string]any]()
	schema.Register[map[string]int]()
	schema.Register[[]any]()
	schema.Register[[]map[string]any]()
	schema.Register[[]knowledge.ReviewError]()
}

// WithKnowledgeReview enables the interactive review stage only for an actual
// application run. Offline callers keep the existing automatic compiler path.
func WithKnowledgeReview(ctx context.Context, store *knowledge.ReviewStore) context.Context {
	return context.WithValue(ctx, knowledgeReviewContextKey{}, store)
}

func knowledgeReviewStore(ctx context.Context) *knowledge.ReviewStore {
	store, _ := ctx.Value(knowledgeReviewContextKey{}).(*knowledge.ReviewStore)
	return store
}

func knowledgeReviewProject(ctx context.Context) string {
	return toolsession.ProjectPathFromContext(ctx)
}

type knowledgeReviewPause struct{ review *knowledge.Review }

func (e *knowledgeReviewPause) Error() string {
	return "知识抽取结果已保存，等待开始制图"
}

func cloneKnowledgeGraph(graph knowledge.Graph) knowledge.Graph {
	encoded, _ := json.Marshal(graph)
	var copied knowledge.Graph
	_ = json.Unmarshal(encoded, &copied)
	return copied
}

func cloneKnowledgeResult(result *knowledge.Result) *knowledge.Result {
	encoded, _ := json.Marshal(result)
	var copied knowledge.Result
	_ = json.Unmarshal(encoded, &copied)
	return &copied
}

func createKnowledgeReview(ctx context.Context, store *knowledge.ReviewStore, source string, contract knowledge.Contract, original *knowledge.Result, graph knowledge.Graph, args map[string]any, trace string, cacheInfo knowledgeCacheInfo, assets []knowledge.ReviewAsset, current *knowledge.Result, reusedFrom *knowledge.Review) (*knowledge.Review, error) {
	if contract.ReviewProtocolVersion != 1 {
		return nil, fmt.Errorf("QGIS 插件尚不支持抽取结果核对，请更新插件并重新打开实例（需要核对协议 1）")
	}
	metadata, _ := json.Marshal(cacheInfo)
	var cache map[string]any
	_ = json.Unmarshal(metadata, &cache)
	return store.Create(knowledge.ReviewCreateInput{
		SessionID: toolsession.SessionIDFromContext(ctx), ProjectPath: knowledgeReviewProject(ctx),
		InstanceID: toolsession.InstanceIDFromContext(ctx), SessionDir: contract.RuntimeSessionDir,
		SourceText: source, Contract: contract, OriginalResult: original, Graph: graph,
		CompileArgs: args, ResourceScope: knowledgeResourceSnapshot(ctx), TraceFile: trace, OriginalCacheInfo: cache, Assets: assets,
		ExtractionStatus: current.Status, PendingIssues: knowledge.ExtractionPendingIssues(current),
		ReusedFrom: reusedFrom,
	})
}

// Only backend context supplies this snapshot. ReviewView and model input do not
// expose it, and resumption must match the session that owns the saved review.
func knowledgeResourceSnapshot(ctx context.Context) *resource.Scope {
	scope, ok := resource.ScopeFromContext(ctx)
	if !ok {
		return nil
	}
	selected := resource.Scope{FlowID: scope.FlowID, FlowDir: scope.FlowDir, Lock: scope.Lock}
	for _, item := range scope.Linked {
		if item != nil && (item.Type == "route_style" || item.Type == "symbols") {
			selected.Linked = append(selected.Linked, item.Clone())
		}
	}
	return &selected
}

func knowledgeReviewResourceContext(ctx context.Context, review *knowledge.Review) (context.Context, error) {
	if review == nil || review.SessionID == "" || review.SessionID != toolsession.SessionIDFromContext(ctx) {
		return nil, fmt.Errorf("知识核对资源作用域与当前会话不一致；草稿仍保留")
	}
	for _, key := range []string{"route_style", "symbols"} {
		handle, ok := review.CompileArgs[key].(map[string]any)
		if !ok {
			continue
		}
		if review.ResourceScope == nil {
			return nil, fmt.Errorf("知识核对缺少已保存的资源作用域，不能恢复 %s；请确认流程资源声明已保存后重新开始抽取", key)
		}
		id, _ := handle["id"].(string)
		matched := false
		for _, linked := range review.ResourceScope.Linked {
			if linked != nil && id != "" && linked.ID == id && linked.Type == key {
				matched = true
				break
			}
		}
		if !matched {
			return nil, fmt.Errorf("知识核对保存的资源作用域缺少 %s「%s」；请确认流程资源声明已保存后重新开始抽取", key, id)
		}
	}
	if review.ResourceScope != nil {
		ctx = resource.WithScope(ctx, *review.ResourceScope)
	}
	return resource.WithInternalHandles(ctx), nil
}

// KnowledgeReviewInfo is intentionally small and safe to persist in chat history.
func KnowledgeReviewInfo(review *knowledge.Review) map[string]any {
	counts := map[string]int{}
	for _, key := range []string{"routes", "events", "spatial_entities", "actors", "callouts"} {
		if items, ok := review.Graph[key].([]any); ok {
			counts[key] = len(items)
			if key == "spatial_entities" {
				for _, raw := range items {
					if item, ok := raw.(map[string]any); ok && item["entity_kind"] == "cartographic_control" {
						counts[key]--
					}
				}
			}
		}
	}
	counts["pending_issues"] = len(knowledge.ValidateReviewPending(review, review.Graph))
	description := "可直接开始制图，也可以先修改抽取结果。"
	if issues := knowledge.ValidateReviewPending(review, review.Graph); len(issues) > 0 {
		description = issues[0].Message
	} else if review.Status == knowledge.ReviewCompileFailed && len(review.Errors) > 0 {
		description = review.Errors[0].Message
	}
	return map[string]any{
		"type": "knowledge_review", "title": "知识抽取结果", "review_id": review.ReviewID,
		"session_id": review.SessionID, "revision": review.Revision, "status": review.Status,
		"summary": counts, "description": description, "can_start": knowledge.HasExtractedContent(review.Graph),
		"errors": review.View().Errors, "updated_at": review.UpdatedAt,
	}
}

func knowledgeReviewUpdateEvent(agent string, info map[string]any) *adk.AgentEvent {
	message := schema.AssistantMessage("", nil)
	message.Extra = map[string]any{"knowledge_review_update": info}
	return namedEvent(agent, adk.EventFromMessage(message, nil, schema.Assistant, ""))
}

func (a *knowledgeSubAgent) reviewInterrupt(ctx context.Context, review *knowledge.Review) *adk.AgentEvent {
	return namedEvent(a.name, adk.StatefulInterrupt(ctx, KnowledgeReviewInfo(review), knowledgeReviewState{
		SessionID: review.SessionID, ReviewID: review.ReviewID,
	}))
}

func (a *knowledgeSubAgent) Resume(ctx context.Context, info *adk.ResumeInfo, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer gen.Close()
		fail := func(err error) { gen.Send(&adk.AgentEvent{AgentName: a.name, Err: err}) }
		state, ok := info.InterruptState.(knowledgeReviewState)
		store := knowledgeReviewStore(ctx)
		if !ok || store == nil || state.SessionID != toolsession.SessionIDFromContext(ctx) {
			fail(fmt.Errorf("知识核对恢复状态不完整或与当前会话不一致；草稿仍保留"))
			return
		}
		review, err := store.Get(state.SessionID, state.ReviewID)
		if err != nil {
			fail(err)
			return
		}
		if !info.IsResumeTarget {
			gen.Send(a.reviewInterrupt(ctx, review))
			return
		}
		var target struct {
			ReviewID string `json:"review_id"`
			Revision int64  `json:"revision"`
		}
		raw, _ := json.Marshal(info.ResumeData)
		if json.Unmarshal(raw, &target) != nil || target.ReviewID != review.ReviewID || target.Revision != review.Revision || review.Status != "compiling" {
			fail(fmt.Errorf("核对版本未确认或已变化，不能使用过期版本制图"))
			return
		}
		gen.Send(knowledgeReviewUpdateEvent(a.name, KnowledgeReviewInfo(review)))
		payload, err := a.compileConfirmedKnowledge(ctx, store, review)
		if err != nil {
			var failure *knowledgeCompileFailure
			if !errors.As(err, &failure) {
				fail(err)
				return
			}
			info := KnowledgeReviewInfo(failure.review)
			gen.Send(knowledgeReviewUpdateEvent(a.name, info))
			encoded, _ := json.Marshal(failure.result())
			gen.Send(namedEvent(a.name, adk.EventFromMessage(schema.AssistantMessage(string(encoded), nil), nil, schema.Assistant, "")))
			return
		}
		gen.Send(knowledgeReviewUpdateEvent(a.name, payload["knowledge_review"].(map[string]any)))
		encoded, _ := json.Marshal(map[string]any{"success": true, "data": payload})
		gen.Send(namedEvent(a.name, adk.EventFromMessage(schema.AssistantMessage(string(encoded), nil), nil, schema.Assistant, "")))
	}()
	return iter
}

// A failed conversion returns to the orchestrator. Human confirmation concerns
// the saved facts; correcting drawing parameters does not require a new review.
func (a *knowledgeSubAgent) compileConfirmedKnowledge(ctx context.Context, store *knowledge.ReviewStore, review *knowledge.Review) (map[string]any, error) {
	payload, err := a.compileReviewedKnowledge(ctx, store, review)
	if err == nil {
		return payload, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	updated, saveErr := store.Fail(review.SessionID, review.ReviewID, review.Revision, []knowledge.ReviewError{{Path: "$", Message: err.Error()}})
	if saveErr != nil {
		return nil, fmt.Errorf("%v；保存失败状态: %w", err, saveErr)
	}
	return nil, &knowledgeCompileFailure{error: err, review: updated}
}

func (a *knowledgeSubAgent) compileReviewedKnowledge(ctx context.Context, store *knowledge.ReviewStore, review *knowledge.Review) (map[string]any, error) {
	var scopeErr error
	ctx, scopeErr = knowledgeReviewResourceContext(ctx, review)
	if scopeErr != nil {
		return nil, scopeErr
	}
	prepared, problems := knowledge.PrepareReviewCompilation(review)
	if len(problems) > 0 {
		return nil, fmt.Errorf("制图数据适配失败: %s", problems[0].Message)
	}
	result := cloneKnowledgeResult(review.OriginalResult)
	result.Graph = prepared
	// The immutable extraction stays in OriginalResult. This run's pre-compile
	// graph must reflect human corrections, including a reused confirmed edition.
	result.SemanticGraph = cloneKnowledgeGraph(review.Graph)
	if result.Completion != nil {
		result.Completion.AdaptationComplete = true
	}
	resolved := map[string]bool{}
	for _, decision := range review.Resolutions {
		resolved[decision.IssueID] = true
	}
	for _, previous := range review.InheritedProvenance {
		for _, decision := range previous.IssueResolutions {
			resolved[decision.IssueID] = true
		}
	}
	remaining := []knowledge.Issue{}
	for _, issue := range result.Issues {
		if !resolved[issue.ID] {
			remaining = append(remaining, issue)
		}
	}
	result.Issues = remaining
	result.Status, result.Error = "ready_to_compile", ""
	args := map[string]any{}
	for key, value := range review.CompileArgs {
		args[key] = value
	}
	token, err := store.SignContext(review)
	if err != nil {
		return nil, err
	}
	args["review_context"] = token
	var cacheInfo knowledgeCacheInfo
	metadata, _ := json.Marshal(review.OriginalCacheInfo)
	_ = json.Unmarshal(metadata, &cacheInfo)
	// Cache only an unchanged, completed extraction. New runs deliberately skip
	// the former model semantic review; legacy completion remains verifiable.
	// Human revisions remain in the confirmed-review store, not the model cache.
	var cache *knowledgeCache
	completion := result.Completion
	if cacheInfo.Status != "confirmed_hit" && review.OriginalResult != nil && review.ExtractionStatus != "needs_review" && review.OriginalResult.Status != "needs_review" &&
		reflect.DeepEqual(review.Graph, review.OriginalResult.Graph) && completion != nil && completion.AdaptationComplete &&
		knowledge.ValidateExtractionCompletion(review.OriginalResult) == nil {
		cfg := a.config
		cache, _ = newKnowledgeCache(a.modelIdentity, cfg, review.Contract)
		if cache != nil {
			result.SemanticGraph = cloneKnowledgeGraph(review.OriginalResult.SemanticGraph)
		}
	}
	payload, err := a.compileKnowledge(ctx, review.SourceText, review.Contract, result, args, review.TraceFile, cache, cacheInfo)
	if err != nil {
		return nil, err
	}
	completed, err := store.Complete(review.SessionID, review.ReviewID, review.Revision, prepared, payload)
	if err != nil {
		return nil, fmt.Errorf("数据表已生成，但已确认版本保存失败: %w", err)
	}
	payload["knowledge_review"] = KnowledgeReviewInfo(completed)
	return payload, nil
}

// ValidateKnowledgeReview uses the same Python validator as the compiler, but
// with validate_only so no data table is written before user confirmation.
func ValidateKnowledgeReview(ctx context.Context, store *knowledge.ReviewStore, review *knowledge.Review, compiler tool.BaseTool) ([]knowledge.ReviewError, error) {
	var scopeErr error
	ctx, scopeErr = knowledgeReviewResourceContext(ctx, review)
	if scopeErr != nil {
		return nil, scopeErr
	}
	if review.Contract.ReviewProtocolVersion != 1 || compiler == nil {
		return nil, fmt.Errorf("插件核对接口不可用，请更新并连接对应实例")
	}
	prepared, problems := knowledge.PrepareReviewCompilation(review)
	if len(problems) > 0 {
		return problems, nil
	}
	token, err := store.SignContext(review)
	if err != nil {
		return nil, err
	}
	args := map[string]any{
		"source_text": review.SourceText, "contract_id": review.Contract.ContractID,
		"knowledge_json": prepared, "review_context": token, "validate_only": true,
	}
	for _, key := range []string{"route_style", "symbols"} {
		if value, exists := review.CompileArgs[key]; exists {
			args[key] = value
		}
	}
	output, err := invokeRuntimeTool(ctx, compiler, knowledgeCompileTool, args)
	if err != nil {
		return nil, err
	}
	payload, err := knowledgeToolPayload(output)
	if err != nil {
		return nil, err
	}
	var response struct {
		ReviewProtocolVersion int `json:"review_protocol_version"`
		Validation            *struct {
			Valid  bool                    `json:"valid"`
			Errors []knowledge.ReviewError `json:"errors"`
		} `json:"validation"`
	}
	raw, _ := json.Marshal(payload)
	if json.Unmarshal(raw, &response) != nil || response.ReviewProtocolVersion != 1 || response.Validation == nil {
		return nil, fmt.Errorf("插件返回的核对协议不匹配；未确认任务，请更新插件")
	}
	if !response.Validation.Valid && len(response.Validation.Errors) == 0 {
		return []knowledge.ReviewError{{Path: "$", Message: "数据校验未通过，请检查必要字段与对象关联"}}, nil
	}
	return response.Validation.Errors, nil
}
