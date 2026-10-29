package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/sessionfiles"
	"github.com/gorilla/mux"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func sessionFilesFixture(t *testing.T) (*mux.Router, *sessionfiles.Store, string) {
	t.Helper()
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	project := filepath.Join(t.TempDir(), "map.qgz")
	svc := newInMemorySessionService()
	if e := svc.SaveSession(map[string]any{"session_id": "files-test", "bound_instance_id": "bound-only"}, project); e != nil {
		t.Fatal(e)
	}
	a := &API{sessionService: svc}
	router := mux.NewRouter()
	a.SetupRoutes(router)
	store, e := sessionfiles.New(project, "files-test")
	if e != nil {
		t.Fatal(e)
	}
	return router, store, project
}
func fileFixture(t *testing.T, s *sessionfiles.Store, name string, data []byte) sessionfiles.Item {
	t.Helper()
	path := filepath.Join(s.Dir, "output", name)
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	item, e := s.Register(path, sessionfiles.Origin{Type: "test"})
	if e != nil {
		t.Fatal(e)
	}
	return item
}
func fileRequest(router *mux.Router, project, id, action string) *httptest.ResponseRecorder {
	method := "GET"
	if action != "content" {
		method = "POST"
	}
	req := httptest.NewRequest(method, "/api/sessions/files-test/files/"+id+"/"+action+"?project_path="+url.QueryEscape(project), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}
func TestSessionFileContentBoundariesAndHeaders(t *testing.T) {
	router, store, project := sessionFilesFixture(t)
	for _, name := range []string{"map.png", "map.pdf", "notes.md", "style.qml"} {
		item := fileFixture(t, store, name, []byte("fixture"))
		w := fileRequest(router, project, item.ID, "content")
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body.String())
		}
		for key, want := range map[string]string{"X-Content-Type-Options": "nosniff", "Content-Security-Policy": "default-src 'none'", "Cache-Control": "private"} {
			if w.Header().Get(key) != want {
				t.Fatalf("missing %s", key)
			}
		}
	}
	for _, name := range []string{"unsafe.html", "script.py", "unknown.bin"} {
		item := fileFixture(t, store, name, []byte("blocked"))
		if w := fileRequest(router, project, item.ID, "content"); w.Code != 415 {
			t.Fatalf("%s: %d", name, w.Code)
		}
	}
	if w := fileRequest(router, project, "f_unknown", "content"); w.Code != 404 {
		t.Fatal(w.Code)
	}
	item := fileFixture(t, store, "changed.txt", []byte("safe"))
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("secret"), 0600)
	os.Remove(item.Path)
	os.Symlink(outside, item.Path)
	if w := fileRequest(router, project, item.ID, "content"); w.Code != 404 || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("escaped: %s", w.Body.String())
	}
}
func TestSessionFileSizeEncodingTablesAndArchive(t *testing.T) {
	router, store, project := sessionFilesFixture(t)
	for _, test := range []struct {
		name string
		size int64
		code int
	}{{"large.png", (50 << 20) + 1, 413}, {"large.csv", (5 << 20) + 1, 413}, {"large.txt", (1 << 20) + 10, 200}} {
		item := fileFixture(t, store, test.name, []byte("x"))
		os.Truncate(item.Path, test.size)
		w := fileRequest(router, project, item.ID, "content")
		if w.Code != test.code {
			t.Fatalf("%s %d", test.name, w.Code)
		}
		if test.name == "large.txt" && (w.Body.Len() != 1<<20 || w.Header().Get("X-Preview-Truncated") != "1") {
			t.Fatal("text limit missing")
		}
	}
	gbk, _ := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("中文文本"))
	item := fileFixture(t, store, "gbk.txt", gbk)
	if w := fileRequest(router, project, item.ID, "content"); w.Body.String() != "中文文本" {
		t.Fatal(w.Body.String())
	}
	csv := "name,value\n"
	for i := 0; i < 250; i++ {
		csv += fmt.Sprintf("row,%d\n", i)
	}
	item = fileFixture(t, store, "rows.csv", []byte(csv))
	w := fileRequest(router, project, item.ID, "content")
	var table struct {
		Data struct {
			Rows      [][]string `json:"rows"`
			Truncated bool       `json:"truncated"`
		} `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &table)
	if len(table.Data.Rows) != 201 || !table.Data.Truncated {
		t.Fatalf("table: %s", w.Body.String())
	}
	var buffer bytes.Buffer
	zw := zip.NewWriter(&buffer)
	f, _ := zw.Create("xl/workbook.xml")
	f.Write([]byte(`<workbook><sheets><sheet name="人口统计" /></sheets></workbook>`))
	zw.Close()
	item = fileFixture(t, store, "book.xlsx", buffer.Bytes())
	w = fileRequest(router, project, item.ID, "content")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "人口统计") {
		t.Fatal(w.Body.String())
	}
}
func TestSessionFileQGISRequiresBoundInstance(t *testing.T) {
	router, store, project := sessionFilesFixture(t)
	item := fileFixture(t, store, "roads.geojson", []byte(`{"type":"FeatureCollection","features":[]}`))
	for _, action := range []string{"inspect", "thumbnail", "load"} {
		w := fileRequest(router, project, item.ID, action)
		if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "此会话绑定") {
			t.Fatalf("%s: %d %s", action, w.Code, w.Body.String())
		}
	}
	if got := sessionFileInstance(map[string]any{"active_instance_id": "other", "bound_instance_id": "bound"}); got != "bound" {
		t.Fatal(got)
	}
	if got := sessionFileInstance(map[string]any{"active_instance_id": "other"}); got != "" {
		t.Fatal(got)
	}
}

func TestSessionFileLoadKeepsBusinessNameAndExplicitOverride(t *testing.T) {
	registry, provider, conn, cleanup := setupInstanceActionBridge(t, "bound-only", []map[string]any{
		{"name": "data_load_vector", "inputSchema": map[string]any{"type": "object"}},
		{"name": "data_load_raster", "inputSchema": map[string]any{"type": "object"}},
	})
	defer cleanup()
	project := filepath.Join(t.TempDir(), "map.qgz")
	svc := newInMemorySessionService()
	if err := svc.SaveSession(map[string]any{"session_id": "files-test", "bound_instance_id": "bound-only"}, project); err != nil {
		t.Fatal(err)
	}
	a := &API{sessionService: svc, localBridgeProvider: provider, instanceRegistry: registry}
	router := mux.NewRouter()
	a.SetupRoutes(router)
	store, err := sessionfiles.New(project, "files-test")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		file, requestName, wantName, tool string
		origin                            sessionfiles.Origin
	}{
		{"data.gpkg", "", "武汉地铁站点", "data_load_vector", sessionfiles.Origin{Type: "data_service", Label: "武汉地铁站点"}},
		{"data.tif", "", "武汉人口格网", "data_load_raster", sessionfiles.Origin{Type: "data_service", Label: "武汉人口格网.tif"}},
		{"boundary_county_420100.geojson", "", "武汉市区县边界", "data_load_vector", sessionfiles.Origin{Type: "tool", Name: "武汉市区县边界"}},
		{"roads.geojson", "", "", "data_load_vector", sessionfiles.Origin{Type: "tool", Label: "导出矢量图层"}},
		{"data.gpkg", "自定义站点", "自定义站点", "data_load_vector", sessionfiles.Origin{Type: "data_service", Label: "武汉地铁站点"}},
	} {
		item := fileFixture(t, store, tc.file, []byte("fixture"))
		item, err = store.Register(item.Path, tc.origin)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]string{"layer_name": tc.requestName})
		response := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/sessions/files-test/files/"+item.ID+"/load?project_path="+url.QueryEscape(project), bytes.NewReader(body)))
			response <- rr
		}()
		message := readInstanceActionWSMessage(t, conn)
		params := message.Data["params"].(map[string]any)
		gotName, _ := params["layer_name"].(string)
		if message.Data["tool_name"] != tc.tool || params["path"] != item.Path || gotName != tc.wantName || params["show"] != true {
			t.Fatalf("load lost name or real source: tool=%v params=%v want=%q", message.Data["tool_name"], params, tc.wantName)
		}
		sendInstanceActionWSMessage(t, conn, instanceActionWSMessage{Type: "tool_result", Data: map[string]any{
			"request_id": message.Data["request_id"], "success": true, "output": map[string]any{"success": true},
		}})
		select {
		case rr := <-response:
			if rr.Code != http.StatusOK {
				t.Fatalf("load: %d %s", rr.Code, rr.Body.String())
			}
		case <-time.After(3 * time.Second):
			t.Fatal("load response timed out")
		}
	}
}
