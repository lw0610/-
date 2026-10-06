package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func newCalloutImportReview(t *testing.T, graph Graph) (*ReviewStore, *Review) {
	t.Helper()
	store := NewReviewStore(t.TempDir())
	source := "原始行军文本保持不变。"
	review, err := store.Create(ReviewCreateInput{
		SessionID: "callout-import", SourceText: source,
		Contract:       Contract{ContractID: "v1", SourceSHA256: reviewHash([]byte(source)), JSONSchema: json.RawMessage(`{}`)},
		OriginalResult: &Result{Graph: graph}, Graph: graph,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store, review
}

func calloutImportImage(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 3))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func calloutTestResolve(_ context.Context, graph map[string]any, _ string) error {
	for _, raw := range array(graph["spatial_entities"]) {
		place := raw.(map[string]any)
		if ValidateResolvedCoordinates(Graph{"spatial_entities": []any{place}}) == nil {
			continue
		}
		place["lon"], place["lat"] = 110.5, 33.5
		place["coordinate_source"], place["coordinate_confidence"] = "llm_estimated", 0.7
		place["geocoding"] = map[string]any{"status": "estimated"}
	}
	return nil
}

func TestCalloutImportPreservesDraftMatchesAndUpdatesImages(t *testing.T) {
	graph := Graph{
		"spatial_entities": []any{map[string]any{"entity_id": "he", "name": "何家冲", "entity_kind": "place", "lon": 114.4, "lat": 31.7, "coordinate_source": "manual"}},
		"events":           []any{map[string]any{"event_id": "departure", "name": "出发", "location_ref": "he"}},
		"routes":           []any{map[string]any{"route_id": "route", "name": "原路线", "paths": []any{}}},
		"callouts":         []any{map[string]any{"callout_id": "existing", "anchor_ref_type": "event", "anchor_ref": "departure", "body": "正文甲\n第二行", "title": "旧标题", "width_mm": 50}},
	}
	store, review := newCalloutImportReview(t, graph)
	draft, _ := cloneGraph(review.Graph)
	array(draft["routes"])[0].(map[string]any)["name"] = "尚未保存的路线名称"
	body := "正文甲\n第二行"
	rows := []calloutImportRow{
		{Sheet: "图文标注", Row: 2, Place: "何家冲\n（河南省罗山县）", Body: body, Caption: "此说明不能成为正文", Image: calloutImportImage(t)},
		{Sheet: "图文标注", Row: 3, Place: "枣阳—保安寨（湖北省）", Body: "完整复合地名及行军上下文。"},
	}
	resolveCalls := 0
	resolve := func(ctx context.Context, g map[string]any, source string) error {
		resolveCalls++
		if !strings.Contains(source, rows[1].Place) || !strings.Contains(source, rows[1].Body) || !strings.Contains(source, review.SourceText) {
			t.Fatalf("Excel context omitted: %q", source)
		}
		latest, err := store.Get(review.SessionID, review.ReviewID)
		if err != nil || latest.Revision != review.Revision || len(array(latest.Graph["callouts"])) != 1 {
			t.Fatal("partial import was persisted before location completion")
		}
		// The shared resolver may enrich route metadata in other workflows;
		// annotation import must merge only entity coordinate fields.
		array(g["routes"])[0].(map[string]any)["resolver_metadata"] = "must not be saved"
		return calloutTestResolve(ctx, g, source)
	}
	path := filepath.Join(t.TempDir(), "notes.xlsx")
	result, err := store.importCalloutRows(context.Background(), review.SessionID, review.ReviewID, review.Revision, path, draft, rows, nil, resolve)
	if err != nil {
		t.Fatal(err)
	}
	if result.ImportedCount != 1 || result.UpdatedCount != 1 || result.ImageCount != 1 || result.NewPlaceCount != 1 || resolveCalls != 1 {
		t.Fatalf("unexpected import result: %+v", result)
	}
	got := result.Review
	if !reflect.DeepEqual(got.Graph["routes"], draft["routes"]) || !reflect.DeepEqual(got.Graph["events"], draft["events"]) || got.SourceText != review.SourceText || got.SourceSHA256 != review.SourceSHA256 {
		t.Fatal("unsaved edits, route/event data, or immutable source were changed")
	}
	if len(array(draft["callouts"])) != 1 || array(draft["callouts"])[0].(map[string]any)["title"] != "旧标题" {
		t.Fatal("import mutated the caller draft")
	}
	first := array(got.Graph["callouts"])[0].(map[string]any)
	if first["callout_id"] != "existing" || first["anchor_ref"] != "he" || first["title"] != "" || first["body"] != body || first["width_mm"] != float64(50) || first["leader_style"] != "double" || first["template"] != "image_top_text_bottom" {
		t.Fatalf("existing callout not adopted correctly: %#v", first)
	}
	second := array(got.Graph["callouts"])[1].(map[string]any)
	if second["template"] != "text_only" || second["image_enabled"] != false {
		t.Fatalf("no-picture row did not become text only: %#v", second)
	}
	newPlace := array(got.Graph["spatial_entities"])[1].(map[string]any)
	if newPlace["name"] != rows[1].Place || newPlace["map_role"] != "background" || newPlace["lon"] != 110.5 {
		t.Fatalf("compound location or qualifier lost: %#v", newPlace)
	}
	if len(got.CalloutImports) != 2 || len(got.Assets) != 1 || len(got.CoordinateLookups) != 1 {
		t.Fatalf("source/assets/automatic-coordinate metadata missing: %#v", got)
	}
	raw, mime, err := store.ReadAsset(got.SessionID, got.ReviewID, got.Assets[0].AssetID)
	if err != nil || mime != "image/png" || !bytes.Equal(raw, rows[0].Image) {
		t.Fatalf("embedded image not available for preview: %s %v", mime, err)
	}
	rows[1].Body = "再次导入时更新正文。"
	repeated, err := store.importCalloutRows(context.Background(), got.SessionID, got.ReviewID, got.Revision, path, nil, rows, nil, resolve)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.ImportedCount != 0 || repeated.UpdatedCount != 2 || repeated.NewPlaceCount != 0 || resolveCalls != 1 || len(array(repeated.Review.Graph["callouts"])) != 2 || len(repeated.Review.Assets) != 1 {
		t.Fatalf("reimport is not stable: %+v", repeated)
	}
	if array(repeated.Review.Graph["callouts"])[1].(map[string]any)["body"] != rows[1].Body {
		t.Fatal("same workbook row did not update its previous body")
	}
}

func TestCalloutImportExactNamesQualifiersAndAmbiguity(t *testing.T) {
	graph := Graph{"spatial_entities": []any{
		map[string]any{"entity_id": "a", "name": "同名村（河南省）", "entity_kind": "place"},
		map[string]any{"entity_id": "b", "name": "同名村（陕西省）", "entity_kind": "place"},
		map[string]any{"entity_id": "c", "name": "保安", "entity_kind": "place", "aliases": []any{"旧保安"}},
		map[string]any{"entity_id": "d", "name": "实体名", "entity_kind": "place"},
	}, "events": []any{map[string]any{"event_id": "event", "name": "别名地点", "location_ref": "d"}}}
	review := &Review{Graph: graph}
	for _, tc := range []struct {
		name, id  string
		ambiguous bool
	}{
		{"同名村\n(河南省)", "a", false}, {"同名村", "", true}, {"同名村（湖北省）", "", false},
		{"旧保安", "c", false}, {"保安寨", "", false}, {"枣阳—保安寨", "", false}, {"别名地点", "d", false},
	} {
		id, ambiguous := matchCalloutPlace(review, tc.name)
		if id != tc.id || ambiguous != tc.ambiguous {
			t.Errorf("match %s = %q/%v, want %q/%v", tc.name, id, ambiguous, tc.id, tc.ambiguous)
		}
	}
	store, saved := newCalloutImportReview(t, graph)
	rows := []calloutImportRow{{Sheet: "表", Row: 2, Place: "同名村", Body: "同名未绑定到任意已有村。"}}
	first, err := store.importCalloutRows(context.Background(), saved.SessionID, saved.ReviewID, saved.Revision, "/notes.xlsx", nil, rows, nil, calloutTestResolve)
	if err != nil || first.NewPlaceCount != 1 || len(first.Warnings) != 1 {
		t.Fatalf("ambiguous location was not kept distinct: %+v %v", first, err)
	}
	second, err := store.importCalloutRows(context.Background(), saved.SessionID, saved.ReviewID, first.Review.Revision, "/notes.xlsx", nil, rows, nil, calloutTestResolve)
	if err != nil || second.NewPlaceCount != 0 || second.UpdatedCount != 1 {
		t.Fatalf("ambiguous source generated duplicate points: %+v %v", second, err)
	}
}

func TestCalloutImportImageFailuresPreserveText(t *testing.T) {
	store, review := newCalloutImportReview(t, Graph{})
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	imageBytes := calloutImportImage(t)
	if err := os.WriteFile(filepath.Join(dir, "relative.png"), imageBytes, 0600); err != nil {
		t.Fatal(err)
	}
	rows := []calloutImportRow{
		{Sheet: "表", Row: 2, Place: "甲地", Body: "坏图片正文保留", Image: []byte("not an image")},
		{Sheet: "表", Row: 3, Place: "乙地", Body: "缺图片正文保留", ImagePath: "missing.png"},
		{Sheet: "表", Row: 4, Place: "丙地", Body: "相对路径图片", ImagePath: "relative.png"},
	}
	result, err := store.importCalloutRows(context.Background(), review.SessionID, review.ReviewID, review.Revision, filepath.Join(dir, "notes.xlsx"), nil, rows, nil, calloutTestResolve)
	if err != nil {
		t.Fatal(err)
	}
	if result.ImportedCount != 3 || result.ImageCount != 1 || len(result.Warnings) != 2 {
		t.Fatalf("image failures discarded valid text rows: %+v", result)
	}
	for i, raw := range array(result.Review.Graph["callouts"]) {
		row := raw.(map[string]any)
		if row["body"] != rows[i].Body || (i < 2 && row["template"] != "text_only") {
			t.Fatalf("image fallback lost body: %#v", row)
		}
	}
}

func TestCalloutImportMoreSpecificNameNeedsMatchingCoordinatesAndBodyToReuse(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		lon        float64
		wantReuse  bool
	}{
		{"same position and body", "相同正文", 110.5, true},
		{"different position", "相同正文", 111.5, false},
		{"different body", "不同正文", 110.5, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			graph := Graph{
				"spatial_entities": []any{map[string]any{"entity_id": "town", "name": "独树镇", "entity_kind": "place", "lon": tc.lon, "lat": 33.5, "coordinate_source": "manual"}},
				"callouts":         []any{map[string]any{"callout_id": "old", "anchor_ref_type": "spatial_entity", "anchor_ref": "town", "body": "相同正文", "width_mm": 58}},
			}
			store, review := newCalloutImportReview(t, graph)
			rows := []calloutImportRow{{Sheet: "表", Row: 2, Place: "独树镇七里岗（河南方城）", Body: tc.body}}
			result, err := store.importCalloutRows(context.Background(), review.SessionID, review.ReviewID, review.Revision, "/notes.xlsx", nil, rows, nil, calloutTestResolve)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantReuse {
				if result.ImportedCount != 0 || result.UpdatedCount != 1 || result.NewPlaceCount != 0 || len(array(result.Review.Graph["callouts"])) != 1 || len(array(result.Review.Graph["spatial_entities"])) != 1 {
					t.Fatalf("same location/body did not reuse prior row: %+v", result)
				}
				if array(result.Review.Graph["callouts"])[0].(map[string]any)["width_mm"] != float64(58) {
					t.Fatal("reuse discarded the previous layout width")
				}
			} else if result.ImportedCount != 1 || result.UpdatedCount != 0 || result.NewPlaceCount != 1 || len(array(result.Review.Graph["callouts"])) != 2 {
				t.Fatal("similar name or body alone was used to merge distinct annotations")
			}
		})
	}
}

func TestCalloutImportFailureCancellationAndConcurrentEditAreAtomic(t *testing.T) {
	for _, scenario := range []string{"resolver failure", "cancellation", "newer draft", "confirmation"} {
		t.Run(scenario, func(t *testing.T) {
			store, before := newCalloutImportReview(t, Graph{"routes": []any{map[string]any{"route_id": "route", "name": "原路线"}}})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			resolve := func(ctx context.Context, g map[string]any, source string) error {
				switch scenario {
				case "resolver failure":
					return errors.New("model unavailable")
				case "cancellation":
					cancel()
				case "newer draft":
					newer, _ := cloneGraph(before.Graph)
					array(newer["routes"])[0].(map[string]any)["name"] = "同时手工修改"
					if _, err := store.SaveDraft(before.SessionID, before.ReviewID, before.Revision, newer); err != nil {
						t.Fatal(err)
					}
				case "confirmation":
					if _, _, err := store.Claim(before.SessionID, before.ReviewID, before.Revision, "claim"); err != nil {
						t.Fatal(err)
					}
				}
				return calloutTestResolve(ctx, g, source)
			}
			rows := []calloutImportRow{{Sheet: "表", Row: 2, Place: "新增地", Body: "正文", Image: calloutImportImage(t)}}
			if _, err := store.importCalloutRows(ctx, before.SessionID, before.ReviewID, before.Revision, "/notes.xlsx", nil, rows, nil, resolve); err == nil {
				t.Fatal("expected failed import")
			}
			after, err := store.Get(before.SessionID, before.ReviewID)
			if err != nil {
				t.Fatal(err)
			}
			if len(array(after.Graph["callouts"])) != 0 || len(array(after.Graph["spatial_entities"])) != 0 || len(after.Assets) != 0 || len(after.CalloutImports) != 0 {
				t.Fatal("failed import left partial graph or asset registrations")
			}
			switch scenario {
			case "newer draft":
				if after.Revision != before.Revision+1 || array(after.Graph["routes"])[0].(map[string]any)["name"] != "同时手工修改" {
					t.Fatal("late import overwrote newer draft")
				}
			case "confirmation":
				if after.Status != ReviewCompiling {
					t.Fatal("late import reopened confirmed review")
				}
			default:
				if after.Revision != before.Revision || !reflect.DeepEqual(after.Graph, before.Graph) {
					t.Fatal("unsuccessful location changed saved draft")
				}
			}
		})
	}
}

func TestCalloutImportRejectsStaleAndCompiledBeforeReadingWorkbook(t *testing.T) {
	store, review := newCalloutImportReview(t, Graph{})
	if _, err := store.ImportCallouts(context.Background(), review.SessionID, review.ReviewID, review.Revision+1, "/missing.xlsx", nil, nil); !errors.Is(err, ErrReviewConflict) {
		t.Fatalf("expected conflict before file read, got %v", err)
	}
	store.mu.Lock()
	review.Status = ReviewCompiled
	err := store.writeLocked(review)
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.ImportCallouts(context.Background(), review.SessionID, review.ReviewID, review.Revision, "/missing.xlsx", nil, nil); !errors.Is(err, ErrReviewConflict) {
		t.Fatalf("compiled review became editable: %v", err)
	}
}

func TestCalloutImportActualWorkbook(t *testing.T) {
	path := os.Getenv("AUTOCARTO_CALLOUT_XLSX")
	if path == "" {
		t.Skip("set AUTOCARTO_CALLOUT_XLSX to run the local workbook integration test")
	}
	rows, _, err := readCalloutWorkbook(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 29 {
		t.Fatalf("actual workbook rows=%d, want 29", len(rows))
	}
	graph := Graph{"spatial_entities": []any{}, "events": []any{}, "routes": []any{map[string]any{"route_id": "route", "name": "原路线", "paths": []any{}}}, "callouts": []any{}}
	for _, row := range rows {
		name, _ := calloutPlaceName(row.Place)
		if name != "何家冲" && name != "独树镇七里岗" {
			continue
		}
		if name == "独树镇七里岗" {
			name = "独树镇"
		}
		id := "existing_" + reviewHash([]byte(name))[:8]
		graph["spatial_entities"] = append(array(graph["spatial_entities"]), map[string]any{"entity_id": id, "name": name, "entity_kind": "place", "lon": 110.5, "lat": 33.5, "coordinate_source": "manual"})
		graph["callouts"] = append(array(graph["callouts"]), map[string]any{"callout_id": "callout_" + id, "anchor_ref_type": "spatial_entity", "anchor_ref": id, "body": row.Body})
	}
	if len(array(graph["callouts"])) != 2 {
		t.Fatal("expected workbook rows for both existing sample callouts")
	}
	store, review := newCalloutImportReview(t, graph)
	result, err := store.ImportCallouts(context.Background(), review.SessionID, review.ReviewID, review.Revision, path, nil, calloutTestResolve)
	if err != nil {
		t.Fatal(err)
	}
	if result.ImportedCount != 27 || result.UpdatedCount != 2 || result.ImageCount != 27 || len(array(result.Review.Graph["callouts"])) != 29 || len(result.Warnings) != 0 {
		t.Fatalf("actual workbook import mismatch: %+v", result)
	}
	if !reflect.DeepEqual(result.Review.Graph["routes"], review.Graph["routes"]) || result.Review.SourceText != review.SourceText {
		t.Fatal("actual import altered routes or source")
	}
	again, err := store.ImportCallouts(context.Background(), review.SessionID, review.ReviewID, result.Review.Revision, path, nil, calloutTestResolve)
	if err != nil || again.ImportedCount != 0 || again.UpdatedCount != 29 || again.NewPlaceCount != 0 || again.ImageCount != 27 || len(array(again.Review.Graph["callouts"])) != 29 {
		t.Fatalf("actual workbook repeat mismatch: %+v %v", again, err)
	}
}

func TestCalloutImportSourceSurvivesConfirmedReviewReuse(t *testing.T) {
	store, review := newCalloutImportReview(t, Graph{})
	rows := []calloutImportRow{{Sheet: "表", Row: 2, Place: "何家冲（河南罗山）", Body: "首次正文", Caption: "首次图片说明", Image: calloutImportImage(t)}}
	first, err := store.importCalloutRows(context.Background(), review.SessionID, review.ReviewID, review.Revision, "/notes.xlsx", nil, rows, nil, calloutTestResolve)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.Claim(review.SessionID, review.ReviewID, first.Review.Revision, "publish"); err != nil {
		t.Fatal(err)
	}
	published, err := store.Complete(review.SessionID, review.ReviewID, first.Review.Revision, first.Review.Graph, map[string]any{"dataset_dir": t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	next, err := store.Create(ReviewCreateInput{
		SessionID: "callout-reused", ProjectPath: published.ProjectPath, SourceText: published.SourceText,
		Contract: published.Contract, OriginalResult: published.OriginalResult, Graph: published.Graph,
		Assets: published.Assets, ReusedFrom: published,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next.CalloutImports, published.CalloutImports) {
		t.Fatal("confirmed reuse dropped workbook row identities and captions")
	}
	rows[0].Body, rows[0].Caption = "更新后的正文", "更新后的说明"
	updated, err := store.importCalloutRows(context.Background(), next.SessionID, next.ReviewID, next.Revision, "/notes.xlsx", nil, rows, nil, nil)
	if err != nil || updated.ImportedCount != 0 || updated.UpdatedCount != 1 || updated.NewPlaceCount != 0 || len(array(updated.Review.Graph["callouts"])) != 1 {
		t.Fatalf("reimport after confirmed reuse duplicated the row: %+v %v", updated, err)
	}
	if array(updated.Review.Graph["callouts"])[0].(map[string]any)["body"] != rows[0].Body {
		t.Fatal("updated workbook content was not applied")
	}
	previous, err := store.Get(published.SessionID, published.ReviewID)
	if err != nil || !reflect.DeepEqual(previous.CalloutImports, published.CalloutImports) {
		t.Fatal("new review mutated the confirmed source metadata")
	}
}

// This opt-in integration reads the user's actual saved review and workbook,
// but creates a new review in a temporary store. Network coordinates are stubbed
// only for newly introduced places; it never edits the source review or assets.
func TestCalloutImportActualSavedReviewCopy(t *testing.T) {
	workbookPath, reviewPath := os.Getenv("AUTOCARTO_CALLOUT_XLSX"), os.Getenv("AUTOCARTO_CALLOUT_REVIEW")
	if workbookPath == "" || reviewPath == "" {
		t.Skip("set AUTOCARTO_CALLOUT_XLSX and AUTOCARTO_CALLOUT_REVIEW for real-input integration")
	}
	originalBytes, err := os.ReadFile(reviewPath)
	if err != nil {
		t.Fatal(err)
	}
	var envelope reviewEnvelope
	if err = json.Unmarshal(originalBytes, &envelope); err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err = json.Compact(&compact, envelope.Review); err != nil || reviewHash(compact.Bytes()) != envelope.SHA256 {
		t.Fatal("source review integrity mismatch")
	}
	var original Review
	if err = json.Unmarshal(envelope.Review, &original); err != nil {
		t.Fatal(err)
	}
	store := NewReviewStore(t.TempDir())
	copy, err := store.Create(ReviewCreateInput{
		SessionID: "actual-input-copy", ProjectPath: original.ProjectPath, SourceText: original.SourceText,
		Contract: original.Contract, OriginalResult: original.OriginalResult, Graph: original.Graph,
	})
	if err != nil {
		t.Fatal(err)
	}
	beforePlaces := map[string]map[string]any{}
	for _, raw := range array(original.Graph["spatial_entities"]) {
		place := raw.(map[string]any)
		beforePlaces[stringValue(place["entity_id"])] = place
	}
	calls, located := 0, 0
	resolve := func(_ context.Context, graph map[string]any, source string) error {
		calls++
		if !strings.Contains(source, "枣阳—保安寨（湖北）") || !strings.Contains(source, "原始行军文本") {
			t.Fatal("workbook and original context were not passed together")
		}
		for _, raw := range array(graph["spatial_entities"]) {
			place := raw.(map[string]any)
			id := stringValue(place["entity_id"])
			if previous := beforePlaces[id]; previous != nil {
				if !reflect.DeepEqual(place, previous) {
					t.Fatalf("existing location %s was changed before the resolver", id)
				}
				continue
			}
			located++
			place["lon"], place["lat"] = 108.0+float64(located)/100, 32.0+float64(located)/100
			place["coordinate_source"], place["coordinate_confidence"] = "llm_estimated", 0.6
			place["geocoding"] = map[string]any{"status": "estimated", "reason": "offline test stub; not a geographic verification"}
		}
		return nil
	}
	result, err := store.ImportCallouts(context.Background(), copy.SessionID, copy.ReviewID, copy.Revision, workbookPath, nil, resolve)
	if err != nil {
		t.Fatal(err)
	}
	if result.ImportedCount != 27 || result.UpdatedCount != 2 || result.ImageCount != 27 || len(array(result.Review.Graph["callouts"])) != 29 || len(result.Warnings) != 0 || located != result.NewPlaceCount || calls != 1 {
		t.Fatalf("real workbook/review import mismatch: %+v, located=%d calls=%d", result, located, calls)
	}
	newCallouts := map[string]map[string]any{}
	for _, raw := range array(result.Review.Graph["callouts"]) {
		row := raw.(map[string]any)
		newCallouts[stringValue(row["callout_id"])] = row
	}
	for _, raw := range array(original.Graph["callouts"]) {
		previous := raw.(map[string]any)
		current := newCallouts[stringValue(previous["callout_id"])]
		if current == nil || current["body"] != previous["body"] || calloutAnchorEntity(result.Review.Graph, current) != calloutAnchorEntity(original.Graph, previous) {
			t.Fatalf("real existing callout lost its identity or anchor: %#v", previous)
		}
	}
	for _, field := range []string{"routes", "events", "actors", "evidence", "point_presentations"} {
		if !reflect.DeepEqual(result.Review.Graph[field], original.Graph[field]) {
			t.Fatalf("import changed existing %s", field)
		}
	}
	if result.Review.SourceText != original.SourceText || result.Review.SourceSHA256 != original.SourceSHA256 {
		t.Fatal("original source was rewritten")
	}
	for _, raw := range array(result.Review.Graph["spatial_entities"]) {
		place := raw.(map[string]any)
		if previous := beforePlaces[stringValue(place["entity_id"])]; previous != nil && !reflect.DeepEqual(previous, place) {
			t.Fatal("existing real coordinates or point metadata changed")
		}
	}
	again, err := store.ImportCallouts(context.Background(), copy.SessionID, copy.ReviewID, result.Review.Revision, workbookPath, nil, resolve)
	if err != nil || again.ImportedCount != 0 || again.UpdatedCount != 29 || again.NewPlaceCount != 0 || again.ImageCount != 27 || calls != 1 {
		t.Fatalf("real-input repeated import not stable: %+v %v", again, err)
	}
	afterBytes, err := os.ReadFile(reviewPath)
	if err != nil || !bytes.Equal(originalBytes, afterBytes) {
		t.Fatal("source user review was modified")
	}
	t.Logf("Actual saved review copy: 29 callouts, 27 images, 2 existing IDs/anchors reused, %d new places with offline-only coordinates; repeat 0 additions, 29 updates", result.NewPlaceCount)
}
