package flowrun

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
)

func TestConditionalAcquisitionReusesProvidedInputs(t *testing.T) {
	for _, tc := range []struct {
		name      string
		input     string
		shouldRun bool
	}{
		{name: "provided statistical table", input: "统计表"},
		{name: "provided thematic layer", input: "已有专题图层"},
		{name: "both inputs missing", shouldRun: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testPlan(2)
			p.Inputs = []flow.InputPlan{
				{Key: "统计表", Label: "统计表", Type: "file"},
				{Key: "已有专题图层", Label: "已有专题图层", Type: "layer"},
			}
			p.Steps[0].Condition = &flow.ConditionDoc{Text: "未提供「统计表」且未提供「已有专题图层」时执行", Rule: flow.ParseCondition("未提供「统计表」且未提供「已有专题图层」时执行")}
			p.Steps[0].ExpectedOutputs = []flow.OutputDeclaration{{Label: "联网统计表", Type: "file", Format: "csv", Required: true}}
			p.Steps[1].Refs.Outputs = []flow.OutputRef{{Step: "s1", Label: "联网统计表"}}
			calls := []string{}
			var acquired string
			c, request, _ := newTestController(t, p, func(_ context.Context, r StepRequest) (StepResult, error) {
				calls = append(calls, r.Step.ID)
				if r.Step.ID == "s1" {
					if !tc.shouldRun {
						t.Fatal("online acquisition ran despite an existing table or thematic layer")
					}
					acquired = filepath.Join(r.SessionDir, "acquired.csv")
					if err := os.WriteFile(acquired, []byte("地区,年份,数值\n测试区域,2024,1\n"), 0o600); err != nil {
						return StepResult{}, err
					}
					raw, _ := json.Marshal(Receipt{Status: "completed", Outputs: map[string]any{"联网统计表": acquired}})
					return StepResult{LastContent: string(raw)}, nil
				}
				if tc.shouldRun && (len(r.Messages) == 0 || !strings.Contains(r.Messages[0].Content, acquired)) {
					t.Fatal("verified acquisition output was not passed to the consumer")
				}
				return completed(r.Step), nil
			}, func(o *Options) {
				o.Model = modelFunc(func(context.Context, string) (string, error) {
					t.Fatal("all-missing input condition should not call a model")
					return "", nil
				})
			})
			request.Inputs = map[string]any{}
			if tc.input == "统计表" {
				path := filepath.Join(request.SessionDir, "provided.csv")
				if err := os.WriteFile(path, []byte("地区,年份,数值\n测试区域,2024,2\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				request.Inputs[tc.input] = path
			} else if tc.input == "已有专题图层" {
				request.Inputs[tc.input] = "existing-layer-id"
			}

			// Capability checks use the same deterministic condition as execution.
			p.Steps[0].Unresolved = "FLOW_CAPABILITY_MISSING_IN_ROLE"
			issues := flow.RoleIssues(p, request.Inputs, nil)
			if (len(issues) > 0) != tc.shouldRun {
				t.Fatalf("unused acquisition capability blocked input reuse: %+v", issues)
			}
			p.Steps[0].Unresolved = ""

			run, err := c.Start(context.Background(), request)
			if err != nil || run.Status != "completed" {
				t.Fatalf("conditional flow failed: run=%+v error=%v", run, err)
			}
			wantStatus, wantCalls := "skipped", 1
			if tc.shouldRun {
				wantStatus, wantCalls = "completed", 2
			}
			if run.Steps[0].Status != wantStatus || len(calls) != wantCalls || run.Steps[0].Condition.By != "rule" {
				t.Fatalf("unexpected execution: state=%+v calls=%v", run.Steps[0], calls)
			}
			if !tc.shouldRun && len(run.Steps[0].Outputs) != 0 {
				t.Fatalf("skipped acquisition should not manufacture a file output: %+v", run.Steps[0].Outputs)
			}
		})
	}
}
