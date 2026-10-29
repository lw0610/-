package flowrun

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func styleCall(args, requested, actual string) ToolSummary {
	var state map[string]any
	_ = json.Unmarshal([]byte(actual), &state)
	if _, hasSymbol := state["symbol"]; hasSymbol && state["renderer"] == nil {
		state["renderer"] = "QgsSingleSymbolRenderer"
		encoded, _ := json.Marshal(state)
		actual = string(encoded)
	}
	return ToolSummary{Name: "layer_set_symbology", Arguments: json.RawMessage(args), Result: `{"success":true,"data":{"items":[{"layer_id":"river-id","requested":` + requested + `,"actual":` + actual + `}]}}`}
}

func TestSymbologyVerifiesOriginalParametersAndQGISReadback(t *testing.T) {
	args := `{"items":[{"layer":"河流","symbol":{"type":"line","layers":[{"color":"#A6CEE3","width":0.46,"offset":0.4}]},"labels":{"field":"name","size":10,"unit":"pt","color":"#A6CEE3","placement":"line","merge_lines":true,"repeat":0,"bold":true,"obstacle":false}}],"scale":0.35}`
	want := `{"symbol":{"type":"line","layers":[{"color":"#a6cee3","width_mm":0.161,"offset_mm":0.14}]},"labels":{"enabled":true,"field":"name","size_mm":1.8,"color":"#a6cee3","font":"Noto Sans CJK SC","placement":"line","merge_lines":true,"repeat_mm":0,"bold":true,"obstacle":false}}`
	for _, tc := range []struct{ name, requested, actual, problem string }{
		{"matches", want, want, ""},
		{"width tolerance", want, strings.Replace(want, "0.161", "0.171", 1), ""},
		{"wrong width", want, strings.Replace(want, "0.161", "0.172", 1), "width_mm"},
		{"wrong color", want, strings.Replace(want, "#a6cee3", "#000000", 1), "color"},
		{"wrong field", want, strings.Replace(want, `"field":"name"`, `"field":"NAME"`, 1), "field"},
		{"labels disabled", want, strings.Replace(want, `"enabled":true`, `"enabled":false`, 1), "enabled"},
		{"wrong merged lines", want, strings.Replace(want, `"merge_lines":true`, `"merge_lines":false`, 1), "merge_lines"},
		{"wrong bold", want, strings.Replace(want, `"bold":true`, `"bold":false`, 1), "bold"},
		{"wrong obstacle", want, strings.Replace(want, `"obstacle":false`, `"obstacle":true`, 1), "obstacle"},
		{"wrong renderer", want, strings.Replace(want, `{`, `{"renderer":"QgsCategorizedSymbolRenderer",`, 1), "renderer"},
		{"echo cannot change request", strings.Replace(want, "0.161", "0.3", 1), strings.Replace(want, "0.161", "0.3", 1), "width_mm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errors := verifySymbology([]ToolSummary{styleCall(args, tc.requested, tc.actual)})
			if tc.problem == "" && len(errors) != 0 || tc.problem != "" && !strings.Contains(strings.Join(errors, ";"), tc.problem) {
				t.Fatalf("errors=%v want=%q", errors, tc.problem)
			}
		})
	}
}

func TestSymbologyRepairKeepsOtherLayerComponents(t *testing.T) {
	args := `{"items":[{"layer":"河流","symbol":{"type":"line","layers":[{"color":"#a6cee3","width":0.46}]},"labels":null}]}`
	want := `{"symbol":{"type":"line","layers":[{"color":"#a6cee3","width_mm":0.46}]},"labels":{"enabled":false}}`
	bad := strings.Replace(want, "0.46", "0.5", 1)
	first := styleCall(args, want, bad)
	labelOnly := styleCall(`{"items":[{"layer":"river-id","labels":null}]}`, `{"labels":{"enabled":false}}`, bad)
	if errors := verifySymbology([]ToolSummary{first, labelOnly}); len(errors) != 1 || !strings.Contains(errors[0], "width_mm") {
		t.Fatalf("unrepaired width was lost: %v", errors)
	}
	corrected := styleCall(args, want, want)
	if errors := verifySymbology([]ToolSummary{first, labelOnly, corrected}); len(errors) != 0 {
		t.Fatalf("corrected layer still fails: %v", errors)
	}
}

func TestReferencedStyleVerifiesRendererAndLabelSwitch(t *testing.T) {
	for _, actual := range []string{
		`{"renderer":"singleSymbol","symbol":{"type":"line"},"labels":{"enabled":false}}`,
		`{"renderer":"categorizedSymbol","symbol":{"type":"line"},"labels":{"enabled":false}}`,
		`{"renderer":"singleSymbol","symbol":{"type":"line"},"labels":{"enabled":true}}`,
	} {
		want := `{"renderer":"singleSymbol","symbol":{"type":"line"},"labels":{"enabled":false}}`
		call := ToolSummary{Name: "layer_load_style", Arguments: json.RawMessage(`{"layer":"铁路","style":"铁路","label_field":"无"}`), Result: `{"success":true,"data":{"layer_id":"rail-id","requested":` + want + `,"actual":` + actual + `}}`}
		errors := verifySymbology([]ToolSummary{call})
		if (len(errors) == 0) != (actual == want) {
			t.Fatalf("actual=%s errors=%v", actual, errors)
		}
	}
}

func TestSymbologyCanReplaceLoadedRenderer(t *testing.T) {
	loaded := `{"renderer":"QgsCategorizedSymbolRenderer","labels":{"enabled":false}}`
	qml := ToolSummary{Name: "layer_load_style", Arguments: json.RawMessage(`{"layer":"河流","style":"分类河流"}`), Result: `{"success":true,"data":{"layer_id":"river-id","requested":` + loaded + `,"actual":` + loaded + `}}`}
	args := `{"items":[{"layer":"river-id","symbol":{"type":"line","layers":[{"color":"#a6cee3","width":0.46}]}}]}`
	want := `{"symbol":{"type":"line","layers":[{"color":"#a6cee3","width_mm":0.46}]}}`
	actual := `{"renderer":"QgsSingleSymbolRenderer","symbol":{"type":"line","layers":[{"color":"#a6cee3","width_mm":0.46}]},"labels":{"enabled":false}}`
	if errors := verifySymbology([]ToolSummary{qml, styleCall(args, want, actual)}); len(errors) != 0 {
		t.Fatalf("the replaced QML renderer was still required: %v", errors)
	}
}

func TestFailedStyleToolIsNotSilentlyAccepted(t *testing.T) {
	call := ToolSummary{Name: "layer_load_style", Arguments: json.RawMessage(`{"layer":"铁路","style":"铁路"}`), Result: `{"success":false,"info":"样式文件不能读取"}`}
	if errors := verifySymbology([]ToolSummary{call}); len(errors) != 1 || !strings.Contains(errors[0], "样式文件不能读取") {
		t.Fatal(errors)
	}
}

func TestStyleReadbackSurvivesDefaultErrorEnvelope(t *testing.T) {
	args := `{"items":[{"layer":"河流","symbol":{"type":"line","layers":[{"color":"#a6cee3","width":0.46}]}},{"layer":"铁路","labels":null}]}`
	response := `{"error":"批量符号化失败","output":{"success":false,"data":{"items":[{"success":true,"layer_id":"river-id","requested":{"symbol":{"type":"line","layers":[{"color":"#a6cee3","width_mm":0.46}]}},"actual":{"renderer":"QgsSingleSymbolRenderer","symbol":{"type":"line","layers":[{"color":"#a6cee3","width_mm":0.46}]}}},{"success":false,"info":"铁路图层不存在"}]}}}`
	errors := verifySymbology([]ToolSummary{{Name: "layer_set_symbology", Arguments: json.RawMessage(args), Result: response}})
	if len(errors) != 1 || !strings.Contains(errors[0], "铁路图层不存在") {
		t.Fatalf("lost partial QGIS results inside runtime error envelope: %v", errors)
	}
}

func TestSymbologyRecordedQGISReadback(t *testing.T) {
	path := filepath.Join("..", "..", "..", "docs", "refactor-v7", "evidence", "D14-readback.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var examples []struct {
		Name  string
		Calls []struct {
			Tool      string
			Arguments json.RawMessage
			Result    json.RawMessage
		}
	}
	if err := json.Unmarshal(raw, &examples); err != nil {
		t.Fatal(err)
	}
	if len(examples) != 7 {
		t.Fatalf("expected the seven specified basemap layers, got %d", len(examples))
	}
	for _, example := range examples {
		t.Run(example.Name, func(t *testing.T) {
			for _, call := range example.Calls {
				if errors := verifySymbology([]ToolSummary{{Name: call.Tool, Arguments: call.Arguments, Result: string(call.Result)}}); len(errors) > 0 {
					t.Fatal(errors)
				}
			}
		})
	}
}

func TestControllerRepairsSymbologyThroughExistingStepLoop(t *testing.T) {
	args := `{"items":[{"layer":"河流","symbol":{"type":"line","layers":[{"color":"#a6cee3","width":0.46}]}}]}`
	want := `{"symbol":{"type":"line","layers":[{"color":"#a6cee3","width_mm":0.46}]}}`
	first := styleCall(args, want, strings.Replace(want, "0.46", "0.5", 1))
	p := testPlan(1)
	var requests []StepRequest
	c, request, _ := newTestController(t, p, func(_ context.Context, r StepRequest) (StepResult, error) {
		requests = append(requests, r)
		result := completed(r.Step)
		result.StyleTools = []ToolSummary{first}
		if len(requests) == 2 {
			result.StyleTools = append(result.StyleTools, styleCall(args, want, want))
		}
		return result, nil
	}, nil)
	run, err := c.Start(context.Background(), request)
	if err != nil || run.Status != "completed" || len(requests) != 2 {
		t.Fatalf("status=%v requests=%d err=%v", run, len(requests), err)
	}
	if requests[0].Step.ID != requests[1].Step.ID || requests[0].Role.ID != requests[1].Role.ID || !strings.Contains(requests[1].Repair, "width_mm") {
		t.Fatalf("repair did not return exact mismatch to original step: %+v", requests[1])
	}
	if run.Steps[0].Attempts[0].Verification.OK || !run.Steps[0].Attempts[1].Verification.OK {
		t.Fatalf("readback was not part of output verification: %+v", run.Steps[0].Attempts)
	}
}
