package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
)

func fixture() *Plan {
	return &Plan{APIVersion: LegacyAPIVersion, ID: "cp-test", Version: 1, Goal: "成图", Data: []Data{{ID: "d1", Name: "原文", From: "conversation"}, {ID: "d2", Name: "数据集", From: "step", Step: "s1"}}, Steps: []Step{{ID: "s1", Title: "抽取", Capabilities: []string{"路线知识抽取"}, Uses: []string{"d1"}, Instructions: "抽取真实信息", Outputs: []flow.OutputDeclaration{{Label: "数据集", Type: "dataset", Format: "route_dataset"}}}, {ID: "s2", Title: "导出", Capabilities: []string{"export"}, Uses: []string{"d2"}, DependsOn: []string{"s1"}, Instructions: "导出成果", Outputs: []flow.OutputDeclaration{{Label: "PDF", Type: "file", Format: "pdf"}}}}, Deliverables: []flow.OutputDeclaration{{Label: "PDF", Type: "file", Format: "pdf"}}}
}
func environment() Environment {
	return Environment{Capabilities: map[string]bool{"路线知识抽取": true, "export": true}}
}
func TestValidationRejectsInvalidPlans(t *testing.T) {
	tests := []struct {
		name, code string
		change     func(*Plan, *Environment)
	}{
		{"schema", "PLAN_SCHEMA_INVALID", func(p *Plan, e *Environment) { p.APIVersion = "unknown" }},
		{"unknown data kind", "PLAN_SCHEMA_INVALID", func(p *Plan, e *Environment) { p.Data[0].Kind = "table" }},
		{"duplicate", "PLAN_REF_INVALID", func(p *Plan, e *Environment) { p.Steps[1].ID = "s1" }},
		{"future dependency", "PLAN_REF_INVALID", func(p *Plan, e *Environment) { p.Steps[0].DependsOn = []string{"s2"} }},
		{"future data", "PLAN_REF_INVALID", func(p *Plan, e *Environment) { p.Steps[0].Uses = []string{"d2"} }},
		{"unknown data", "PLAN_REF_INVALID", func(p *Plan, e *Environment) { p.Steps[1].Uses = []string{"missing"} }},
		{"unknown capability", "PLAN_CAPABILITY_UNKNOWN", func(p *Plan, e *Environment) { p.Steps[0].Capabilities = []string{"imagined"} }},
		{"invalid output", "PLAN_OUTPUT_INVALID", func(p *Plan, e *Environment) { p.Steps[0].Outputs[0].Type = "anything" }},
		{"missing final", "PLAN_OUTPUT_INVALID", func(p *Plan, e *Environment) { p.Deliverables[0].Label = "made-up" }},
		{"missing producer output", "PLAN_OUTPUT_INVALID", func(p *Plan, e *Environment) { p.Data[1].Output = "invented" }},
		{"fabricated source", "PLAN_SOURCE_NOT_FROM_FIND", func(p *Plan, e *Environment) {
			p.Data = append(p.Data, Data{ID: "d3", Name: "底图", From: "acquire", FindID: "missing", Choice: "c999"})
			e.ValidateSource = func(id string, c any) error { return fmt.Errorf("unknown candidate") }
		}},
		{"missing file", "PLAN_DATA_MISSING", func(p *Plan, e *Environment) { p.Data[0].From = "user"; p.Data[0].Path = "/missing-test-file" }},
		{"missing layer", "PLAN_DATA_MISSING", func(p *Plan, e *Environment) { p.Data[0].From = "project"; p.Data[0].Layer = "fake" }},
		{"missing session file", "PLAN_DATA_MISSING", func(p *Plan, e *Environment) { p.Data[0].From = "session"; p.Data[0].FileID = "fake" }},
		{"missing style", "PLAN_STYLE_NOT_FOUND", func(p *Plan, e *Environment) { p.Data[0].Style = "不存在" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := fixture()
			env := environment()
			tc.change(p, &env)
			ds := Validate(p, env)
			for _, d := range ds {
				if d.Code == tc.code {
					return
				}
			}
			t.Fatalf("missing %s: %+v", tc.code, ds)
		})
	}
}
func TestValidReferencesAndLocalLookups(t *testing.T) {
	p := fixture()
	env := environment()
	if d := Validate(p, env); len(d) != 0 {
		t.Fatal(d)
	}
	path := filepath.Join(t.TempDir(), "data.csv")
	if e := os.WriteFile(path, []byte("x,y\n1,2"), 0600); e != nil {
		t.Fatal(e)
	}
	p.Data[0].From = "user"
	p.Data[0].Path = path
	if e := RequireValid(p, env); e != nil {
		t.Fatal(e)
	}
	for _, from := range []string{"project", "session"} {
		p.Data[0].From = from
		env.ValidateData = func(d Data) error { return nil }
		if e := RequireValid(p, env); e != nil {
			t.Fatal(e)
		}
	}
	p.Data[0].From = "acquire"
	p.Data[0].FindID = "df-local"
	p.Data[0].Choice = "c1"
	calls := 0
	env.ValidateSource = func(id string, c any) error {
		calls++
		if id != "df-local" || c != "c1" {
			return fmt.Errorf("unexpected")
		}
		return nil
	}
	if e := RequireValid(p, env); e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	p.Data[0].Style = "河流线"
	env.StyleExists = func(name string) bool { return name == "河流线" }
	if ds := Validate(p, env); len(ds) != 0 {
		t.Fatal(ds)
	}
	p.Data[0].Style = ""
	if ds := Validate(p, env); len(ds) != 0 {
		t.Fatal("unselected styles must not be required", ds)
	}

}
func TestDecodeDoesNotAcceptUnknownFieldsOrTrailingJSON(t *testing.T) {
	for _, raw := range []string{`{"api_version":"carto-plan/v1","invented":1}`, `{} {}`, `{"version":"one"}`} {
		if _, e := Decode([]byte(raw)); e == nil {
			t.Fatal(raw)
		}
	}
}
func TestStoreVersionsAndConcurrentEdits(t *testing.T) {
	dir := t.TempDir()
	s, e := NewStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	p := fixture()
	if e = s.Save(&Snapshot{Plan: p, SessionID: "session", Status: "draft"}, 0); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other, _ := NewStore(dir)
			next, _ := Clone(p)
			next.Version = 2
			if other.Save(&Snapshot{Plan: next, SessionID: "session"}, 1) == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("accepted competing edits: %d", success)
	}
	if _, e = os.Stat(filepath.Join(dir, "plans", "cp-test.v1.json")); e != nil {
		t.Fatal(e)
	}
	list, e := s.List()
	if e != nil || len(list) != 1 || list[0].Plan.Version != 2 {
		t.Fatal(list, e)
	}
	if _, e = s.Load("../elsewhere"); e == nil {
		t.Fatal("path traversal")
	}
	if _, e = s.UpdateStatus(p.ID, 2, func(v *Snapshot) error { v.Plan.Goal = "changed"; return nil }); e == nil {
		t.Fatal("state mutation changed plan")
	}
	if _, e = s.UpdateStatus(p.ID, 2, func(v *Snapshot) error { v.Status = "cancelled"; return nil }); e != nil {
		t.Fatal(e)
	}
}

func TestAcquireMustPrecedeConsumerAndCannotRepeat(t *testing.T) {
	p := fixture()
	env := environment()
	env.ValidateSource = func(string, any) error { return nil }
	p.Data = append(p.Data, Data{ID: "download", Name: "底图", From: "acquire", FindID: "df", Choice: "auto"})
	p.Steps[0].Uses = append(p.Steps[0].Uses, "download")
	// No explicit acquire is valid: execution inserts it before the first consumer.
	if err := RequireValid(p, env); err != nil {
		t.Fatal(err)
	}
	p.Steps = append(p.Steps, Step{ID: "get", Title: "取数", Kind: "acquire", Data: []string{"download"}})
	if err := RequireValid(p, env); err == nil {
		t.Fatal("consumer before explicit acquisition accepted")
	}
	p.Steps = append([]Step{p.Steps[2]}, p.Steps[:2]...)
	if err := RequireValid(p, env); err != nil {
		t.Fatal(err)
	}
	p.Steps = append(p.Steps, Step{ID: "get-again", Title: "重复", Kind: "acquire", Data: []string{"download"}})
	if err := RequireValid(p, env); err == nil {
		t.Fatal("duplicate acquisition accepted")
	}
}
