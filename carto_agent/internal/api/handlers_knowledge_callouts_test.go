package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/gorilla/mux"
)

type knowledgeCalloutSessionService struct {
	*knowledgeCoordinatesSessionService
	resolve func(context.Context, map[string]any, string) error
}

func (s *knowledgeCalloutSessionService) ResolveKnowledgeGraphCoordinates(ctx context.Context, graph map[string]any, source string) error {
	return s.resolve(ctx, graph, source)
}

func calloutAPIWorkbook(t *testing.T) string {
	t.Helper()
	files := map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="urn:relationships"><sheets><sheet name="图文标注" r:id="sheet"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="sheet" Type="urn:relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   `<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>地点</t></is></c><c r="B1" t="inlineStr"><is><t>文本</t></is></c><c r="C1" t="inlineStr"><is><t>图片</t></is></c></row><row r="2"><c r="A2" t="inlineStr"><is><t>甲地（河南省）</t></is></c><c r="B2" t="inlineStr"><is><t>正文甲</t></is></c><c r="C2" t="inlineStr"><is><t>说明保留在来源</t></is></c></row><row r="3"><c r="A3" t="inlineStr"><is><t>枣阳—保安寨（湖北省）</t></is></c><c r="B3" t="inlineStr"><is><t>复合地点的定位上下文。</t></is></c></row></sheetData></worksheet>`,
	}
	var content bytes.Buffer
	archive := zip.NewWriter(&content)
	for name, body := range files {
		file, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "notes.xlsx")
	if err := os.WriteFile(path, content.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func newCalloutImportAPI(t *testing.T) (*mux.Router, *knowledgeCalloutSessionService, *knowledge.Review) {
	t.Helper()
	_, base, review := newKnowledgeCoordinatesAPI(t)
	service := &knowledgeCalloutSessionService{knowledgeCoordinatesSessionService: base}
	service.resolve = func(context.Context, map[string]any, string) error {
		return errors.New("unexpected coordinate resolver")
	}
	router := mux.NewRouter()
	NewAPI(nil, nil, service, service, "", nil).SetupRoutes(router)
	return router, service, review
}

func TestImportKnowledgeCalloutsReturnsSavedDraftWithResolvedLocations(t *testing.T) {
	router, service, before := newCalloutImportAPI(t)
	before.Graph["routes"].([]any)[0].(map[string]any)["name"] = "未保存的路线修改"
	calls := 0
	service.resolve = func(ctx context.Context, graph map[string]any, source string) error {
		calls++
		if !strings.Contains(source, "枣阳—保安寨（湖北省）") || !strings.Contains(source, "复合地点的定位上下文") {
			t.Fatal("workbook qualifiers and body did not reach normal geocoder")
		}
		current, err := service.store.Get(before.SessionID, before.ReviewID)
		if err != nil || current.Revision != before.Revision || current.Graph["callouts"] != nil {
			t.Fatal("import persisted a draft before resolving coordinates")
		}
		for _, raw := range graph["spatial_entities"].([]any) {
			place := raw.(map[string]any)
			place["lon"], place["lat"], place["coordinate_source"] = 111.0, 32.0, "gazetteer"
		}
		return nil
	}
	payload, _ := json.Marshal(map[string]any{"base_revision": before.Revision, "source_path": calloutAPIWorkbook(t), "graph": before.Graph})
	path := "/api/sessions/" + before.SessionID + "/knowledge-reviews/" + before.ReviewID + "/callouts/import"
	response := requestKnowledgeCoordinates(router, http.MethodPost, path, string(payload))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Success bool `json:"success"`
		Data    struct {
			Review        knowledge.ReviewView `json:"review"`
			ImportedCount int                  `json:"imported_count"`
			UpdatedCount  int                  `json:"updated_count"`
			ImageCount    int                  `json:"image_count"`
			NewPlaceCount int                  `json:"new_place_count"`
			Warnings      []string             `json:"warnings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	data := envelope.Data
	if !envelope.Success || calls != 1 || data.ImportedCount != 2 || data.UpdatedCount != 0 || data.ImageCount != 0 || data.NewPlaceCount != 1 || data.Warnings == nil || len(data.Warnings) != 0 || data.Review.Revision != before.Revision+1 {
		t.Fatalf("wrong import response: %s", response.Body.String())
	}
	persisted, err := service.store.Get(before.SessionID, before.ReviewID)
	if err != nil || !reflect.DeepEqual(persisted.Graph, data.Review.Graph) || !reflect.DeepEqual(persisted.Graph["routes"], before.Graph["routes"]) || persisted.SourceText != before.SourceText {
		t.Fatal("response did not preserve saved content and unsaved route edit")
	}
	if len(persisted.CalloutImports) != 2 || len(data.Review.CalloutImports) != 2 {
		t.Fatal("row source metadata absent")
	}
}

func TestImportKnowledgeCalloutsLocationFailureLeavesReviewUnchanged(t *testing.T) {
	router, service, before := newCalloutImportAPI(t)
	service.resolve = func(context.Context, map[string]any, string) error { return errors.New("定位模型不可用") }
	payload, _ := json.Marshal(map[string]any{"base_revision": before.Revision, "source_path": calloutAPIWorkbook(t)})
	path := "/api/sessions/" + before.SessionID + "/knowledge-reviews/" + before.ReviewID + "/callouts/import"
	response := requestKnowledgeCoordinates(router, http.MethodPost, path, string(payload))
	if response.Code == http.StatusOK || !strings.Contains(response.Body.String(), "定位模型不可用") || !strings.Contains(response.Body.String(), "草稿未修改") {
		t.Fatalf("location failure concealed: %d %s", response.Code, response.Body.String())
	}
	latest, err := service.store.Get(before.SessionID, before.ReviewID)
	if err != nil || latest.Revision != before.Revision || !reflect.DeepEqual(latest.Graph, before.Graph) || len(latest.CalloutImports) != 0 {
		t.Fatal("failed import changed persisted review")
	}
}

func TestImportKnowledgeCalloutsValidatesContractAndRevision(t *testing.T) {
	for _, body := range []string{`{`, `{"base_revision":1,"source_path":"/bad.xls","unexpected":true}`, `{"base_revision":1,"source_path":"relative.xlsx"}`, `{"base_revision":99,"source_path":"/missing.xlsx"}`} {
		t.Run(body, func(t *testing.T) {
			router, service, before := newCalloutImportAPI(t)
			path := fmt.Sprintf("/api/sessions/%s/knowledge-reviews/%s/callouts/import", before.SessionID, before.ReviewID)
			response := requestKnowledgeCoordinates(router, http.MethodPost, path, body)
			want := http.StatusUnprocessableEntity
			if strings.Contains(body, `"base_revision":99`) {
				want = http.StatusConflict
			}
			if response.Code != want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, want, response.Body.String())
			}
			latest, err := service.store.Get(before.SessionID, before.ReviewID)
			if err != nil || latest.Revision != before.Revision || !reflect.DeepEqual(latest.Graph, before.Graph) {
				t.Fatal("invalid request changed review")
			}
		})
	}
}
