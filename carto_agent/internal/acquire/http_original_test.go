package acquire

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
)

func TestHTTPOriginalSharedAcrossParallelCategories(t *testing.T) {
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downloads.Add(1)
		time.Sleep(30 * time.Millisecond)
		_, _ = w.Write([]byte("real original fixture"))
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	source := &datasource.Source{ID: "source.fixture", Version: "1", Hosts: []string{parsed.Host}, RefreshDays: 7}
	engine := &Engine{Store: NewStore(t.TempDir(), 1024)}
	adapter := HTTPAdapter{Client: server.Client()}
	var wait sync.WaitGroup
	for _, category := range []string{"hydro.river_line", "transport.railway"} {
		wait.Add(1)
		go func(category string) {
			defer wait.Done()
			fetched, err := engine.fetchHTTPOriginal(context.Background(), adapter, FetchRequest{Source: source, Provide: &datasource.Provide{URL: server.URL + "/china.osm.pbf"}, Item: flow.AcquisitionItem{Category: category}})
			if err != nil || len(fetched.Files) != 1 {
				t.Errorf("%v %v", fetched, err)
			}
		}(category)
	}
	wait.Wait()
	if downloads.Load() != 1 {
		t.Fatalf("same original downloaded %d times", downloads.Load())
	}
}

func TestRegionalArchiveSelectionAndCoverageIdentity(t *testing.T) {
	catalog := &datasource.Catalog{Regions: []datasource.Region{{Name: "湖南省", Code: "430000"}, {Name: "长沙市", Code: "430100"}}}
	source := datasource.Source{ID: "source.fixture", RegionURLs: map[string]string{"430000": "https://example.org/hunan.pbf"}}
	for _, name := range []string{"湖南省", "长沙市", "430000"} {
		provide, code := regionalProvide(catalog, flow.AcquisitionItem{Extent: &flow.ExtentPlan{Region: name}}, source, datasource.Provide{URL: "https://example.org/china.pbf"})
		if provide.URL != "https://example.org/hunan.pbf" || code != "430000" {
			t.Fatal(name, provide, code)
		}
	}
	provide, code := regionalProvide(catalog, flow.AcquisitionItem{Extent: &flow.ExtentPlan{Region: "湘鄂两省"}}, source, datasource.Provide{URL: "https://example.org/china.pbf"})
	if provide.URL != "https://example.org/china.pbf" || code != "" {
		t.Fatal(provide, code)
	}
	store := NewStore(t.TempDir(), 1024)
	spec := CacheSpec{SourceID: source.ID, Category: "hydro.river_line", CoverageRegion: "430000", ScaleClass: "regional", BBox: []float64{0, 0, 10, 10}, AdapterVersion: "v7"}
	_, _, err := store.Materialize(context.Background(), spec, 7, func(dir string) (Manifest, error) {
		if err := os.WriteFile(filepath.Join(dir, "out", "fixture.json"), []byte("{}"), 0600); err != nil {
			return Manifest{}, err
		}
		return Manifest{OutputFiles: []string{"out/fixture.json"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		region string
		count  int
	}{{"430100", 1}, {"420000", 0}, {"", 0}} {
		records, err := store.FindSemantic(context.Background(), spec.Category, nil, "regional", []float64{1, 1, 2, 2}, test.region)
		if err != nil || len(records) != test.count {
			t.Fatalf("%s: %v %v", test.region, records, err)
		}
	}
}
