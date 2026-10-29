package deepagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
	"github.com/autocarto/carto_agent/internal/taskmaterials"
	"github.com/autocarto/carto_agent/internal/toolsession"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestKnowledgeUsesOriginalAttachedBytesInsteadOfTaskDescription(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	project := filepath.Join(t.TempDir(), "project.qgz")
	sid := "knowledge-materials"
	path := filepath.Join(t.TempDir(), "史料.txt")
	source := "  红军从何家冲出发。\n\n途经朱堂，到达桐柏。  \n"
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	dir, err := config.ResolveSessionDir(project, sid)
	if err != nil {
		t.Fatal(err)
	}
	if err = dataservice.SaveBindings(dir, []dataservice.Binding{{Name: "史料.txt", Path: path, Explicit: true}}); err != nil {
		t.Fatal(err)
	}
	store, err := sessionfiles.New(project, sid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.RegisterProvided(path, sessionfiles.Origin{Type: "provided"}); err != nil {
		t.Fatal(err)
	}
	ctx := knowledge.WithOriginalSource(toolsession.WithContext(context.Background(), sid, project, ""), "根据上传的史料制作行军路线图")
	ctx, err = taskmaterials.Prepare(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []any{
		"读取史料文件内容", path,
		map[string]any{"source_ref": path},
		map[string]any{"source_ref": "current_task_input"},
		map[string]any{"source_refs": []string{path}},
	} {
		text, ok := input.(string)
		if !ok {
			raw, _ := json.Marshal(input)
			text = string(raw)
		}
		got, _, err := resolveKnowledgeInputWithContext(ctx, &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage(text)}})
		if err != nil || got != source {
			t.Fatalf("source lost through %s: %q %v", text, got, err)
		}
	}
	t.Run("explicit_current_message_is_not_replaced_by_previous_attachment", func(t *testing.T) {
		pasted := "  新史料：部队由丙地转移至丁地。\n"
		next := knowledge.WithOriginalSource(ctx, pasted)
		input := &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage(`{"source_ref":"current_user_message"}`)}}
		got, _, err := resolveKnowledgeInputWithContext(next, input)
		if err != nil || got != pasted {
			t.Fatalf("new pasted source replaced by previous attachment: %q %v", got, err)
		}
	})
	// The source snapshot survives delegation without requiring the main model
	// to transcribe it or silently switching to bytes changed halfway through.
	if err := os.WriteFile(path, []byte("changed after binding"), 0600); err != nil {
		t.Fatal(err)
	}
	got, _, err := resolveKnowledgeInputWithContext(ctx, &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage("继续抽取")}})
	if err != nil || got != source {
		t.Fatalf("delegation did not preserve original snapshot: %q %v", got, err)
	}
}

func TestKnowledgeRejectsDelegationWithoutSourceButKeepsExplicitAndPastedText(t *testing.T) {
	ctx := knowledge.WithOriginalSource(context.Background(), "请制作行军路线图")
	input := &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage("读取史料文件内容")}}
	if _, _, err := resolveKnowledgeInputWithContext(ctx, input); err == nil || !strings.Contains(err.Error(), "不能把子任务描述当作史料") {
		t.Fatalf("delegation description treated as source: %v", err)
	}
	for _, text := range []string{"红军从甲地到达乙地。", `{"source_text":"红军从甲地到达乙地。"}`} {
		got, _, err := resolveKnowledgeInput(&adk.AgentInput{Messages: []*schema.Message{schema.UserMessage(text)}})
		if err != nil || got != "红军从甲地到达乙地。" {
			t.Fatalf("explicit historical source rejected: %q %v", got, err)
		}
	}
	pasted := "红军从甲地到达乙地。"
	for _, ref := range []string{"current_user_message", "current_task_input"} {
		raw, _ := json.Marshal(map[string]any{"source_ref": ref})
		input := &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage(string(raw))}}
		got, _, err := resolveKnowledgeInputWithContext(knowledge.WithOriginalSource(context.Background(), pasted), input)
		if err != nil || got != pasted {
			t.Fatalf("%s lost trusted source without attachments: %q %v", ref, got, err)
		}
	}
}
