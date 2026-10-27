package datasource

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func builtinCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := Load("../../../autocarto/tools/system/data-acquisition/assets", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Diagnostics) > 0 {
		t.Fatalf("catalog diagnostics: %+v", c.Diagnostics)
	}
	return c
}
func TestBuiltinCatalogAndAlias(t *testing.T) {
	c := builtinCatalog(t)
	if len(c.Categories) != 23 || len(c.Sources) != 9 {
		t.Fatalf("catalog counts %d/%d", len(c.Categories), len(c.Sources))
	}
	category, params, err := c.ResolveCategory("省域", nil)
	if err != nil || category.ID != "admin.division" || params["level"] != "省" {
		t.Fatalf("%+v %+v %v", category, params, err)
	}
	if _, _, err = c.ResolveCategory("行政区面", nil); err == nil {
		t.Fatal("required level accepted")
	}
	if _, _, err = c.ResolveCategory("省域", map[string]string{"level": "街道"}); err == nil {
		t.Fatal("invalid level accepted")
	}
	encoded, _ := json.Marshal(c)
	again := builtinCatalog(t)
	other, _ := json.Marshal(again)
	if string(encoded) != string(other) {
		t.Fatal("catalog must be deterministic")
	}
	for _, s := range c.SourcesFor("transport.railway") {
		if len(s.Provides) == 0 {
			t.Fatal(s.ID)
		}
	}
	if TerritoryReason(category, c.Source("source.naturalearth.10m"), true) == "" {
		t.Fatal("territory source admitted")
	}
	if TerritoryReason(category, &Source{Kind: "local_folder", Compliance: "unknown"}, true) != "" {
		t.Fatal("explicit local source rejected")
	}
}
func TestRejectInvalidUserCatalog(t *testing.T) {
	c := builtinCatalog(t)
	base := Source{APIVersion: "data_source/v1", ID: "user.local.fixture", Name: "测试来源", Kind: "local_folder", Root: t.TempDir(), Provides: []Provide{{Category: "transport.railway", Path: "rail.shp"}}}
	for _, tt := range []struct {
		name   string
		mutate func(*Source)
		code   string
	}{
		{"override", func(s *Source) { s.ID = "source.builtin.cn-admin" }, "DS_ID_CONFLICT"},
		{"missing host", func(s *Source) { s.Kind = "http_file" }, "DS_HOST_MISSING"},
		{"unknown category", func(s *Source) { s.Provides = []Provide{{Category: "unregistered"}} }, "DS_CATEGORY_UNKNOWN"},
		{"traversal", func(s *Source) { s.Provides = []Provide{{Category: "transport.railway", Path: "../rail.shp"}} }, "DS_SCHEMA_INVALID"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			copy := base
			tt.mutate(&copy)
			raw, err := marshalSource(copy)
			if err != nil {
				t.Fatal(err)
			}
			_, ds := c.ParseSource(raw, false)
			found := false
			for _, d := range ds {
				found = found || d.Code == tt.code
			}
			if !found {
				t.Fatalf("%s: %+v", tt.code, ds)
			}
		})
	}
	for _, raw := range []string{
		"apiVersion: data_source/v1\nid: user.test\napi_key: forbidden",
		"apiVersion: data_source/v1\nid: user.test\nprovides:\n - category: transport.railway\n   url: https://example.com/file?tk=forbidden",
		"apiVersion: data_source/v1\nid: user.test\nprovides:\n - category: transport.railway\n   url: https://user:password@example.com/file",
	} {
		_, ds := c.ParseSource([]byte(raw), false)
		if len(ds) == 0 || ds[0].Code != "DS_SECRET_INLINE" {
			t.Fatalf("secret not rejected: %+v", ds)
		}
	}
	root := t.TempDir()
	raw, _ := marshalSource(base)
	if err := os.WriteFile(filepath.Join(root, "a.yaml"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.yaml"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load("../../../autocarto/tools/system/data-acquisition/assets", root)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Sources) != 10 || len(loaded.Diagnostics) != 1 || loaded.Diagnostics[0].Code != "DS_ID_CONFLICT" {
		t.Fatalf("%+v", loaded.Diagnostics)
	}
}
func TestHostWhitelistIsExact(t *testing.T) {
	for _, raw := range []string{"https://example.com.evil.test/file", "https://user@example.com/file", "https://example.com:444/file"} {
		u, _ := url.Parse(raw)
		if HostAllowed(u, []string{"example.com"}) {
			t.Fatal(raw)
		}
	}
	u, _ := url.Parse("https://example.com/file")
	if !HostAllowed(u, []string{"example.com"}) {
		t.Fatal("allowed host rejected")
	}
}
