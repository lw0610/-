package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/autocarto/carto_agent/internal/protocol"
	"github.com/gorilla/mux"
)

type knowledgeCoordinatesSessionService struct {
	*inMemorySessionService
	store   *knowledge.ReviewStore
	geocode func(context.Context, string, string, int64) (*knowledge.Review, error)
}

func (s *knowledgeCoordinatesSessionService) KnowledgeReviewStore() *knowledge.ReviewStore {
	return s.store
}

func (s *knowledgeCoordinatesSessionService) GeocodeKnowledgeReview(ctx context.Context, sid, rid string, revision int64) (*knowledge.Review, error) {
	return s.geocode(ctx, sid, rid, revision)
}

func newKnowledgeCoordinatesAPI(t *testing.T) (*mux.Router, *knowledgeCoordinatesSessionService, *knowledge.Review) {
	t.Helper()
	source := "部队到达甲地。"
	digest := sha256.Sum256([]byte(source))
	graph := knowledge.Graph{
		"spatial_entities": []any{map[string]any{"entity_id": "p1", "name": "甲地", "entity_kind": "place"}},
		"routes":           []any{map[string]any{"route_id": "r1", "name": "原路线"}},
	}
	store := knowledge.NewReviewStore(t.TempDir())
	review, err := store.Create(knowledge.ReviewCreateInput{
		SessionID: "session-geocoding", SourceText: source,
		Contract: knowledge.Contract{
			ContractID: "v1", SourceSHA256: hex.EncodeToString(digest[:]), JSONSchema: json.RawMessage(`{}`),
		},
		OriginalResult: &knowledge.Result{Graph: graph}, Graph: graph,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := &knowledgeCoordinatesSessionService{inMemorySessionService: newInMemorySessionService(), store: store}
	service.geocode = func(context.Context, string, string, int64) (*knowledge.Review, error) {
		t.Fatal("unexpected geocoding service call")
		return nil, nil
	}
	handler := NewAPI(nil, nil, service, service, "", nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)
	return router, service, review
}

func requestKnowledgeCoordinates(router *mux.Router, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(httpContractVersionHeader, protocol.ContractVersionV1)
	request.Header.Set("X-Correlation-ID", "corr-test-1")
	request.Header.Set("X-Evidence-ID", "evid-test-1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func decodeKnowledgeCoordinatesView(t *testing.T, response *httptest.ResponseRecorder) knowledge.ReviewView {
	t.Helper()
	var envelope struct {
		Success bool                 `json:"success"`
		Data    knowledge.ReviewView `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if response.Code == http.StatusOK && !envelope.Success {
		t.Fatalf("response has success=false: %s", response.Body.String())
	}
	return envelope.Data
}

func TestGeocodeKnowledgeReviewReturnsAndPersistsCoordinates(t *testing.T) {
	router, service, before := newKnowledgeCoordinatesAPI(t)
	calls := 0
	service.geocode = func(ctx context.Context, sid, rid string, revision int64) (*knowledge.Review, error) {
		calls++
		if sid != before.SessionID || rid != before.ReviewID || revision != before.Revision {
			t.Fatalf("incorrect routed request: %q %q %d", sid, rid, revision)
		}
		return service.store.ResolveDraftCoordinates(ctx, sid, rid, revision, func(_ context.Context, graph map[string]any, source string) error {
			if source != before.SourceText {
				t.Fatalf("wrong source text %q", source)
			}
			place := graph["spatial_entities"].([]any)[0].(map[string]any)
			place["lon"], place["lat"] = 109.8, 33.5
			place["coordinate_source"] = "gazetteer"
			place["geocoding"] = map[string]any{"provider": "tianditu", "status": "resolved", "query": "某县甲地"}
			return nil
		})
	}
	path := "/api/sessions/" + before.SessionID + "/knowledge-reviews/" + before.ReviewID
	response := requestKnowledgeCoordinates(router, http.MethodPost, path+"/geocode", fmt.Sprintf(`{"base_revision":%d}`, before.Revision))
	if response.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", response.Code, response.Body.String())
	}
	view := decodeKnowledgeCoordinatesView(t, response)
	place := view.Graph["spatial_entities"].([]any)[0].(map[string]any)
	if place["lon"] != 109.8 || place["lat"] != 33.5 || place["coordinate_source"] != "gazetteer" || place["name"] != "甲地" {
		t.Fatalf("resolved coordinates missing from displayed graph: %#v", place)
	}
	if place["geocoding"].(map[string]any)["provider"] != "tianditu" || view.Revision != before.Revision+1 || view.ReviewID != before.ReviewID {
		t.Fatalf("updated provenance or revision missing: %#v", view)
	}
	if !reflect.DeepEqual(view.Graph["routes"], before.Graph["routes"]) {
		t.Fatal("geocoding changed routes")
	}
	read := requestKnowledgeCoordinates(router, http.MethodGet, path, "")
	if read.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", read.Code, read.Body.String())
	}
	if saved := decodeKnowledgeCoordinatesView(t, read); !reflect.DeepEqual(saved.Graph, view.Graph) || saved.Revision != view.Revision {
		t.Fatal("subsequent GET did not return the enriched review")
	}
	if calls != 1 {
		t.Fatalf("geocoder called %d times", calls)
	}
	var wire map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	data := wire["data"].(map[string]any)
	for _, privateField := range []string{"contract", "original_result", "compile_args", "resume"} {
		if _, exists := data[privateField]; exists {
			t.Fatalf("internal review field exposed: %s", privateField)
		}
	}
}

func TestGeocodeKnowledgeReviewStaleRevisionPreservesNewerGraph(t *testing.T) {
	router, service, before := newKnowledgeCoordinatesAPI(t)
	manual, err := service.store.Get(before.SessionID, before.ReviewID)
	if err != nil {
		t.Fatal(err)
	}
	place := manual.Graph["spatial_entities"].([]any)[0].(map[string]any)
	place["name"], place["lon"], place["lat"], place["coordinate_source"] = "人工修正地点", 110.2, 34.2, "manual"
	latest, err := service.store.SaveDraft(before.SessionID, before.ReviewID, before.Revision, manual.Graph)
	if err != nil {
		t.Fatal(err)
	}
	service.geocode = func(ctx context.Context, sid, rid string, revision int64) (*knowledge.Review, error) {
		return service.store.ResolveDraftCoordinates(ctx, sid, rid, revision, func(context.Context, map[string]any, string) error {
			t.Fatal("stale request must not spend a lookup")
			return nil
		})
	}
	path := "/api/sessions/" + before.SessionID + "/knowledge-reviews/" + before.ReviewID + "/geocode"
	response := requestKnowledgeCoordinates(router, http.MethodPost, path, fmt.Sprintf(`{"base_revision":%d}`, before.Revision))
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	payload, envelope := decodeEnvelopeResponse(t, response)
	assertEnvelopeResponse(t, payload, envelope, "KNOWLEDGE_REVIEW_CONFLICT", "knowledge_review", "review")
	view := decodeKnowledgeCoordinatesView(t, response)
	if view.Revision != latest.Revision || !reflect.DeepEqual(view.Graph, latest.Graph) {
		t.Fatal("conflict response did not include the latest review")
	}
	persisted, err := service.store.Get(before.SessionID, before.ReviewID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Revision != latest.Revision || !reflect.DeepEqual(persisted.Graph, latest.Graph) {
		t.Fatal("stale lookup overwrote the newer manual edit")
	}
}

func TestGeocodeKnowledgeReviewRejectsInvalidRequests(t *testing.T) {
	for _, body := range []string{
		`{`,
		`{"base_revision":1,"graph":{}}`,
		`{"base_revision":1} {"base_revision":1}`,
		`{"base_revision":"1"}`,
		`[]`,
	} {
		t.Run(body, func(t *testing.T) {
			router, service, before := newKnowledgeCoordinatesAPI(t)
			path := "/api/sessions/" + before.SessionID + "/knowledge-reviews/" + before.ReviewID + "/geocode"
			response := requestKnowledgeCoordinates(router, http.MethodPost, path, body)
			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			payload, envelope := decodeEnvelopeResponse(t, response)
			assertEnvelopeResponse(t, payload, envelope, "KNOWLEDGE_REVIEW_INVALID", "knowledge_review", "review")
			latest, err := service.store.Get(before.SessionID, before.ReviewID)
			if err != nil {
				t.Fatal(err)
			}
			if latest.Revision != before.Revision || !reflect.DeepEqual(latest.Graph, before.Graph) {
				t.Fatal("invalid request changed the review")
			}
		})
	}
}
