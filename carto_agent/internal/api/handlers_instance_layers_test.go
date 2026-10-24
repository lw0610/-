package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/discovery"
	bridgeprovider "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/gorilla/mux"
)

func TestExtractInstanceLayerItems(t *testing.T) {
	for _, input := range []string{
		`{"success":true,"info":{"layers":[{"id":"road-1","name":"道路","fields":[{"name":"不能作为图层"}]}]}}`,
		`{"data":{"info":{"layers":[{"id":"road-1","name":"道路"}]}}}`,
		`{"result":{"data":{"layers":[{"id":"road-1","name":"道路"}]}}}`,
		`{"result":"{\"info\":{\"layers\":[{\"id\":\"road-1\",\"name\":\"道路\"}]}}"}`,
		`{"data":[{"id":"road-1","name":"道路"}]}`,
	} {
		got, err := extractInstanceLayerItems(input)
		if err != nil || !reflect.DeepEqual(got, []instanceLayerItem{{ID: "road-1", Name: "道路"}}) {
			t.Fatalf("input=%s got=%#v err=%v", input, got, err)
		}
	}
	for _, input := range []string{
		`{"info":{"fields":[{"id":"field-1","name":"字段"}]}}`,
		`{"data":{"fields":[{"id":"field-1","name":"字段"}]}}`,
		`{"other":{"layers":[{"id":"nested-1","name":"错误路径"}]}}`,
		`{"info":{"layers":[{"fields":[{"id":"field-1","name":"字段"}]}]}}`,
		`{"info":{"layers":[{"name":"无ID"}]}}`,
		`{"info":{"layers":null}}`,
		`{"success":false,"info":{"layers":[]}}`,
		`{"status":"error","data":{"layers":[]}}`,
		`{"name":"无列表"}`,
		`not json`,
	} {
		if items, err := extractInstanceLayerItems(input); err == nil {
			t.Fatalf("must reject malformed/irrelevant result: %s => %#v", input, items)
		}
	}
	empty, err := extractInstanceLayerItems(`{"info":{"layers":[]}}`)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty list must remain an empty JSON array: %#v %v", empty, err)
	}
	items, err := extractInstanceLayerItems(`{"info":{"layers":[{"id":"a","name":"同名图层"},{"id":"a","name":"重复ID"},{"id":"b","name":"同名图层"}]}}`)
	if err != nil || !reflect.DeepEqual(items, []instanceLayerItem{{ID: "a", Name: "同名图层"}, {ID: "b", Name: "同名图层"}}) {
		t.Fatalf("deduplicate IDs, retain distinct layers with equal names: %#v %v", items, err)
	}
}

type layerPolicySpy struct {
	stubInstanceAssetCatalogService
	tool string
}

func (s *layerPolicySpy) ResolveToolAccessPolicy(_ context.Context, tool string) (string, []string, bool, error) {
	s.tool = tool
	return s.visibility, s.scopes, s.known, s.err
}

func TestInstanceLayersEndpointUsesReadOnlyBridgeInvocation(t *testing.T) {
	registry, provider, conn, cleanup := setupInstanceActionBridge(t, "layer-instance", nil)
	defer cleanup()
	policy := &layerPolicySpy{stubInstanceAssetCatalogService: stubInstanceAssetCatalogService{visibility: "public", scopes: []string{"tool.invoke"}, known: true}}
	router := buildInstanceTestRouterWithLocalBridgeAndAssetCatalog(t, registry, provider, policy)
	response := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/instances/layer-instance/layers", nil))
		response <- rr
	}()
	message := readInstanceActionWSMessage(t, conn)
	if message.Type != "tool_invoke" || message.Data["tool_name"] != "get_layers_info" {
		t.Fatalf("unexpected bridge request: %#v", message)
	}
	params := message.Data["params"].(map[string]any)
	if params["target_instance_id"] != "layer-instance" || params["include_invisible"] != true {
		t.Fatalf("must target requested instance and include all layers: %#v", params)
	}
	sendInstanceActionWSMessage(t, conn, instanceActionWSMessage{Type: "tool_result", Data: map[string]any{
		"request_id": message.Data["request_id"], "success": true,
		"output": map[string]any{"success": true, "info": map[string]any{"layers": []any{map[string]any{"id": "roads-1", "name": "道路", "fields": []any{map[string]any{"name": "字段不得外泄为图层"}}}}}},
	}})
	select {
	case rr := <-response:
		if rr.Code != http.StatusOK {
			t.Fatalf("GET layers: %d %s", rr.Code, rr.Body.String())
		}
		var body struct {
			Success bool `json:"success"`
			Data    struct {
				InstanceID string              `json:"instance_id"`
				Items      []instanceLayerItem `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil || !body.Success || body.Data.InstanceID != "layer-instance" || !reflect.DeepEqual(body.Data.Items, []instanceLayerItem{{ID: "roads-1", Name: "道路"}}) {
			t.Fatalf("invalid layer response: %s error=%v", rr.Body.String(), err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("layers response timed out")
	}
	if policy.tool != "get_layers_info" {
		t.Fatalf("authorization bypassed or wrong tool: %q", policy.tool)
	}
}

func TestInstanceLayersEndpointRejectsUnavailableInstance(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	registry := discovery.NewInstanceRegistry()
	defer registry.Close()
	if err := registry.Register(discovery.RegisterRequest{InstanceID: "disconnected", PID: 1001}); err != nil {
		t.Fatal(err)
	}
	provider := bridgeprovider.NewProvider(discovery.ServiceNameQGISBridge, registry)
	defer provider.Close()
	for _, test := range []struct {
		name string
		api  *API
		path string
		code int
	}{
		{"empty", &API{instanceRegistry: registry}, "/api/instances/%20/layers", http.StatusBadRequest},
		{"unknown", &API{instanceRegistry: registry}, "/api/instances/missing/layers", http.StatusNotFound},
		{"registry missing", &API{}, "/api/instances/one/layers", http.StatusServiceUnavailable},
		{"provider missing", &API{instanceRegistry: registry}, "/api/instances/disconnected/layers", http.StatusServiceUnavailable},
		{"bridge disconnected", &API{instanceRegistry: registry, localBridgeProvider: provider}, "/api/instances/disconnected/layers", http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := mux.NewRouter()
			router.HandleFunc("/api/instances/{id}/layers", test.api.handleGetInstanceLayers)
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, test.path, nil))
			if rr.Code != test.code {
				t.Fatalf("got %d want %d: %s", rr.Code, test.code, rr.Body.String())
			}
		})
	}
}

func TestInstanceLayersEndpointHonorsGoPolicyBeforeDispatch(t *testing.T) {
	for _, test := range []struct {
		name   string
		policy stubInstanceAssetCatalogService
		code   int
	}{
		{"hidden", stubInstanceAssetCatalogService{visibility: "hidden", known: true}, http.StatusForbidden},
		{"missing scope", stubInstanceAssetCatalogService{visibility: "public", known: true, scopes: []string{"catalog.read"}}, http.StatusForbidden},
		{"unknown", stubInstanceAssetCatalogService{known: false}, http.StatusForbidden},
		{"policy unavailable", stubInstanceAssetCatalogService{err: errors.New("offline")}, http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry, provider, conn, cleanup := setupInstanceActionBridge(t, "policy-instance", nil)
			defer cleanup()
			router := buildInstanceTestRouterWithLocalBridgeAndAssetCatalog(t, registry, provider, test.policy)
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/instances/policy-instance/layers", nil))
			if rr.Code != test.code {
				t.Fatalf("got %d want %d: %s", rr.Code, test.code, rr.Body.String())
			}
			_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
			if _, _, err := conn.ReadMessage(); err == nil {
				t.Fatal("policy denial must happen before tool invocation")
			}
		})
	}
}

func TestInstanceLayersEndpointRejectsFailedAndMalformedResults(t *testing.T) {
	for _, result := range []map[string]any{
		{"success": false, "error": "读取失败", "error_code": "TOOL_FAILED"},
		{"success": true, "output": map[string]any{"info": map[string]any{"fields": []any{map[string]any{"id": "field", "name": "字段"}}}}},
	} {
		registry, provider, conn, cleanup := setupInstanceActionBridge(t, "invalid-result", nil)
		router := buildInstanceTestRouterWithLocalBridge(t, registry, provider)
		done := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/instances/invalid-result/layers", nil))
			done <- rr
		}()
		request := readInstanceActionWSMessage(t, conn)
		result["request_id"] = request.Data["request_id"]
		sendInstanceActionWSMessage(t, conn, instanceActionWSMessage{Type: "tool_result", Data: result})
		select {
		case rr := <-done:
			if rr.Code != http.StatusBadGateway || decodeAPIResponse(t, rr).Success {
				t.Fatalf("failed/malformed tool response must fail honestly: %d %s", rr.Code, rr.Body.String())
			}
		case <-time.After(3 * time.Second):
			t.Fatal("failed result response timed out")
		}
		cleanup()
	}
}
