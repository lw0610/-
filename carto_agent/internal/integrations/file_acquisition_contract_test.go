package integrations

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/fileops"
	"github.com/autocarto/carto_agent/internal/skill"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
	"github.com/autocarto/carto_agent/internal/toolsession"
)

// The source and its transcription are fixtures, not a claim about model OCR.
// All file tools, their shipped schemas, acquisition queries, table parsing,
// materialization, registration receipts and hash verification are production
// code. No model, network or QGIS normalization stub can create false success.
func TestFileToolsProduceVerifiedTableForExactAcquisitionQuery(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	project := filepath.Join(t.TempDir(), "project.qgz")
	const sessionID = "file-acquisition-contract"
	sessionDir, err := config.ResolveSessionDir(project, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(sessionDir, "output"), 0700); err != nil {
		t.Fatal(err)
	}
	catalog := &datasource.Catalog{
		Categories: []datasource.Category{{ID: "stats.fixture", Name: "统计指标", Geometry: "table"}},
		Sources:    []datasource.Source{{ID: "portal.fixture", Name: "Fixture portal", Kind: "portal", Status: "experimental", Provides: []datasource.Provide{{Category: "stats.fixture"}}}},
	}
	store := acquire.NewStore(filepath.Join(t.TempDir(), "cache"), 0)
	// Intentionally leave Invoke nil: a nonspatial table must be usable without
	// invoking a spatial QGIS handler that rejects CSVs without geometry/CRS.
	acquisition := &acquire.Engine{Catalog: catalog, Store: store, Config: config.AcquisitionConfig{}.WithDefaults()}
	service := dataservice.New(dataservice.Options{
		SessionID: sessionID, SessionDir: sessionDir, Engine: acquisition,
		Environment: datasource.PlanningEnvironment{Catalog: catalog, AutoAcquire: "allow", Integrations: map[string]bool{"browser": true, "web": true}},
		Config:      config.AcquisitionConfig{NetworkPolicy: "direct", FallbackPolicy: "allow"},
	})
	_, sourceFile, _, _ := runtime.Caller(0)
	assets := filepath.Join(filepath.Dir(sourceFile), "../../..", "autocarto")
	tools := toolpkg.NewRegistry()
	if err = tools.LoadDirectories(filepath.Join(assets, "tools/system/file-operations"), filepath.Join(assets, "tools/system/data-service")); err != nil {
		t.Fatal(err)
	}
	registry := skill.NewRegistry(tools)
	if err = registry.LoadDirectories(filepath.Join(assets, "skills/system/file-operations"), filepath.Join(assets, "skills/system/data-service")); err != nil {
		t.Fatal(err)
	}
	engine := skill.NewEngine(registry, skill.NewInternalBackend(func(ctx context.Context, name string, args map[string]any) (any, error) {
		switch {
		case fileops.Handles(name):
			return fileops.Invoke(ctx, name, args)
		case dataservice.Handles(name):
			return dataservice.Invoke(ctx, name, args)
		default:
			return nil, fmt.Errorf("unexpected external tool: %s", name)
		}
	}))
	base := dataservice.WithService(toolsession.WithContext(context.Background(), sessionID, project, ""), service)
	request := dataservice.FindRequest{Need: dataservice.Need{Name: "测试指标", What: dataservice.What{Category: "stats.fixture"}, Extent: "bbox:0,0,1,1", Scale: "auto", Fields: map[string]string{"region": "region", "year": "year", "value": "value"}}, IncludeFallback: true}
	find := func() (dataservice.FindResult, dataservice.Selection) {
		t.Helper()
		found, err := service.Find(base, request)
		if err != nil || found.State != "exploration_required" {
			t.Fatalf("fixture query did not need discovery: %+v %v", found, err)
		}
		selected, err := service.ValidateChoice(base, found.FindID, nil)
		if err != nil || !selected.RequiresDiscovery() || selected.Rows[0].Candidate.Kind != "portal" {
			t.Fatalf("portal not pinned: %+v %v", selected, err)
		}
		return found, selected
	}
	first, firstSelection := find()
	second, secondSelection := find()
	if first.FindID == second.FindID {
		t.Fatal("queries unexpectedly share an identifier")
	}
	ctx := dataservice.WithAcquisitionSelection(base, firstSelection)
	rawSource := "地区\t年份\t指标\n甲\t2024\t42\n乙\t2024\t31\n"
	input := filepath.Join(sessionDir, "output", "source.txt")
	if err = os.WriteFile(input, []byte(rawSource), 0600); err != nil {
		t.Fatal(err)
	}
	read, err := engine.Execute(ctx, "file_read", map[string]any{"path": input})
	if err != nil {
		t.Fatal(err)
	}
	text := read.Output.(map[string]any)["content"].(string)
	reader := csv.NewReader(strings.NewReader(text))
	reader.Comma = '\t'
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rows, [][]string{{"地区", "年份", "指标"}, {"甲", "2024", "42"}, {"乙", "2024", "31"}}) {
		t.Fatalf("source text changed: %#v", rows)
	}
	rows[0] = []string{"region", "year", "value"}
	var csvText strings.Builder
	writer := csv.NewWriter(&csvText)
	if err = writer.WriteAll(rows); err != nil {
		t.Fatal(err)
	}
	written, err := engine.Execute(ctx, "file_write", map[string]any{"path": "result.csv", "content": csvText.String()})
	if err != nil {
		t.Fatal(err)
	}
	path := written.Output.(map[string]any)["path"].(string)
	registerArgs := map[string]any{"find_id": first.FindID, "row": request.Need.Name, "path": path, "kind": "portal", "url": "https://example.test/official/table", "license": "fixture only", "attribution": "synthetic integration test", "notes": []string{"原始表为测试夹具；不代表真实统计数据"}}
	registered, err := engine.Execute(ctx, "data_register", registerArgs)
	if err != nil {
		t.Fatalf("real CSV registration failed: %v", err)
	}
	var result dataservice.GetResult
	encoded, _ := json.Marshal(registered.Output)
	if err = json.Unmarshal(encoded, &result); err != nil || result.Status != "completed" {
		t.Fatalf("registration did not complete: %s %v", encoded, err)
	}
	verified, err := service.GetSelection(ctx, firstSelection, dataservice.GetRequest{FindID: first.FindID})
	if err != nil || verified.Status != "completed" || len(verified.Rows) != 1 || verified.Rows[0].FindID != first.FindID {
		t.Fatalf("exact-query receipt rejected: %+v %v", verified, err)
	}
	row := verified.Rows[0]
	if row.Count != 2 || row.Kind != "portal" || row.Attribution != "synthetic integration test" {
		t.Fatalf("registration lost table/source evidence: %+v", row)
	}
	for _, output := range row.Paths {
		data, err := os.ReadFile(output)
		if err != nil || string(data) != csvText.String() || filepath.Ext(output) != ".csv" {
			t.Fatalf("registered bytes changed: %s %q %v", output, data, err)
		}
		hash := sha256.Sum256(data)
		if row.SHA256[output] != hex.EncodeToString(hash[:]) {
			t.Fatal("registered output hash does not match real bytes")
		}
	}
	record, found, err := store.Lookup(ctx, row.StoreKey)
	if err != nil || !found || len(record.Raw()) != 1 {
		t.Fatalf("source original not retained in verified store: %+v %v", record, err)
	}
	original, err := os.ReadFile(record.Raw()[0])
	if err != nil || string(original) != csvText.String() {
		t.Fatalf("source CSV altered in storage: %q %v", original, err)
	}
	sourceHash := sha256.Sum256(original)
	if record.Spec.SourceVersion != hex.EncodeToString(sourceHash[:]) {
		t.Fatal("source hash is not bound to materialized provenance")
	}
	otherCtx := dataservice.WithAcquisitionSelection(base, secondSelection)
	if _, err = service.GetSelection(otherCtx, secondSelection, dataservice.GetRequest{FindID: second.FindID}); err == nil || !strings.Contains(err.Error(), "DATA_DISCOVERY_INCOMPLETE") {
		t.Fatalf("another query reused this receipt: %v", err)
	}
	// This is only a claimed path; a query without a matching registration
	// receipt stays incomplete, and a changed artifact invalidates its receipt.
	if err = os.WriteFile(row.Paths[0], []byte("changed after registration"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = service.GetSelection(ctx, firstSelection, dataservice.GetRequest{FindID: first.FindID}); err == nil || !strings.Contains(err.Error(), "DATA_REGISTER_MISMATCH") {
		t.Fatalf("tampered table accepted: %v", err)
	}
}
