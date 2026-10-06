package geocoding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type fakeModel struct {
	generate func(context.Context, []*schema.Message) (*schema.Message, error)
}

func (f fakeModel) Generate(ctx context.Context, in []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return f.generate(ctx, in)
}
func (f fakeModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("not used")
}
func modelReply(value any) *schema.Message {
	b, _ := json.Marshal(value)
	return &schema.Message{Content: string(b)}
}
func place(id, name string) map[string]any {
	return map[string]any{"entity_id": id, "name": name, "entity_kind": "place", "factual": true, "map_role": "route_node", "evidence_refs": []any{"ev1"}}
}
func graphWith(places ...map[string]any) map[string]any {
	items, nodes := make([]any, len(places)), make([]any, len(places))
	for i, p := range places {
		items[i] = p
		nodes[i] = map[string]any{"ref_id": p["entity_id"], "ref_type": "spatial_entity"}
	}
	return map[string]any{"spatial_entities": items, "routes": []any{map[string]any{"route_id": "r1", "name": "路线", "arrow_width": 5, "paths": []any{map[string]any{"nodes": nodes}}}}}
}
func fixtureServer(t *testing.T, pois []map[string]any, total int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("tk") != "test-secret" || r.URL.Query().Get("type") != "query" {
			t.Error("incorrect API authentication parameters")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": map[string]any{"infocode": 1000}, "resultType": 1, "count": total, "pois": pois})
	}))
}
func poi(name, address, coord string) map[string]any {
	return map[string]any{"name": name, "address": address, "lonlat": coord, "hotPointID": "poi-1", "typeName": "地名"}
}
func metadataOf(e map[string]any) map[string]any { return e["geocoding"].(map[string]any) }
func automaticModel(t *testing.T, onStage func(string, []map[string]any)) fakeModel {
	t.Helper()
	return fakeModel{generate: func(_ context.Context, messages []*schema.Message) (*schema.Message, error) {
		var input struct {
			Places []map[string]any `json:"places"`
		}
		if err := json.Unmarshal([]byte(messages[1].Content), &input); err != nil {
			return nil, err
		}
		prompt := messages[0].Content
		stage := "plan"
		if prompt == selectionPrompt {
			stage = "select"
		}
		if prompt == estimationPrompt {
			stage = "estimate"
		}
		if onStage != nil {
			onStage(stage, input.Places)
		}
		var out []map[string]any
		for _, p := range input.Places {
			v := map[string]any{"entity_id": p["entity_id"]}
			switch stage {
			case "plan":
				v["modern_name"], v["province"], v["city"], v["county"] = p["name"], "河南省", "信阳市", "罗山县"
				v["qualified_query"] = "河南省信阳市罗山县" + str(p["name"])
				v["administrative_hint"] = "罗山县"
				v["reason"] = "结合路线前后地点判断现代属地"
			case "select":
				v["candidate_index"], v["reason"] = 0, "候选位置与路线前后节点相符"
			case "estimate":
				v["lon"], v["lat"], v["confidence"], v["reason"] = 114.408, 31.758, 0.6, "根据罗山县地理位置及路线前后节点估算代表点"
			}
			out = append(out, v)
		}
		return modelReply(map[string]any{"places": out}), nil
	}}
}

func TestResolveWritesCoordinatesIntoSameEntityAndPreservesGraph(t *testing.T) {
	entity := place("p1", "何家冲")
	graph := graphWith(entity)
	beforeRoutes, _ := json.Marshal(graph["routes"])
	var observed map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_ = json.Unmarshal([]byte(req.URL.Query().Get("postStr")), &observed)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": map[string]any{"infocode": "1000"}, "resultType": "1", "count": "1", "pois": []any{poi("何家冲", "河南省信阳市罗山县铁铺镇", "114.408,31.758")}})
	}))
	defer server.Close()
	r := Resolver{Key: "test-secret", Endpoint: server.URL, Model: automaticModel(t, func(stage string, _ []map[string]any) {
		if stage != "plan" {
			t.Errorf("unexpected model stage %s", stage)
		}
	})}
	if err := r.Resolve(context.Background(), graph, "红军从何家冲出发。"); err != nil {
		t.Fatal(err)
	}
	if entity["lon"] != 114.408 || entity["lat"] != 31.758 || entity["coordinate_source"] != "gazetteer" {
		t.Fatal(entity)
	}
	if entity["name"] != "何家冲" || entity["entity_id"] != "p1" {
		t.Fatal("identity changed")
	}
	if metadataOf(entity)["status"] != "resolved" || metadataOf(entity)["selection_method"] != "exact_match" {
		t.Fatal(metadataOf(entity))
	}
	if observed["queryType"] != float64(12) || observed["specify"] != "罗山县" || observed["keyWord"] != "河南省信阳市罗山县何家冲" {
		t.Fatalf("unqualified request: %#v", observed)
	}
	afterRoutes, _ := json.Marshal(graph["routes"])
	if string(beforeRoutes) != string(afterRoutes) {
		t.Fatal("routes changed")
	}
}

func TestPreservesAllPreviouslyValidCoordinateSources(t *testing.T) {
	var places []map[string]any
	for _, source := range []string{"manual", "source_text", "gazetteer", "derived_control", "llm_estimated"} {
		e := place(source, source)
		e["lon"], e["lat"], e["coordinate_source"], e["coordinate_confidence"] = 109.2, 33.1, source, 0.8
		places = append(places, e)
	}
	graph := graphWith(places...)
	before, _ := json.Marshal(graph)
	r := Resolver{}
	if err := r.Resolve(context.Background(), graph, ""); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(graph)
	if string(before) != string(after) {
		t.Fatal("previous coordinates changed")
	}
}

func TestRegionsDirectionsAndControlsReceiveEstimatedCoordinates(t *testing.T) {
	region, control, area := place("region", "桐柏山区"), place("control", "转折点"), place("area", "天水北关一带")
	region["entity_kind"], control["entity_kind"] = "region", "cartographic_control"
	var estimateCount atomic.Int32
	r := Resolver{Model: automaticModel(t, func(stage string, places []map[string]any) {
		if stage == "estimate" {
			estimateCount.Add(int32(len(places)))
		}
	})}
	if err := r.Resolve(context.Background(), graphWith(region, control, area), ""); err != nil {
		t.Fatal(err)
	}
	if estimateCount.Load() != 3 {
		t.Fatal("not all spatial entities estimated")
	}
	for _, e := range []map[string]any{region, control, area} {
		if !trustedCoordinates(e) || metadataOf(e)["status"] != "estimated" || e["coordinate_confidence"] != 0.6 {
			t.Fatal(e)
		}
	}
	if control["coordinate_source"] != "derived_control" || str(control["generation_reason"]) == "" {
		t.Fatal("control provenance missing")
	}
	if region["coordinate_source"] != "llm_estimated" {
		t.Fatal("estimate incorrectly attributed to provider")
	}
}

func TestHomonymsAutomaticallySelectedUsingContextAndNeighbors(t *testing.T) {
	server := fixtureServer(t, []map[string]any{poi("柳林", "河南省罗山县甲镇", "114.1,31.1"), poi("柳林", "河南省罗山县乙镇", "114.2,31.2")}, 2)
	defer server.Close()
	prev, target, next := place("p0", "前站"), place("p1", "柳林"), place("p2", "后站")
	for _, e := range []map[string]any{prev, next} {
		e["lon"], e["lat"], e["coordinate_source"] = 114.3, 31.7, "manual"
	}
	base := automaticModel(t, nil)
	var chose atomic.Bool
	r := Resolver{Key: "test-secret", Endpoint: server.URL, Model: fakeModel{generate: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
		if input[0].Content == selectionPrompt {
			chose.Store(true)
			if !strings.Contains(input[1].Content, "前站") || !strings.Contains(input[1].Content, "后站") || !strings.Contains(input[1].Content, "114.3") {
				t.Error("route neighbors missing")
			}
			return modelReply(map[string]any{"places": []any{map[string]any{"entity_id": "p1", "candidate_index": 1, "reason": "第二个柳林与前后站位置相符"}}}), nil
		}
		return base.generate(ctx, input)
	}}}
	if err := r.Resolve(context.Background(), graphWith(prev, target, next), "经过柳林前往后站"); err != nil {
		t.Fatal(err)
	}
	if !chose.Load() || target["lon"] != 114.2 || metadataOf(target)["selection_method"] != "model_candidate" {
		t.Fatal(target)
	}
}

func TestQualifiedSearchRetriesModernNameWithinAdministrativeArea(t *testing.T) {
	var mu sync.Mutex
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var post map[string]any
		_ = json.Unmarshal([]byte(req.URL.Query().Get("postStr")), &post)
		mu.Lock()
		queries = append(queries, str(post["keyWord"]))
		mu.Unlock()
		if post["specify"] != "罗山县" {
			t.Error("retry lost administrative scope")
		}
		if post["keyWord"] != "何家冲" {
			_, _ = w.Write([]byte(`{"status":{"infocode":3001}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": map[string]any{"infocode": 1000}, "resultType": 1, "count": 1, "pois": []any{poi("何家冲", "河南省罗山县", "114.4,31.7")}})
	}))
	defer server.Close()
	e := place("p1", "何家冲")
	r := Resolver{Key: "test-secret", Endpoint: server.URL, Model: automaticModel(t, nil)}
	if err := r.Resolve(context.Background(), graphWith(e), ""); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(queries, []string{"河南省信阳市罗山县何家冲", "何家冲"}) || e["coordinate_source"] != "gazetteer" {
		t.Fatal(queries, e)
	}
}

func TestProviderFailureOrMissingKeyFallsBackOnlyAfterSearchStage(t *testing.T) {
	cases := []struct {
		name, response string
		key            string
		status         int
	}{
		{"auth", `{"code":301013,"msg":"test-secret"}`, "test-secret", 200},
		{"bad json", `test-secret`, "test-secret", 200},
		{"http", `test-secret`, "test-secret", 500},
		{"area", `{"status":{"infocode":1000},"resultType":3,"area":{"lonlat":"110,34"}}`, "test-secret", 200},
		{"no key", ``, "", 200},
		{"no hit", `{"status":{"infocode":3001}}`, "test-secret", 200},
		{"invalid point", `{"status":{"infocode":1000},"resultType":1,"count":1,"pois":[{"name":"何家冲","lonlat":"NaN,34"}]}`, "test-secret", 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()
			e := place("p1", "何家冲")
			r := Resolver{Key: tc.key, Endpoint: server.URL, Model: automaticModel(t, func(stage string, _ []map[string]any) {
				if stage == "estimate" && tc.key != "" && requests.Load() == 0 {
					t.Error("estimated before query")
				}
			})}
			if err := r.Resolve(context.Background(), graphWith(e), ""); err != nil {
				t.Fatal(err)
			}
			if e["coordinate_source"] != "llm_estimated" || metadataOf(e)["status"] != "estimated" {
				t.Fatal(e)
			}
			encoded, _ := json.Marshal(e)
			if strings.Contains(string(encoded), "test-secret") {
				t.Fatal("key leaked")
			}
		})
	}
}

func TestInvalidEstimateRetriedAndFailureIsTransactional(t *testing.T) {
	for _, retrySuccess := range []bool{true, false} {
		t.Run(fmt.Sprint(retrySuccess), func(t *testing.T) {
			e := place("p1", "柳林")
			e["lon"] = 999.0
			graph := graphWith(e)
			before, _ := json.Marshal(graph)
			var estimates atomic.Int32
			base := automaticModel(t, nil)
			r := Resolver{Model: fakeModel{generate: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
				if input[0].Content == estimationPrompt {
					n := estimates.Add(1)
					if n == 1 || !retrySuccess {
						return modelReply(map[string]any{"places": []any{map[string]any{"entity_id": "p1", "lon": 0, "lat": 0, "confidence": 0.1, "reason": "invalid placeholder"}}}), nil
					}
				}
				return base.generate(ctx, input)
			}}}
			err := r.Resolve(context.Background(), graph, "")
			if estimates.Load() != 2 {
				t.Fatal("invalid output not retried")
			}
			if retrySuccess {
				if err != nil || !trustedCoordinates(e) {
					t.Fatal(err, e)
				}
			} else {
				after, _ := json.Marshal(graph)
				if err == nil || string(before) != string(after) {
					t.Fatal("failed run mutated graph", err)
				}
			}
		})
	}
}

func TestOneUnresolvedEntityRollsBackEvenOtherSuccessfulCoordinates(t *testing.T) {
	good, bad := place("good", "何家冲"), place("bad", "柳林")
	graph := graphWith(good, bad)
	before, _ := json.Marshal(graph)
	base := automaticModel(t, nil)
	r := Resolver{Model: fakeModel{generate: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
		if input[0].Content == estimationPrompt {
			return modelReply(map[string]any{"places": []any{map[string]any{"entity_id": "good", "lon": 114.4, "lat": 31.7, "confidence": 0.8, "reason": "known position"}}}), nil
		}
		return base.generate(ctx, input)
	}}}
	if err := r.Resolve(context.Background(), graph, ""); err == nil {
		t.Fatal("missing coordinate returned success")
	}
	after, _ := json.Marshal(graph)
	if string(before) != string(after) {
		t.Fatal("partial commit")
	}
}

func TestCancellationLeavesOriginalGraphUntouched(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-started; cancel() }()
	e := place("p1", "何家冲")
	graph := graphWith(e)
	before, _ := json.Marshal(graph)
	r := Resolver{Key: "test-secret", Endpoint: server.URL, Model: automaticModel(t, nil)}
	start := time.Now()
	err := r.Resolve(ctx, graph, "")
	if !errors.Is(err, context.Canceled) || time.Since(start) > time.Second {
		t.Fatal(err)
	}
	after, _ := json.Marshal(graph)
	if string(before) != string(after) {
		t.Fatal("cancellation changed graph")
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("request failed at %s", r.URL)
}
func TestTransportAndRedirectErrorsNeverExposeKey(t *testing.T) {
	r := Resolver{Key: "test-secret", Client: &http.Client{Transport: failingTransport{}}}
	if _, err := r.search(context.Background(), "柳林", "罗山县"); err == nil || strings.Contains(err.Error(), "test-secret") || strings.Contains(err.Error(), "https:") {
		t.Fatal(err)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("followed redirect") }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"?tk=test-secret", http.StatusFound)
	}))
	defer server.Close()
	r.Endpoint, r.Client = server.URL, nil
	if _, err := r.search(context.Background(), "柳林", "罗山县"); err == nil || strings.Contains(err.Error(), "test-secret") {
		t.Fatal(err)
	}
}

func TestBatchedPlanningEstimatesAllSeventyTwoPlaces(t *testing.T) {
	var places []map[string]any
	for i := 0; i < 72; i++ {
		places = append(places, place(fmt.Sprintf("p%d", i), fmt.Sprintf("地点%d", i)))
	}
	var planned, estimated atomic.Int32
	r := Resolver{Model: automaticModel(t, func(stage string, batch []map[string]any) {
		if len(batch) > batchSize {
			t.Errorf("oversized batch %d", len(batch))
		}
		if stage == "plan" {
			planned.Add(int32(len(batch)))
		}
		if stage == "estimate" {
			estimated.Add(int32(len(batch)))
		}
	})}
	if err := r.Resolve(context.Background(), graphWith(places...), ""); err != nil {
		t.Fatal(err)
	}
	if planned.Load() != 72 || estimated.Load() != 72 {
		t.Fatal("lost a batch")
	}
	for _, e := range places {
		if !trustedCoordinates(e) {
			t.Fatal(e)
		}
	}
}

func TestNoModelReturnsExplicitServiceErrorWithoutMutation(t *testing.T) {
	e := place("p1", "柳林")
	graph := graphWith(e)
	before, _ := json.Marshal(graph)
	r := Resolver{Key: "test-secret"}
	if err := r.Resolve(context.Background(), graph, ""); err == nil {
		t.Fatal("missing model returned success")
	}
	after, _ := json.Marshal(graph)
	if string(before) != string(after) {
		t.Fatal("mutation on error")
	}
}

func TestRepeatedPlaceQueriesReuseProviderResponse(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": map[string]any{"infocode": 1000}, "resultType": 1, "count": 1, "pois": []any{poi("何家冲", "河南省罗山县铁铺镇", "114.4,31.7")}})
	}))
	defer server.Close()
	p1, p2 := place("p1", "何家冲"), place("p2", "何家冲")
	r := Resolver{Key: "test-secret", Endpoint: server.URL, Model: automaticModel(t, nil)}
	if err := r.Resolve(context.Background(), graphWith(p1, p2), ""); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 || p1["lon"] != p2["lon"] || p1["entity_id"] == p2["entity_id"] {
		t.Fatal("query cache or entity identity is incorrect")
	}
}

func TestPlanNormalizesSingleAdministrativeScopeAndKeepsQualifiedQuery(t *testing.T) {
	for _, tc := range []struct{ province, city, county, hint, want string }{
		{"陕西省", "商洛市", "山阳县", "陕西省商洛市山阳县", "山阳县"},
		{"河南省", "信阳市", "", "河南省信阳市", "信阳市"},
		{"", "", "", "河南省信阳市罗山县", "罗山县"},
		{"", "", "", "湖北省宜昌市当阳市", "当阳市"},
		{"", "", "", "北京市", "北京市"},
		{"", "", "", "156110000", "156110000"},
	} {
		p := normalizePlan(queryPlan{ModernName: "袁家沟口村", QualifiedQuery: "陕西省商洛市山阳县袁家沟口村", Province: tc.province, City: tc.city, County: tc.county, AdministrativeHint: tc.hint}, "袁家沟口")
		if p.AdministrativeHint != tc.want || p.QualifiedQuery != "陕西省商洛市山阳县袁家沟口村" {
			t.Fatalf("bad normalization: %#v", p)
		}
	}
}

func TestExistingEstimateWithoutConfidenceIsResolvedAgain(t *testing.T) {
	e := place("p1", "何家冲")
	e["lon"], e["lat"], e["coordinate_source"] = 114.0, 31.0, "llm_estimated"
	r := Resolver{Model: automaticModel(t, nil)}
	if err := r.Resolve(context.Background(), graphWith(e), ""); err != nil {
		t.Fatal(err)
	}
	if e["coordinate_confidence"] != 0.6 || e["lon"] != 114.408 {
		t.Fatal("invalid existing estimate was preserved", e)
	}
}

func TestSameNamedDistrictInAnotherCityIsRejected(t *testing.T) {
	p := queryPlan{ModernName: "长安区", Province: "陕西省", City: "西安市", County: "长安区"}
	if candidateCoherent(Candidate{Name: "长安区", Address: "石家庄市长安区", Lon: 114.5, Lat: 38.0}, p) {
		t.Fatal("same district name in different city accepted")
	}
	if !candidateCoherent(Candidate{Name: "长安区", Address: "西安市长安区", Lon: 108.9, Lat: 34.1}, p) {
		t.Fatal("matching city rejected")
	}
	p = queryPlan{ModernName: "兴平", Province: "陕西省", City: "咸阳市", County: "兴平市"}
	if !candidateCoherent(Candidate{Name: "兴平", Address: "兴平市", Lon: 108.5, Lat: 34.3}, p) {
		t.Fatal("county-level city mistaken for wrong parent city")
	}
}

func TestContextIsBoundedAndOverlappingMentionsAreNotRepeated(t *testing.T) {
	source := strings.Repeat("甲", 80) + "何家冲附近的何家冲" + strings.Repeat("乙", 200)
	context := contextFor(source, "何家冲")
	if len([]rune(context)) > 320 || strings.Count(context, "何家冲") != 2 {
		t.Fatal("source context was duplicated or exceeded input budget")
	}
	w := &placeWork{entity: place("p1", "何家冲"), meta: metadata("何家冲"), candidates: []Candidate{{Name: "何家冲", Address: "罗山县", Lon: 114.4, Lat: 31.7, SourceID: "unique-provider-candidate"}}}
	w.meta["candidates"] = w.candidates
	encoded, _ := json.Marshal(w.modelInput(true))
	if strings.Count(string(encoded), "unique-provider-candidate") != 1 {
		t.Fatal("candidate repeated in model input")
	}
}

func TestModelPhaseCancellationLeavesGlobalContextAvailableForEstimation(t *testing.T) {
	base := automaticModel(t, nil)
	r := Resolver{Model: fakeModel{generate: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
		if input[0].Content == plannerPrompt {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return base.generate(ctx, input)
	}}}
	w := &placeWork{entity: place("p1", "何家冲"), meta: metadata("何家冲")}
	global, cancelGlobal := context.WithTimeout(context.Background(), time.Second)
	defer cancelGlobal()
	phase, cancelPhase := context.WithTimeout(global, 10*time.Millisecond)
	r.plan(phase, []*placeWork{w})
	cancelPhase()
	if global.Err() != nil {
		t.Fatal("planning timeout cancelled overall resolution")
	}
	r.estimate(global, []*placeWork{w})
	if !w.done || !trustedCoordinates(w.entity) {
		t.Fatal("fallback was unavailable after phase timeout")
	}
	if planningBudget+searchBudget+selectionBudget > ResolutionTimeout-90*time.Second {
		t.Fatal("phase limits consume estimation reserve")
	}
}
