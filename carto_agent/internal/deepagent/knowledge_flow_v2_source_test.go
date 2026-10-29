package deepagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/planexec"
	"github.com/autocarto/carto_agent/internal/taskmaterials"
	"github.com/autocarto/carto_agent/internal/toolsession"
	"github.com/cloudwego/eino/adk"
)

func TestV2KnowledgeReadsOnlySelectedFilesInsideProvidedFolder(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	project := filepath.Join(t.TempDir(), "project.qgz")
	sid, folder := "v2-folder-source", t.TempDir()
	first, second := filepath.Join(folder, "史料甲.txt"), filepath.Join(folder, "史料乙.txt")
	texts := map[string]string{first: "  部队从甲地出发，到达乙地。\n", second: "第二支部队从丙地到丁地。", filepath.Join(folder, "不用的材料.txt"): "这份材料不属于本次抽取。"}
	for path, text := range texts {
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := config.ResolveSessionDir(project, sid)
	if err != nil {
		t.Fatal(err)
	}
	if err = dataservice.SaveBindings(dir, []dataservice.Binding{{Name: "材料文件夹", Path: folder, Explicit: true, Current: true}}); err != nil {
		t.Fatal(err)
	}
	original := "本次消息原文：部队由戊地向己地转移。"
	ctx := knowledge.WithOriginalSource(toolsession.WithContext(context.Background(), sid, project, ""), original)
	ctx, err = taskmaterials.Prepare(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		refs []string
	}{
		{"selected_child", []string{first}},
		{"two_selected_children", []string{first, second}},
		{"message_despite_folder_attachment", []string{"消息原文"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &plan.Plan{APIVersion: plan.APIVersion, ID: "selected-material", Version: 1, Goal: "抽取指定原文"}
			inputs := map[string]any{}
			step := plan.Step{ID: "extract", Title: "抽取路线", Capabilities: []string{"route_knowledge"}, Produces: []plan.Product{{ID: "route", Kind: "dataset", Format: "route_dataset"}}}
			for i, ref := range tc.refs {
				id := []string{"source_a", "source_b"}[i]
				p.Data = append(p.Data, plan.Data{ID: id, Name: id, Source: "user", Ref: ref, Kind: "file"})
				step.Inputs = append(step.Inputs, id)
				inputs[id] = ref
				if ref == "消息原文" {
					inputs[id] = map[string]any{"source": "user", "text": original}
				}
			}
			p.Steps = []plan.Step{step}
			executable, bindings, err := planexec.Build(p, planexec.BuildOptions{Bindings: inputs, Environment: flow.Environment{Roles: []flow.RoleSpec{{ID: "knowledge_engineer", Kind: "executor", Executor: "route_knowledge", Enabled: true}}}})
			if err != nil {
				t.Fatal(err)
			}
			messages, err := flowrun.NewRouteKnowledgeExecutor().BuildInput(executable.Steps[0], &flowrun.RunState{CartoPlan: p, Plan: executable, Inputs: bindings})
			if err != nil {
				t.Fatal(err)
			}
			var request map[string]any
			if err = json.Unmarshal([]byte(messages[0].Content), &request); err != nil {
				t.Fatal(err)
			}
			got, _, err := resolveKnowledgeInputWithContext(ctx, &adk.AgentInput{Messages: messages})
			if err != nil {
				t.Fatal(err)
			}
			if len(tc.refs) == 1 {
				wantRef, wantText := tc.refs[0], texts[tc.refs[0]]
				if wantRef == "消息原文" {
					wantRef, wantText = "current_user_message", original
				}
				if request["source_ref"] != wantRef || got != wantText {
					t.Fatalf("selected source was replaced: request=%v text=%q", request, got)
				}
			} else {
				if request["source_ref"] != nil || !reflect.DeepEqual(request["source_refs"], []any{first, second}) || !strings.Contains(got, texts[first]) || !strings.Contains(got, texts[second]) {
					t.Fatalf("selected source collection was lost: request=%v text=%q", request, got)
				}
			}
			if strings.Contains(got, "这份材料不属于本次抽取") {
				t.Fatal("unselected folder child was extracted")
			}
		})
	}
}
