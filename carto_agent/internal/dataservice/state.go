package dataservice

import (
	"context"
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/cloudwego/eino/adk"
)

const (
	RouteDirect  = "direct"
	RouteExplore = "explore"
)

// A discovery result describes the next action, not a promise that a file exists.
// Both planning and execution use the same trusted Selection to choose a route.
func describeFindState(r *FindResult) {
	r.State, r.NextAction = "unavailable", "provide_data"
	for _, d := range r.Diagnostics {
		if d.Code == "DATA_NEED_PARAMS_INVALID" || d.Code == "DATA_NEED_UNRECOGNIZED" || d.Code == "DATA_PROVIDED_INVALID" || d.Code == "DATA_EXTENT_UNRESOLVED" {
			r.State, r.NextAction = "needs_input", "correct_need"
			return
		}
	}
	for _, row := range r.Rows {
		for _, c := range row.Candidates {
			if !c.Available {
				continue
			}
			r.State, r.NextAction = "acquirable", "plan_acquire"
			if c.Path != "" || c.LayerID != "" {
				r.State, r.NextAction = "available", "use_existing"
			}
			return
		}
	}
	for _, c := range r.Fallback {
		if c.Available {
			r.State, r.NextAction = "exploration_required", "plan_acquire"
			return
		}
	}
}

func (s Selection) RequiresDiscovery() bool {
	for _, row := range s.Rows {
		if row.Route == RouteExplore {
			return true
		}
	}
	return false
}

type acquisitionSelectionKey struct{}

// Only the trusted executor supplies this snapshot. It is not a model argument.
func WithAcquisitionSelection(ctx context.Context, selection Selection) context.Context {
	return context.WithValue(ctx, acquisitionSelectionKey{}, selection)
}
func AcquisitionSelection(ctx context.Context) (Selection, bool) {
	selection, ok := ctx.Value(acquisitionSelectionKey{}).(Selection)
	if ok {
		return selection, true
	}
	// An explicit data_get binds only this expert invocation, including resume.
	raw, exists, err := adk.GetRunLocalValue(ctx, "data-service.selection")
	if err != nil || !exists {
		return Selection{}, false
	}
	text, ok := raw.(string)
	if !ok || json.Unmarshal([]byte(text), &selection) != nil {
		return Selection{}, false
	}
	return selection, true
}

// A confirmed plan may outlive discovery TTL. Scope that exception to its exact
// frozen query; unrelated tool calls still use the ordinary expiring lookup.
func (s *Service) acquisitionQuery(ctx context.Context, id string) (query, error) {
	selection, ok := AcquisitionSelection(ctx)
	if !ok {
		return s.load(id)
	}
	if selection.FindID != id {
		return query{}, failure("DATA_QUERY_SCOPE_MISMATCH", "当前获取任务只能使用已绑定的查询："+selection.FindID)
	}
	q := query{Request: selection.Request, Result: FindResult{FindID: id, Fallback: selection.Fallback}, Items: map[string]datasource.AcquisitionItem{}}
	for _, row := range selection.Rows {
		q.Items[row.Row] = row.Item
		found := FindRow{Row: row.Row, Category: row.Item.Category}
		if row.Route != RouteExplore {
			found.Recommended = row.Candidate.ID
			found.Candidates = []Candidate{row.Candidate}
		}
		q.Result.Rows = append(q.Result.Rows, found)
	}
	return q, nil
}
