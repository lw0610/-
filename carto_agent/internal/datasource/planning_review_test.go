package datasource

import (
	"strings"
	"testing"
)

func TestGEEDefaultParametersRemainOverridable(t *testing.T) {
	c := builtinCatalog(t)
	env := PlanningEnvironment{Catalog: c, Integrations: map[string]bool{"gee": true}}
	for _, tt := range []struct {
		category  string
		params    map[string]string
		key, want string
	}{
		{"terrain.dem", nil, "scale", "30"},
		{"population.grid", map[string]string{"year": "2019"}, "year", "2019"},
		{"imagery.optical", map[string]string{"scale": "20", "cloud_max": "10"}, "scale", "20"},
	} {
		t.Run(tt.category, func(t *testing.T) {
			item := env.Rank(AcquisitionItem{Category: tt.category, Params: tt.params, Acquirable: true})
			if !item.Available || item.Chosen == nil || item.Chosen.SourceID != "source.gee.catalog" {
				t.Fatalf("%+v", item)
			}
			effective := item.Chosen.Provide.EffectiveParams(item.Params, nil)
			if effective[tt.key] != tt.want {
				t.Fatal(effective)
			}
		})
	}
	if c.Source("source.gee.catalog").Provides[3].Defaults["year"] != "2020" {
		t.Fatal("source defaults mutated")
	}
}

func TestRequiredParameterCanComeFromSourceDefault(t *testing.T) {
	c := &Catalog{Categories: []Category{{ID: "population", Name: "人口", Params: map[string]Parameter{"year": {Required: true, Values: []string{"2019", "2020"}}}}}, Sources: []Source{{ID: "source.population", Kind: "gee", Provides: []Provide{{Category: "population", Defaults: map[string]string{"year": "2020"}}}}}}
	env := PlanningEnvironment{Catalog: c}
	for _, params := range []map[string]string{nil, {"year": "2019"}} {
		_, declared, err := c.ResolveAcquisitionCategory("人口", params)
		if err != nil {
			t.Fatal(err)
		}
		if params == nil && len(declared) != 0 {
			t.Fatal("source-specific defaults leaked into declared parameters")
		}
		if item := env.Rank(AcquisitionItem{Category: "population", Params: declared, Acquirable: true}); !item.Available {
			t.Fatalf("%+v", item)
		}
	}
	if _, _, err := c.ResolveCategory("人口", nil); err == nil {
		t.Fatal("strict category resolution changed")
	}
	if _, _, err := c.ResolveAcquisitionCategory("人口", map[string]string{"year": "2024"}); err == nil {
		t.Fatal("explicit invalid year replaced by default")
	}
	c.Sources[0].Provides[0].Defaults["year"] = "2024"
	if item := env.Rank(AcquisitionItem{Category: "population", Acquirable: true}); item.Available {
		t.Fatal("invalid default accepted")
	}
	c.Sources[0].Provides[0].Defaults = nil
	if _, _, err := c.ResolveAcquisitionCategory("人口", nil); err == nil {
		t.Fatal("missing required value accepted without default")
	}
}

func TestProvideConstraintsAndSourceDefaultsAreDistinct(t *testing.T) {
	c := &Catalog{Categories: []Category{{ID: "admin", Name: "行政区", Params: map[string]Parameter{"level": {Required: true, Values: []string{"省", "市"}}}}}, Sources: []Source{{ID: "source.province", Kind: "local_folder", Root: t.TempDir(), Provides: []Provide{{Category: "admin", Params: map[string]string{"level": "省"}}}}}}
	env := PlanningEnvironment{Catalog: c}
	if item := env.Rank(AcquisitionItem{Category: "admin", Acquirable: true, Params: map[string]string{"level": "省"}}); !item.Available {
		t.Fatalf("%+v", item)
	}
	if item := env.Rank(AcquisitionItem{Category: "admin", Acquirable: true, Params: map[string]string{"level": "市"}}); item.Available {
		t.Fatal("fixed provincial dataset offered as city data")
	}
	if item := env.Rank(AcquisitionItem{Category: "admin", Acquirable: true}); item.Available {
		t.Fatal("constraint silently used as default")
	}
}

func TestCachedSourceOnlyBypassesDownloadPrerequisites(t *testing.T) {
	c := &Catalog{Categories: []Category{{ID: "test", Name: "测试", Geometry: "line"}}, Sources: []Source{{ID: "source.test", Kind: "gee", Status: "active", Requires: []string{"integration:gee", "secret:fixture"}, SizeMB: 9000, Provides: []Provide{{Category: "test"}}}}}
	item := AcquisitionItem{Category: "test", Acquirable: true, Extent: &ExtentPlan{BBox: []float64{110, 30, 111, 31}}}
	e := PlanningEnvironment{Catalog: c, MaxDownloadMB: 1}
	fresh := e.Rank(item)
	if fresh.Available || len(fresh.Reasons) != 3 {
		t.Fatalf("%+v", fresh)
	}
	e.CacheLookup = func(AcquisitionItem, SourceChoice) (string, bool) { return "cached-fixture", true }
	cached := e.Rank(item)
	if !cached.Available || !cached.Chosen.CacheHit || cached.Chosen.Tier != 1 {
		t.Fatalf("%+v", cached)
	}
}

func TestCachedSourceStillHonorsPolicyAndCoverage(t *testing.T) {
	for _, tt := range []struct {
		name, reason string
		mutate       func(*PlanningEnvironment, *Source, *Category, *AcquisitionItem)
	}{
		{"disabled", "停用", func(_ *PlanningEnvironment, s *Source, _ *Category, _ *AcquisitionItem) { s.Status = "disabled" }},
		{"experimental", "未完成验证", func(_ *PlanningEnvironment, s *Source, _ *Category, _ *AcquisitionItem) { s.Status = "experimental" }},
		{"denied", "关闭自动获取", func(e *PlanningEnvironment, _ *Source, _ *Category, _ *AcquisitionItem) { e.AutoAcquire = "deny" }},
		{"excluded", "排除了", func(_ *PlanningEnvironment, s *Source, _ *Category, i *AcquisitionItem) {
			i.ExcludedSources = []string{s.ID}
		}},
		{"search authorization", "未允许联网搜索", func(_ *PlanningEnvironment, s *Source, _ *Category, _ *AcquisitionItem) { s.Kind = "web_search" }},
		{"territory", "疆域", func(_ *PlanningEnvironment, s *Source, c *Category, _ *AcquisitionItem) {
			c.Sensitivity = "cn_territory"
			s.Compliance = "open"
		}},
		{"minimum scale", "过于概略", func(_ *PlanningEnvironment, s *Source, _ *Category, i *AcquisitionItem) {
			s.Scale.MinDenominator = 2000000
			i.Scale = 100000
		}},
		{"maximum scale", "较小范围", func(_ *PlanningEnvironment, s *Source, _ *Category, i *AcquisitionItem) {
			s.Scale.MaxDenominator = 1000
			i.Scale = 100000
		}},
		{"coverage bbox", "覆盖范围", func(_ *PlanningEnvironment, s *Source, _ *Category, _ *AcquisitionItem) {
			s.Coverage.BBox = []float64{0, 0, 1, 1}
		}},
		{"coverage country", "只覆盖中国", func(_ *PlanningEnvironment, s *Source, _ *Category, i *AcquisitionItem) {
			s.Coverage.Regions = []string{"CN"}
			i.Extent.BBox = []float64{0, 0, 1, 1}
		}},
		{"minimum area", "不适合当前小范围", func(_ *PlanningEnvironment, s *Source, _ *Category, _ *AcquisitionItem) { s.Limits.MinAreaKM2 = 1e8 }},
		{"maximum area", "查询限制", func(_ *PlanningEnvironment, s *Source, _ *Category, _ *AcquisitionItem) { s.Limits.MaxAreaKM2 = 1 }},
		{"missing local root", "目录不存在", func(_ *PlanningEnvironment, s *Source, _ *Category, _ *AcquisitionItem) {
			s.Kind = "local_folder"
			s.Root = ""
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := &Catalog{Categories: []Category{{ID: "test", Name: "测试", Geometry: "line"}}, Sources: []Source{{ID: "source.test", Kind: "http_file", Builtin: true, Status: "active", Provides: []Provide{{Category: "test"}}}}}
			lookedUp := false
			e := PlanningEnvironment{Catalog: c, CacheLookup: func(AcquisitionItem, SourceChoice) (string, bool) { lookedUp = true; return "cached", true }}
			item := AcquisitionItem{Category: "test", Acquirable: true, Extent: &ExtentPlan{BBox: []float64{110, 30, 111, 31}}}
			tt.mutate(&e, &c.Sources[0], &c.Categories[0], &item)
			got := e.Rank(item)
			if got.Available || lookedUp || !strings.Contains(strings.Join(got.Reasons, "；"), tt.reason) {
				t.Fatalf("lookedUp=%v %+v", lookedUp, got)
			}
		})
	}
}

func TestSourceAlternativesRetainTheirOwnDefaults(t *testing.T) {
	c := &Catalog{Categories: []Category{{ID: "population", Name: "人口", Params: map[string]Parameter{"year": {Required: true}}}}, Sources: []Source{
		{ID: "source.a", Kind: "gee", Priority: 10, Provides: []Provide{{Category: "population", Defaults: map[string]string{"year": "2020", "scale": "100"}}}},
		{ID: "source.b", Kind: "gee", Priority: 20, Provides: []Provide{{Category: "population", Defaults: map[string]string{"year": "2019", "scale": "200"}}}},
	}}
	env := PlanningEnvironment{Catalog: c}
	item := env.Rank(AcquisitionItem{Category: "population", Acquirable: true})
	if !item.Available || len(item.Alternatives) != 1 {
		t.Fatalf("%+v", item)
	}
	chosen := item.Chosen.Provide.EffectiveParams(item.Params, nil)
	alternate := item.Alternatives[0].Provide.EffectiveParams(item.Params, nil)
	if chosen["year"] != "2020" || alternate["year"] != "2019" || len(item.Params) != 0 {
		t.Fatal(chosen, alternate, item.Params)
	}
	override := map[string]string{"year": "2018"}
	if got := item.Alternatives[0].Provide.EffectiveParams(override, nil); got["year"] != "2018" || got["scale"] != "200" {
		t.Fatal(got)
	}
}
