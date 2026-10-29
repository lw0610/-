package flowrun

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource"
)

func TestReceiptFormsRepairAndToolSummaryLimit(t *testing.T) {
	valid := `{"status":"completed","summary":"包含括号 { 与 }","outputs":{"值":{"id":1}},"issues":[]}`
	for _, text := range []string{valid, "```json\n" + valid + "\n```", "过程 {\"status\":\"not_receipt\"}\n完成：" + valid} {
		if r, err := ParseReceipt(text); err != nil || r.Status != "completed" {
			t.Fatal(r, err)
		}
	}
	for _, text := range []string{"", `{}`, `{"status":"other","outputs":{}}`, `{"status":"completed","outputs":null}`, "```json\nnot-json\n```", "{incomplete"} {
		if _, err := ParseReceipt(text); err == nil {
			t.Fatal("invalid receipt accepted", text)
		}
	}
	if len(topLevelObjects(`text {"x":"quoted \\\" }"} {"second":1}`)) != 2 {
		t.Fatal("JSON string brace handling")
	}
	for _, model := range []ModelCaller{nil, modelFunc(func(context.Context, string) (string, error) { return "", errors.New("offline") }), modelFunc(func(context.Context, string) (string, error) { return "invalid", nil })} {
		_, repair, err := readReceipt(context.Background(), flow.StepPlan{}, StepResult{LastContent: "bad"}, model)
		if err == nil {
			t.Fatal("bad repair accepted")
		}
		if model != nil && repair == nil {
			t.Fatal("repair audit missing")
		}
	}
	if got := truncateUTF8("文字文字", 5); !strings.HasPrefix(got, "文") {
		t.Fatal(got)
	}
}

func toString(value any) string { raw, _ := json.Marshal(value); return string(raw) }

func TestStepMessageResourcesAndUpstreamTruncation(t *testing.T) {
	p := testPlan(2)
	p.Inputs = []flow.InputPlan{{Key: "text", Label: "文本", Type: "text"}, {Key: "source", Label: "原文", Type: "conversation_text"}, {Key: "count", Label: "数字", Type: "number"}}
	p.Resources = []flow.ResourcePlan{{Key: "list", Label: "清单", Type: "data_list", BaseDirFromInput: "数据目录", Entry: "/tmp/list.csv", BaseDir: "/tmp/bound-data", Entries: []resource.ResourceEntry{{Name: "数据", Path: "/tmp/data.csv"}}}, {Key: "doc", Label: "文档", Type: "doc", Entry: "/tmp/reference.md"}, {Key: "private", Label: "非本步资源", Type: "doc", Entry: "/tmp/private.md"}}
	p.Rules = []string{"先检查再执行"}
	p.Steps[1].Refs.Resources = []string{"list", "doc"}
	p.Steps[1].Refs.Outputs = []flow.OutputRef{{Step: "s1", Label: "保留"}}
	run := &RunState{Plan: p, Cursor: 1, Inputs: map[string]any{"text": "input", "source": map[string]any{"$ref": "current_user_message", "length": 5}, "count": 2}, Steps: []StepState{{ID: "s1", Index: 1, Title: "前步", Status: "completed", Outputs: map[string]any{"保留": "exact", "大内容": strings.Repeat("x", 22000)}, Attempts: []Attempt{{Receipt: &Receipt{Summary: "前步完成"}}}}, {ID: "s2", LastFailure: "缺文件", UserGuidance: "重新选择"}}}
	text, truncated, err := BuildStepMessage(p.Steps[1], run)
	if err != nil || !truncated || strings.Contains(text, strings.Repeat("x", 22000)) || !strings.Contains(text, "exact") || !strings.Contains(text, "前步完成") || !strings.Contains(text, "重新选择") || strings.Contains(text, "/tmp/data.csv") || strings.Contains(text, "/tmp/bound-data") || strings.Contains(text, "非本步资源") || !strings.Contains(text, "/tmp/reference.md") {
		t.Fatalf("%v %v %s", truncated, err, text)
	}
	if strings.Contains(text, "$ref") {
		t.Fatal("raw source metadata leaked instead of original-source placeholder")
	}
	roles := []flow.RoleSpec{{ID: "executor", Kind: "executor", Enabled: true}, {ID: "hidden", Kind: "agent", Enabled: true, FlowOnly: true}, {ID: "main", Kind: "agent", Enabled: true, IsMain: true}, {ID: "first", Kind: "agent", Enabled: true, Tools: []string{"a", "b"}}, {ID: "second", Kind: "agent", Enabled: true, Tools: []string{"a", "b"}}}
	for _, tools := range [][]string{nil, {"a", "b"}} {
		role, err := SelectRole(flow.StepPlan{Kind: "auto", SuggestedTools: tools}, roles)
		if err != nil || role.ID != "first" {
			t.Fatal(role, err)
		}
	}
	if _, err := SelectRole(flow.StepPlan{Role: "disabled"}, roles); err == nil {
		t.Fatal("missing role accepted")
	}
	if _, err := SelectRole(flow.StepPlan{Kind: "auto"}, nil); err == nil {
		t.Fatal("auto accepted no roles")
	}
}

func validInitialReview(t *testing.T, root string) map[string]any {
	t.Helper()
	manifest := filepath.Join(root, "review.json")
	if err := os.WriteFile(manifest, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	dimensions := map[string]any{}
	for _, name := range reviewDimensions {
		dimensions[name] = map[string]any{"has_issue": false}
	}
	return map[string]any{"dimension_results": dimensions, "suggestions": []any{}, "review_manifest_path": manifest, "reserved_final_image_path": filepath.Join(root, "final.png")}
}
func TestReviewValidationValidAndInvalidStructures(t *testing.T) {
	root := t.TempDir()
	valid := validInitialReview(t, root)
	if err := VerifyInitialReview(valid, root); err != nil {
		t.Fatal(err)
	}
	clone := func() map[string]any {
		raw, _ := json.Marshal(valid)
		var result map[string]any
		_ = json.Unmarshal(raw, &result)
		return result
	}
	suggestion := map[string]any{"suggestion_id": "id", "category": "layout", "target_item_ids": []any{"one"}, "executable": false, "adjustment": nil}
	for _, edit := range []func(map[string]any){
		func(r map[string]any) { delete(r, "dimension_results") }, func(r map[string]any) { delete(r["dimension_results"].(map[string]any), "layout") }, func(r map[string]any) {
			r["dimension_results"].(map[string]any)["layout"] = map[string]any{"has_issue": "yes"}
		},
		func(r map[string]any) {
			r["dimension_results"].(map[string]any)["layout"] = map[string]any{"has_issue": true}
		}, func(r map[string]any) { r["suggestions"] = "not array" }, func(r map[string]any) { r["suggestions"] = []any{false} },
		func(r map[string]any) { s := map[string]any{}; r["suggestions"] = []any{s} }, func(r map[string]any) { r["review_manifest_path"] = "missing" }, func(r map[string]any) { r["reserved_final_image_path"] = "https://invalid/file.png" }, func(r map[string]any) { r["reserved_final_image_path"] = filepath.Join(root, "missing", "final.png") },
	} {
		r := clone()
		edit(r)
		if err := VerifyInitialReview(r, root); err == nil {
			t.Fatal("invalid initial review accepted", r)
		}
	}
	for _, change := range []map[string]any{{"category": "bad"}, {"target_item_ids": "bad"}, {"target_item_ids": []any{1}}, {"executable": "true"}, {"adjustment": map[string]any{}}, {"executable": true, "adjustment": nil}, {"executable": true, "adjustment": map[string]any{}}, {"suggestion_id": ""}} {
		r := clone()
		s := map[string]any{}
		for key, value := range suggestion {
			s[key] = value
		}
		for key, value := range change {
			s[key] = value
		}
		r["suggestions"] = []any{s}
		if err := VerifyInitialReview(r, root); err == nil {
			t.Fatal("invalid suggestion accepted", change)
		}
	}
	r := clone()
	r["suggestions"] = []any{suggestion}
	if err := VerifyInitialReview(r, root); err != nil {
		t.Fatal(err)
	}
	r = clone()
	r["suggestions"] = []any{suggestion, suggestion}
	if err := VerifyInitialReview(r, root); err == nil {
		t.Fatal("duplicate suggestion accepted")
	}
	suggestion["executable"] = true
	suggestion["adjustment"] = map[string]any{"tool_name": "move"}
	r = clone()
	r["suggestions"] = []any{suggestion}
	if err := VerifyInitialReview(r, root); err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{nil, "bad", "null", 12, map[string]any{"final_passed": true}, map[string]any{"final_passed": "true", "remaining_issues": []any{}}, map[string]any{"final_passed": true, "remaining_issues": []any{}, "suggestion_results": "bad"}, map[string]any{"final_passed": true, "remaining_issues": []any{}, "suggestion_results": []any{true}}, map[string]any{"final_passed": true, "remaining_issues": []any{}, "suggestion_results": []any{map[string]any{"status": "unknown"}}}} {
		if err := VerifyFinalReview(value); err == nil {
			t.Fatal("invalid final review accepted", value)
		}
	}
	for _, status := range []string{"correctly_applied", "partially_applied", "not_applied", "skipped", "regression"} {
		if err := VerifyFinalReview(map[string]any{"final_passed": false, "remaining_issues": []any{}, "suggestion_results": []any{map[string]any{"status": status}}}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOutputValidationFilesDatasetsLayersAndReadOnlyTools(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "result.txt"), []byte("result"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "empty.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tools := toolFunc(func(_ context.Context, name string, args map[string]any) (any, error) {
		if name == "layout_info" {
			if args["name"] != "map" {
				return map[string]any{"success": false, "error": "missing"}, nil
			}
			return map[string]any{"success": true}, nil
		}
		return map[string]any{"success": true, "info": map[string]any{"layers": []any{map[string]any{"id": "id-1", "name": "roads"}}}}, nil
	})
	for _, tc := range []struct {
		kind, format string
		value        any
		want         bool
	}{
		{"file", "txt", "result.txt", true}, {"file", "png", "result.txt", false}, {"file", "", "empty.txt", false}, {"file", "", "missing", false}, {"file", "", ".", false}, {"file", "", 123, false},
		{"layout", "", "map", true}, {"layout", "", "missing", false}, {"layout", "", 1, false}, {"layers", "", []any{"roads", map[string]any{"layer_id": "id-1"}}, true}, {"layers", "", map[string]any{"id": "id-1"}, true}, {"layers", "", []any{"missing"}, false}, {"layers", "", "roads", true}, {"layers", "", []any{}, false},
		{"dataset", "", ".", true}, {"dataset", "", "result.txt", false}, {"dataset", "", 10, false}, {"dataset", "route_dataset", ".", false}, {"dataset", "statistical_dataset", ".", false}, {"text", "", "value", true}, {"text", "", "", false}, {"confirmation", "", false, true}, {"any", "", nil, true}, {"unknown", "", "value", false},
	} {
		err := verifyOutput(context.Background(), flow.OutputDeclaration{Type: tc.kind, Format: tc.format}, tc.value, root, tools)
		if (err == nil) != tc.want {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	for _, name := range []string{"places.csv", "routes.csv", "route_paths.csv", "points.csv", "symbol_mapping.csv", "callouts.csv", "manifest.json"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("header\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, format := range []string{"route_dataset", "statistical_dataset"} {
		if err := verifyOutput(context.Background(), flow.OutputDeclaration{Type: "dataset", Format: format}, root, root, tools); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"layout", "layers"} {
		if err := verifyOutput(context.Background(), flow.OutputDeclaration{Type: kind}, "map", root, nil); err == nil {
			t.Fatal("missing readonly channel accepted")
		}
		if err := verifyOutput(context.Background(), flow.OutputDeclaration{Type: kind}, "map", root, toolFunc(func(context.Context, string, map[string]any) (any, error) { return nil, errors.New("offline") })); err == nil {
			t.Fatal("tool error ignored")
		}
	}
	step := flow.StepPlan{ExpectedOutputs: []flow.OutputDeclaration{{Label: "required", Type: "text", Required: true}, {Label: "optional", Type: "text"}}}
	if v := VerifyOutputs(context.Background(), step, Receipt{Outputs: map[string]any{}}, root, nil); v.OK || len(v.Errors) != 1 {
		t.Fatal(v)
	}
	if checkToolSuccess(nil) == nil || checkToolSuccess(map[string]any{"success": false}) == nil {
		t.Fatal("tool failure accepted")
	}
}

func TestStoreBoundaryAndCorruptRecords(t *testing.T) {
	store := FileRunStore{}
	ctx := context.Background()
	root := t.TempDir()
	for _, id := range []string{"../escape", "x", "/absolute"} {
		if _, err := RunPath(root, id); err == nil {
			t.Fatal("bad ID accepted")
		}
	}
	if _, err := RunPath("relative", "valid-id"); err == nil {
		t.Fatal("relative session path accepted")
	}
	if err := store.Save(ctx, nil); err == nil {
		t.Fatal("nil run accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.Save(cancelled, &RunState{}); err == nil {
		t.Fatal("cancelled write accepted")
	}
	if _, err := store.Load(cancelled, root, "run-one"); err == nil {
		t.Fatal("cancelled read accepted")
	}
	if ids, err := store.MarkInterrupted(ctx, root); err != nil || len(ids) != 0 {
		t.Fatal(ids, err)
	}
	path, _ := RunPath(root, "run-one")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("bad json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(ctx, root, "run-one"); err == nil {
		t.Fatal("bad JSON accepted")
	}
	if _, err := store.MarkInterrupted(ctx, root); err == nil {
		t.Fatal("corrupt startup record ignored")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "target"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(ctx, root, "run-one"); err == nil {
		t.Fatal("symlink record accepted")
	}
	if _, err := cloneRun(&RunState{Inputs: map[string]any{"invalid": make(chan int)}}); err == nil {
		t.Fatal("unserializable input accepted")
	}
}

func TestChoicePayloadsAndConfirmationOptions(t *testing.T) {
	for _, data := range []any{"retry", `{"choice":"retry","guidance":"note"}`, map[string]any{"selected": []string{"retry"}, "note": "note"}, map[string]any{"value": "retry"}, map[string]any{"option": "retry", "text": "补充说明"}, map[string]any{"options": []string{"retry"}}, map[string]any{"confirmed": true}, map[string]any{"confirmed": false}, map[string]any{"user_data": map[string]any{"selected": []string{"retry"}}}, map[string]any{"decisions": map[string]any{"one": map[string]any{"selected": []string{"retry"}, "text": "note"}}}} {
		if value, err := parseChoice(data); err != nil || value.Choice == "" {
			t.Fatal(value, err)
		}
	}
	for _, data := range []any{nil, 12, "", map[string]any{}, make(chan int)} {
		if _, err := parseChoice(data); err == nil {
			t.Fatal("invalid choice accepted", data)
		}
	}
	if got := confirmationForStep("- first\n* second", "title", false); len(got.Options) != 2 || got.Options[1].Value != "second" {
		t.Fatal(got)
	}
	if got := confirmationForStep("question", "title", false); len(got.Options) != 2 || got.Default != "confirm" {
		t.Fatal(got)
	}
	if got := confirmationForStep("failure", "title", true); len(got.Options) != 3 || got.Default != "retry" {
		t.Fatal(got)
	}
	if !reflect.DeepEqual(topLevelObjects("no JSON"), []string{}) {
		t.Fatal("empty scan")
	}
}
