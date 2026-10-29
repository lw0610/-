package dataservice

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestSelectionResolvesUpstreamExtentOnceWithoutMutatingPreview(t *testing.T) {
	s, _ := fixture(t)
	s.options.Environment.Catalog.Sources = nil
	request := riverRequest()
	request.Need.Extent = "「派生边界」 外扩 10%"
	request.IncludeFallback = true
	found, err := s.Find(context.Background(), request)
	if err != nil || len(found.Fallback) == 0 {
		t.Fatal(found, err)
	}
	selection, err := s.ValidateChoice(context.Background(), found.FindID, map[string]string{"*": found.Fallback[0].ID})
	if err != nil || !selection.Rows[0].Item.Extent.Pending {
		t.Fatal(selection, err)
	}
	if _, err = s.ResolveSelectionExtent(context.Background(), selection, nil); err == nil {
		t.Fatal("unavailable upstream bounds must not launch an expert")
	}
	path := filepath.Join(t.TempDir(), "boundary.geojson")
	if err = os.WriteFile(path, []byte(`{"type":"Polygon","coordinates":[[[110,31],[111,31],[111,32],[110,32],[110,31]]]}`), 0600); err != nil {
		t.Fatal(err)
	}
	frozen, err := s.ResolveSelectionExtent(context.Background(), selection, map[string]string{"派生边界": path})
	if err != nil || frozen.Rows[0].Item.Extent.Pending || frozen.Rows[0].Item.Extent.Path != "" || !selection.Rows[0].Item.Extent.Pending {
		t.Fatal("resolution did not preserve preview or expand the trusted bounds", frozen, err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	again, err := s.ResolveSelectionExtent(context.Background(), frozen, map[string]string{"派生边界": path})
	if err != nil {
		t.Fatal("frozen scope should not reread upstream on resume", err)
	}
	for i, expected := range []float64{109.9, 30.9, 111.1, 32.1} {
		if math.Abs(again.Rows[0].Item.Extent.BBox[i]-expected) > 1e-9 {
			t.Fatal("extent expanded repeatedly or changed after freeze", again.Rows[0].Item.Extent)
		}
	}
}

func TestSelectionKeepsExistingLocalDataUsableWithoutExtent(t *testing.T) {
	s, _ := fixture(t)
	path := filepath.Join(t.TempDir(), "river.geojson")
	if err := os.WriteFile(path, []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"LineString","coordinates":[[110,31],[111,32]]},"properties":{"name":"fixture"}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	s.options.UserData = []Binding{{Name: "河流", Path: path}}
	request := riverRequest()
	request.Need.Extent = ""
	request.Need.Fields = nil
	found, err := s.Find(context.Background(), request)
	if err != nil || len(found.Rows) != 1 {
		t.Fatal(found, err)
	}
	choice := ""
	for _, candidate := range found.Rows[0].Candidates {
		if candidate.Path == path {
			choice = candidate.ID
		}
	}
	if choice == "" {
		t.Fatal("local file was not discovered", found)
	}
	result, err := s.Get(context.Background(), GetRequest{FindID: found.FindID, Choice: map[string]string{"*": choice}})
	if err != nil || result.Status != "completed" || len(result.Rows) != 1 || len(result.Rows[0].Paths) == 0 {
		t.Fatal("local data unexpectedly required a download extent", result, err)
	}
}
