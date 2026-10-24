package flow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/resource"
)

func TestLegacyPlanReadCanonicalWrite(t *testing.T) {
	var p FlowPlan
	if err := json.Unmarshal([]byte(`{"cache_key":"old-key","resources":[{"type":"data_list","resolved_path":"/saved/list.csv","entries":[{"name":"saved","path":"/saved/data.gpkg"}]},{"type":"symbols","resolved_path":"/saved/symbols"}]}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.CompileKey != "old-key" || p.Resources[0].Entry != "/saved/list.csv" || p.Resources[0].Root != "/saved" || p.Resources[0].Entries[0].Path != "/saved/data.gpkg" || p.Resources[1].Root != "/saved/symbols" {
		t.Fatalf("saved snapshot changed: %+v", p)
	}
	if got := p.Resources[0].ResolvedResource(); got.ResolvedPath != p.Resources[0].Entry || got.Handle()["entry"] != "/saved/list.csv" {
		t.Fatalf("canonical handle lost saved path: %+v", got)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"cache_key"`) || strings.Contains(string(raw), `"resolved_path"`) || !strings.Contains(string(raw), `"compile_key":"old-key"`) {
		t.Fatalf("legacy fields escaped: %s", raw)
	}
}

func TestLegacyPlanFieldsDoNotReplaceCanonicalValues(t *testing.T) {
	var p FlowPlan
	if err := json.Unmarshal([]byte(`{"compile_key":"new-key","cache_key":"old-key","resources":[{"type":"doc","entry":"/new/doc.md","root":"/new","resolved_path":"/old/doc.md"},{"type":"data_folder","root":"/new/data","resolved_path":"/old/data"}]}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.CompileKey != "new-key" || p.Resources[0].Entry != "/new/doc.md" || p.Resources[0].Root != "/new" || p.Resources[1].Root != "/new/data" {
		t.Fatalf("canonical values replaced: %+v", p)
	}
}

func TestResolvedResourceDoesNotPublishLegacyPath(t *testing.T) {
	raw, err := json.Marshal(&resource.ResolvedResource{Entry: "/current/doc.md", ResolvedPath: "/private/parser-path"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "resolved_path") || strings.Contains(string(raw), "parser-path") || !strings.Contains(string(raw), `"entry":"/current/doc.md"`) {
		t.Fatalf("private field escaped: %s", raw)
	}
}
