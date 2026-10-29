package dataservice

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/hitl"
	"github.com/autocarto/carto_agent/internal/runtimepersist"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func contractExplore(t *testing.T, s *Service) (FindResult, Selection) {
	t.Helper()
	s.options.Environment.Catalog.Sources = nil
	req := riverRequest()
	req.IncludeFallback = true
	found, err := s.Find(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := s.ValidateChoice(context.Background(), found.FindID, nil)
	if err != nil || len(selection.Rows) != 1 || !selection.RequiresDiscovery() {
		t.Fatalf("exploration fixture is not selectable: %+v %v", selection, err)
	}
	return found, selection
}

func contractRegistration(t *testing.T, s *Service, id string) RegisterRequest {
	t.Helper()
	path := filepath.Join(s.options.SessionDir, "download.geojson")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"LineString","coordinates":[[110,25],[111,26]]},"properties":{"name":"river"}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	return RegisterRequest{FindID: id, Row: "河流", Path: path, Kind: "web_search", URL: "https://example.test/river.geojson", License: "CC0"}
}

func requireContractError(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), code) {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestAcquisitionContractDirectAndExplorationSelection(t *testing.T) {
	ctx := context.Background()
	s, _ := fixture(t)
	req := riverRequest()
	req.IncludeFallback = true
	direct, err := s.Find(ctx, req)
	if err != nil || direct.State != "acquirable" || direct.NextAction != "plan_acquire" {
		t.Fatalf("direct state: %+v %v", direct, err)
	}
	q, err := s.load(direct.FindID)
	if err != nil {
		t.Fatal(err)
	}
	// A cache with both types must still prefer an available direct candidate.
	q.Result.Fallback = s.fallbackCandidates(q.Items["河流"], "河流")
	if err := s.saveQuery(q); err != nil {
		t.Fatal(err)
	}
	selected, err := s.ValidateChoice(ctx, direct.FindID, nil)
	if err != nil || selected.RequiresDiscovery() || selected.Rows[0].Route != RouteDirect {
		t.Fatalf("fallback displaced direct source: %+v %v", selected, err)
	}
	_, err = s.ValidateChoice(ctx, direct.FindID, map[string]string{"*": q.Result.Fallback[0].ID})
	requireContractError(t, err, "DATA_CHOICE_INVALID")

	explore, selected := contractExplore(t, s)
	if explore.State != "exploration_required" || explore.NextAction != "plan_acquire" || selected.Rows[0].Route != RouteExplore {
		t.Fatalf("exploration was presented as completed data: %+v %+v", explore, selected)
	}
	for _, fallback := range explore.Fallback {
		chosen, err := s.ValidateChoice(ctx, explore.FindID, map[string]string{"河流": fallback.ID})
		if err != nil || chosen.Rows[0].Candidate.ID != fallback.ID || !chosen.RequiresDiscovery() {
			t.Fatalf("real fallback rejected: %+v %v", chosen, err)
		}
	}
	for _, choice := range []map[string]string{{"*": "c-invented"}, {"不存在的行": explore.Fallback[0].ID}} {
		_, err := s.ValidateChoice(ctx, explore.FindID, choice)
		requireContractError(t, err, "DATA_CHOICE_INVALID")
	}
	_, err = s.Get(ctx, GetRequest{FindID: explore.FindID})
	requireContractError(t, err, "DATA_QUERY_SCOPE_REQUIRED")
}

func TestAcquisitionContractUnresolvedNeedsNeverComplete(t *testing.T) {
	for _, kind := range []string{"unknown_category", "invalid_parameter"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := fixture(t)
			req := riverRequest()
			if kind == "unknown_category" {
				req.Need.What = What{Category: "不存在的类别"}
			} else {
				s.options.Environment.Catalog.Categories[0].Params = map[string]datasource.Parameter{"level": {Required: true, Values: []string{"one"}}}
				req.Need.Params = map[string]string{"level": "invalid"}
			}
			found, err := s.Find(context.Background(), req)
			if err != nil || found.State != "needs_input" || found.NextAction != "correct_need" || len(found.Rows) != 0 {
				t.Fatalf("invalid need not actionable: %+v %v", found, err)
			}
			if _, err := s.ValidateChoice(context.Background(), found.FindID, nil); err == nil {
				t.Fatal("unresolved query became a successful empty selection")
			}
			if found.FindID != "" {
				t.Fatal("invalid need received a query ID", found.FindID)
			}
			// Also exercise old or restored caches with zero rows.
			found.FindID = "df-00000000000000000000000000000001"
			if err := s.saveQuery(query{Request: req, Result: found}); err != nil {
				t.Fatal(err)
			}
			_, err = s.ValidateChoice(context.Background(), found.FindID, nil)
			requireContractError(t, err, "DATA_NEED_UNRESOLVED")
			// Restored/frozen selections also enter GetSelection directly.
			got, err := s.GetSelection(context.Background(), Selection{FindID: found.FindID, Request: req}, GetRequest{FindID: found.FindID})
			if err == nil || got.Status == "completed" {
				t.Fatalf("empty frozen selection was reported complete: %+v %v", got, err)
			}
		})
	}
}

func TestAcquisitionContractExplicitScopeSurvivesLaterQuery(t *testing.T) {
	s, _ := fixture(t)
	s.options.Environment.Integrations = map[string]bool{"web": true}
	a, selectedA := contractExplore(t, s)
	ctxA := WithAcquisitionSelection(context.Background(), selectedA)
	s.options.Environment.Integrations = map[string]bool{"osm": true}
	b, selectedB := contractExplore(t, s)
	if a.FindID == b.FindID {
		t.Fatal("find IDs are not distinct")
	}
	if err := s.AuthorizeFallback(ctxA, "web_search"); err != nil {
		t.Fatal("later B changed A's available acquisition methods", err)
	}
	if err := s.AuthorizeFallback(ctxA, "osm_get_roads"); err != nil {
		t.Fatal("v7 must not freeze allowed methods", err)
	}
	if err := s.AuthorizeFallback(WithAcquisitionSelection(context.Background(), selectedB), "osm_get_roads"); err != nil {
		t.Fatal(err)
	}
	if err := s.AuthorizeFallback(context.Background(), "web_search"); err != nil {
		t.Fatal("settings allow must not require a query", err)
	}
	_, err := s.Get(ctxA, GetRequest{FindID: b.FindID})
	requireContractError(t, err, "DATA_QUERY_SCOPE_MISMATCH")
	_, err = s.Register(ctxA, contractRegistration(t, s, b.FindID))
	requireContractError(t, err, "DATA_QUERY_SCOPE_MISMATCH")
}

func TestAcquisitionContractEnvironmentChecksDoNotAskPermission(t *testing.T) {
	s, _ := fixture(t)
	s.options.Config.NetworkPolicy = "deny"
	s.options.Config.FallbackPolicy = "deny"
	for _, name := range []string{"browser_check_environment", "browser_list_tools", "browser_close"} {
		if FallbackNeedsAuthorization(name) {
			t.Fatalf("environment inspection classified as fetching: %s", name)
		}
		if err := s.AuthorizeFallback(context.Background(), name); err != nil {
			t.Fatalf("environment inspection requested HITL or scope: %s: %v", name, err)
		}
	}
	if !FallbackNeedsAuthorization("browser_navigate") || !FallbackNeedsAuthorization("web_search") {
		t.Fatal("actual online access escaped authorization")
	}
}

func TestAcquisitionContractRegistrationReceiptsAreExactAndVerified(t *testing.T) {
	s, _ := fixture(t)
	a, selectedA := contractExplore(t, s)
	b, _ := contractExplore(t, s)
	ctxA := WithAcquisitionSelection(context.Background(), selectedA)
	getA := func() (GetResult, error) { return s.GetSelection(ctxA, selectedA, GetRequest{FindID: a.FindID}) }
	_, err := getA()
	requireContractError(t, err, "DATA_DISCOVERY_INCOMPLETE")
	if _, err := s.Register(context.Background(), contractRegistration(t, s, b.FindID)); err != nil {
		t.Fatal(err)
	}
	_, err = getA()
	requireContractError(t, err, "DATA_DISCOVERY_INCOMPLETE")
	// Even a receipt at the expected filename must identify this exact query.
	other, err := os.ReadFile(s.registrationPath(b.FindID, "河流"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.registrationPath(a.FindID, "河流"), other, 0600); err != nil {
		t.Fatal(err)
	}
	_, err = getA()
	requireContractError(t, err, "DATA_REGISTER_MISMATCH")
	if _, err := s.Register(ctxA, contractRegistration(t, s, a.FindID)); err != nil {
		t.Fatal(err)
	}
	got, err := getA()
	if err != nil || got.Status != "completed" || len(got.Rows) != 1 || got.Rows[0].FindID != a.FindID {
		t.Fatalf("verified receipt rejected: %+v %v", got, err)
	}
	receipt := got.Rows[0]
	receipt.Row = "另一数据行"
	if err := atomicJSON(s.registrationPath(a.FindID, "河流"), receipt); err != nil {
		t.Fatal(err)
	}
	_, err = getA()
	requireContractError(t, err, "DATA_REGISTER_MISMATCH")
	if err := atomicJSON(s.registrationPath(a.FindID, "河流"), got.Rows[0]); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(got.Rows[0].Paths[0], []byte("changed after registration"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = getA()
	requireContractError(t, err, "DATA_REGISTER_MISMATCH")
}

func TestAcquisitionContractFrozenSelectionSurvivesDiscoveryTTL(t *testing.T) {
	s, _ := fixture(t)
	found, selected := contractExplore(t, s)
	s.options.Now = func() time.Time { return found.ExpiresAt.Add(time.Second) }
	request := contractRegistration(t, s, found.FindID)
	_, err := s.ValidateChoice(context.Background(), found.FindID, nil)
	requireContractError(t, err, "DATA_FIND_EXPIRED")
	_, err = s.Register(context.Background(), request)
	requireContractError(t, err, "DATA_FIND_EXPIRED")
	ctx := WithAcquisitionSelection(context.Background(), selected)
	if err := s.AuthorizeFallback(ctx, "web_search"); err != nil {
		t.Fatal("confirmed acquisition lost its query at TTL", err)
	}
	if _, err := s.Register(ctx, request); err != nil {
		t.Fatal("confirmed acquisition cannot register after TTL", err)
	}
	got, err := s.GetSelection(ctx, selected, GetRequest{FindID: found.FindID})
	if err != nil || got.Status != "completed" || len(got.Rows) != 1 {
		t.Fatalf("frozen acquisition failed: %+v %v", got, err)
	}
}

func TestAcquisitionContractFallbackAvailabilityUsesConfiguredRuntimes(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		browser, web, search, osm     bool
		portal, catalogWeb, publicWeb bool
	}{
		{name: "web_only", web: true, search: true, catalogWeb: true, publicWeb: true},
		{name: "browser_only", browser: true, search: true, portal: true},
		{name: "search_disabled", browser: true, web: true, portal: true},
		{name: "all_disabled"},
		{name: "osm_remains_available", osm: true},
		{name: "all_enabled", browser: true, web: true, search: true, osm: true, portal: true, catalogWeb: true, publicWeb: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, calls := fixture(t)
			s.options.Environment.Integrations = map[string]bool{"browser": tc.browser, "web": tc.web, "osm": tc.osm}
			s.options.Environment.WebSearch = tc.search
			s.options.Config.WebSearchEnabled = tc.search
			// Deliberately omit requires: the execution kind still needs its
			// integration even in a user-provided catalog.
			s.options.Environment.Catalog.Sources = []datasource.Source{
				{ID: "portal.fixture", Name: "portal", Kind: "portal", Provides: []datasource.Provide{{Category: "water.river"}}},
				{ID: "web.fixture", Name: "web", Kind: "web_search", Provides: []datasource.Provide{{Category: "water.river"}}},
			}
			req := riverRequest()
			req.IncludeFallback = true
			found, err := s.Find(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			actual := map[string]bool{}
			for _, candidate := range found.Fallback {
				key := candidate.SourceID
				if key == "" {
					key = candidate.Kind
				}
				actual[key] = candidate.Available
				if !candidate.Available && len(candidate.Reasons) == 0 {
					t.Fatal("unavailable source has no configuration explanation", candidate)
				}
				_, choiceErr := s.ValidateChoice(context.Background(), found.FindID, map[string]string{"*": candidate.ID})
				if candidate.Available && choiceErr != nil || !candidate.Available && choiceErr == nil {
					t.Fatalf("candidate availability and choice differ: %+v %v", candidate, choiceErr)
				}
			}
			for key, want := range map[string]bool{"portal.fixture": tc.portal, "web.fixture": tc.catalogWeb, "web_search": tc.publicWeb, "custom_osm": tc.osm} {
				if actual[key] != want {
					t.Fatalf("%s advertised=%v want=%v; candidates=%+v", key, actual[key], want, found.Fallback)
				}
			}
			wantState := "unavailable"
			if tc.portal || tc.catalogWeb || tc.publicWeb || tc.osm {
				wantState = "exploration_required"
			}
			if found.State != wantState || calls.Load() != 0 {
				t.Fatalf("availability state or read-only discovery changed: %+v tools=%d", found, calls.Load())
			}
			if !tc.catalogWeb && !tc.publicWeb {
				_, err := s.Register(context.Background(), contractRegistration(t, s, found.FindID))
				requireContractError(t, err, "DATA_CHOICE_INVALID")
			}
		})
	}
}

func TestAcquisitionContractFallbackRetainsCatalogRequirements(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		secret       bool
		params       map[string]string
		available    bool
	}{
		{name: "ready", secret: true, params: map[string]string{"season": "wet"}, available: true},
		{name: "missing_secret", params: map[string]string{"season": "wet"}},
		{name: "browser_exploration_without_adapter", status: "experimental", secret: true, params: map[string]string{"season": "wet"}, available: true},
		{name: "different_parameters", secret: true, params: map[string]string{"season": "dry"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := fixture(t)
			s.options.Environment.Integrations = map[string]bool{"browser": true, "web": true}
			s.options.Environment.Secrets = map[string]bool{"fixture": tc.secret}
			s.options.Environment.Catalog.Categories[0].Params = map[string]datasource.Parameter{"season": {Values: []string{"dry", "wet"}}}
			s.options.Environment.Catalog.Sources = []datasource.Source{{ID: "portal.fixture", Name: "portal", Kind: "portal", Status: tc.status, Requires: []string{"secret:fixture", "integration:browser", "login:user"}, Provides: []datasource.Provide{{Category: "water.river", Params: map[string]string{"season": "wet"}}}}}
			req := riverRequest()
			req.Need.Params, req.IncludeFallback = tc.params, true
			found, err := s.Find(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			var portal *Candidate
			for i := range found.Fallback {
				if found.Fallback[i].SourceID == "portal.fixture" {
					portal = &found.Fallback[i]
				}
			}
			if portal == nil || portal.Available != tc.available {
				t.Fatalf("catalog requirements lost: %+v want available=%v", portal, tc.available)
			}
			if found.State != "exploration_required" {
				t.Fatalf("unavailable portal hid independent public search: %+v", found)
			}
		})
	}
}

func TestAcquisitionContractBuiltinStatisticsPortalWithSearchDisabled(t *testing.T) {
	s, _ := fixture(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := datasource.Load(filepath.Join(root, "autocarto", "tools", "system", "data-acquisition", "assets"), "")
	if err != nil {
		t.Fatal(err)
	}
	s.options.Environment.Catalog, s.options.Engine.Catalog = catalog, catalog
	// The user's current settings enable browser and web transport, while
	// leaving public search disabled. No user configuration is read or changed.
	s.options.Environment.Integrations = map[string]bool{"browser": true, "web": true, "osm": true}
	s.options.Environment.WebSearch, s.options.Config.WebSearchEnabled = false, false
	s.options.Config.FallbackPolicy = "ask"
	req := FindRequest{Need: Need{Name: "各市人口", What: What{Category: "stats.annual_indicator"}, Extent: "bbox:31,110,36,117", Scale: "auto", Fields: map[string]string{"region": "地区", "year": "年份", "value": "人口"}}, IncludeFallback: true}
	found, err := s.Find(context.Background(), req)
	if err != nil || found.State != "exploration_required" || len(found.Rows) != 1 || len(found.Rows[0].Candidates) != 0 {
		t.Fatalf("builtin statistics portal cannot enter exploration: %+v %v", found, err)
	}
	var selectedID string
	for _, candidate := range found.Fallback {
		if candidate.SourceID == "source.portal.nbs" && candidate.Available {
			selectedID = candidate.ID
		}
		if candidate.Kind == "web_search" && candidate.Available {
			t.Fatal("disabled search was enabled by portal exploration")
		}
	}
	if selectedID == "" {
		t.Fatal("actual NBS catalog entry unavailable", found.Fallback)
	}
	selected, err := s.ValidateChoice(context.Background(), found.FindID, map[string]string{"*": selectedID})
	if err != nil || !selected.RequiresDiscovery() || selected.Rows[0].Candidate.Kind != "portal" || selected.Rows[0].Item.Category != "stats.annual_indicator" {
		t.Fatalf("actual NBS query was not pinned as portal exploration: %+v %v", selected, err)
	}
}

func TestAcquisitionContractApprovedPortalCanReadDownloadAndRegisterWithoutSearch(t *testing.T) {
	s, _ := fixture(t)
	s.options.Environment.Integrations = map[string]bool{"browser": true, "web": true}
	s.options.Environment.WebSearch, s.options.Config.WebSearchEnabled = false, false
	s.options.Config.FallbackPolicy = "ask"
	s.options.Environment.Catalog.Sources = []datasource.Source{{ID: "source.portal", Name: "portal", Kind: "portal", Status: "experimental", Provides: []datasource.Provide{{Category: "water.river"}}}}
	req := riverRequest()
	req.IncludeFallback = true
	found, err := s.Find(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := s.ValidateChoice(context.Background(), found.FindID, nil)
	if err != nil || selected.Rows[0].Candidate.Kind != "portal" {
		t.Fatalf("portal fixture unavailable: %+v %v", selected, err)
	}
	ctx := WithAcquisitionSelection(context.Background(), selected)
	tools := adk.ToolsConfig{}
	var calls []string
	for _, name := range []string{"web_read_page", "web_download_file"} {
		tools.Tools = append(tools.Tools, contractTool{name: name, run: func(callCtx context.Context, _ string) (any, error) {
			if err := s.AuthorizeFallback(callCtx, name); err != nil {
				return nil, err
			}
			calls = append(calls, name)
			return "local transport fixture", nil
		}})
	}
	chat := &contractModel{messages: []*schema.Message{contractCall("web_read_page", `{}`), contractCall("web_download_file", `{}`), schema.AssistantMessage("done", nil)}}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{Name: "portal", Description: "local fixture", Model: chat, ToolsConfig: tools})
	if err != nil {
		t.Fatal(err)
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, CheckPointStore: runtimepersist.NewMemoryCheckpointStore(nil)})
	event, err := contractEvents(runner.Query(ctx, "acquire portal", adk.WithCheckPointID("portal-transport")))
	if err != nil || event == nil || len(calls) != 0 {
		t.Fatalf("portal transport bypassed or failed before permission: %v calls=%v", err, calls)
	}
	interrupt := event.Action.Interrupted.InterruptContexts[0]
	resumed, err := runner.ResumeWithParams(ctx, "portal-transport", &adk.ResumeParams{Targets: map[string]any{interrupt.ID: map[string]any{"option": "allow"}}})
	if err != nil {
		t.Fatal(err)
	}
	if event, err := contractEvents(resumed); err != nil || event != nil || len(calls) != 2 {
		t.Fatalf("approved portal cannot use read/download transport: event=%v err=%v calls=%v", event, err, calls)
	}
	if err := s.AuthorizeFallback(ctx, "web_search"); err != nil {
		t.Fatal("session approval should cover configured search", err)
	}
	registration := contractRegistration(t, s, found.FindID)
	_, err = s.Register(ctx, registration) // web_search is not this query's source.
	requireContractError(t, err, "DATA_CHOICE_INVALID")
	registration.Kind = "portal"
	got, err := s.Register(ctx, registration)
	if err != nil || got.Status != "completed" || len(got.Rows) != 1 || got.Rows[0].Kind != "portal" {
		t.Fatalf("transport changed the registered source kind: %+v %v", got, err)
	}
	verified, err := s.GetSelection(ctx, selected, GetRequest{FindID: found.FindID})
	if err != nil || verified.Status != "completed" {
		t.Fatalf("portal receipt not accepted: %+v %v", verified, err)
	}
}

type contractModel struct {
	messages []*schema.Message
	index    int
}

func (m *contractModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}
func (m *contractModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	if m.index >= len(m.messages) {
		return nil, fmt.Errorf("unexpected extra model request")
	}
	result := m.messages[m.index]
	m.index++
	return result, nil
}
func (m *contractModel) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

type contractTool struct {
	name string
	run  func(context.Context, string) (any, error)
}

func (t contractTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name, Desc: "local contract fixture"}, nil
}
func (t contractTool) InvokableRun(ctx context.Context, raw string, _ ...tool.Option) (string, error) {
	result, err := t.run(ctx, raw)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(result)
	return string(encoded), err
}

func contractCall(name, arguments string) *schema.Message {
	return schema.AssistantMessage("", []schema.ToolCall{{ID: "call-" + name, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: arguments}}})
}

func contractEvents(iter *adk.AsyncIterator[*adk.AgentEvent]) (*adk.AgentEvent, error) {
	for {
		event, ok := iter.Next()
		if !ok {
			return nil, nil
		}
		if event.Err != nil {
			return nil, event.Err
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			return event, nil
		}
	}
}

func TestBuildCanSelectMultipleDataQueriesInOneRun(t *testing.T) {
	s, _ := fixture(t)
	a, _ := contractExplore(t, s)
	b, _ := contractExplore(t, s)
	var scopes []string
	get := contractTool{name: "data_get", run: func(ctx context.Context, raw string) (any, error) {
		var request GetRequest
		if err := json.Unmarshal([]byte(raw), &request); err != nil {
			return nil, err
		}
		result, err := s.Get(ctx, request)
		if err != nil {
			return nil, err
		}
		selection, ok := AcquisitionSelection(ctx)
		if !ok || result.Status != "exploration_required" {
			t.Fatalf("query did not reach local exploration handoff: %+v", result)
		}
		scopes = append(scopes, selection.FindID)
		return result, nil
	}}
	chat := &contractModel{messages: []*schema.Message{
		contractCall("data_get", `{"find_id":"`+a.FindID+`"}`),
		contractCall("data_get", `{"find_id":"`+b.FindID+`"}`),
		schema.AssistantMessage("done", nil),
	}}
	toolConfig := adk.ToolsConfig{}
	toolConfig.Tools = []tool.BaseTool{get}
	agent, err := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{Name: "orchestrator", Description: "offline test", Model: chat, ToolsConfig: toolConfig})
	if err != nil {
		t.Fatal(err)
	}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: agent})
	interrupted, err := contractEvents(runner.Query(context.Background(), "prepare two inputs"))
	if err != nil || interrupted != nil {
		t.Fatalf("second query blocked by previous selection: %v, %v", interrupted, err)
	}
	if len(scopes) != 2 || scopes[0] != a.FindID || scopes[1] != b.FindID {
		t.Fatalf("wrong query handoff: %v", scopes)
	}
}

func TestAcquisitionContractADKToolScopeAndRealOptionResume(t *testing.T) {
	for _, option := range []string{"allow", "deny"} {
		t.Run(option, func(t *testing.T) {
			s, _ := fixture(t)
			s.options.Config.FallbackPolicy = "ask"
			a, _ := contractExplore(t, s)
			var laterFindID string
			var prepared GetResult
			var scopeIDs []string
			var environmentChecks, onlineCalls int
			tools := []tool.BaseTool{
				contractTool{name: "data_get", run: func(ctx context.Context, raw string) (any, error) {
					var args map[string]any
					if err := json.Unmarshal([]byte(raw), &args); err != nil {
						return nil, err
					}
					result, err := s.Invoke(ctx, "data_get", args)
					if err == nil {
						prepared = result.(GetResult)
					}
					return result, err
				}},
				contractTool{name: "data_find", run: func(ctx context.Context, _ string) (any, error) {
					req := riverRequest()
					req.Need.Name, req.IncludeFallback = "更晚的另一查询", true
					result, err := s.Find(ctx, req)
					laterFindID = result.FindID
					return result, err
				}},
				contractTool{name: "browser_check_environment", run: func(ctx context.Context, _ string) (any, error) {
					if err := s.AuthorizeFallback(ctx, "browser_check_environment"); err != nil {
						return nil, err
					}
					environmentChecks++
					return "ready", nil
				}},
				contractTool{name: "web_search", run: func(ctx context.Context, _ string) (any, error) {
					selection, ok := AcquisitionSelection(ctx)
					if !ok {
						return nil, fmt.Errorf("data_get RunLocalValue was lost in next tool context")
					}
					scopeIDs = append(scopeIDs, selection.FindID)
					if err := s.AuthorizeFallback(ctx, "web_search"); err != nil {
						return nil, err
					}
					onlineCalls++
					return "local fixture search only", nil
				}},
			}
			chat := &contractModel{messages: []*schema.Message{
				contractCall("data_get", `{"find_id":"`+a.FindID+`"}`),
				contractCall("data_find", `{}`),
				contractCall("browser_check_environment", `{}`),
				contractCall("web_search", `{}`),
				schema.AssistantMessage("done", nil),
			}}
			ctx := context.Background()
			toolConfig := adk.ToolsConfig{}
			toolConfig.Tools = tools
			agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{Name: "system_data_acquisition", Description: "local test", Model: chat, ToolsConfig: toolConfig})
			if err != nil {
				t.Fatal(err)
			}
			runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, CheckPointStore: runtimepersist.NewMemoryCheckpointStore(nil)})
			event, err := contractEvents(runner.Query(ctx, "acquire", adk.WithCheckPointID("scope-resume")))
			if err != nil || event == nil {
				t.Fatalf("real ADK data_get -> fallback did not reach permission: %v", err)
			}
			if prepared.Status != "exploration_required" || prepared.Preparation == nil || environmentChecks != 1 || onlineCalls != 0 || laterFindID == "" || laterFindID == a.FindID {
				t.Fatalf("pre-interrupt sequence failed: prepared=%+v env=%d online=%d later=%s", prepared, environmentChecks, onlineCalls, laterFindID)
			}
			interrupt := event.Action.Interrupted.InterruptContexts[0]
			info, ok := interrupt.Info.(hitl.InterruptPayload)
			if !ok || !strings.Contains(info.Description, "本会话") {
				t.Fatalf("permission did not target selected A: %+v", interrupt.Info)
			}
			// Expiry and a fresh context model real checkpoint restoration, rather
			// than preserving a context.WithValue outside the ADK runtime.
			s.options.Now = func() time.Time { return a.ExpiresAt.Add(time.Second) }
			resumed, err := runner.ResumeWithParams(context.Background(), "scope-resume", &adk.ResumeParams{Targets: map[string]any{interrupt.ID: map[string]any{"option": option}}})
			if err != nil {
				t.Fatal(err)
			}
			nextInterrupt, runErr := contractEvents(resumed)
			if nextInterrupt != nil {
				t.Fatal("resumed choice asked the same permission again")
			}
			if option == "deny" {
				requireContractError(t, runErr, "DATA_NETWORK_NOT_ALLOWED")
				if onlineCalls != 0 || s.fallbackApproved(a.FindID) {
					t.Fatal("option deny granted access")
				}
			} else if runErr != nil || onlineCalls != 1 || !s.fallbackApproved(a.FindID) {
				t.Fatalf("option allow did not authorize one operation: calls=%d err=%v", onlineCalls, runErr)
			}
			if len(scopeIDs) != 2 || scopeIDs[0] != a.FindID || scopeIDs[1] != a.FindID || s.fallbackApproved(laterFindID) != (option == "allow") {
				t.Fatalf("query scope leaked across tools/resume: %+v", scopeIDs)
			}
		})
	}
}
