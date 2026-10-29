package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
	"github.com/autocarto/carto_agent/internal/taskmaterials"
	"github.com/autocarto/carto_agent/internal/toolsession"
	"github.com/cloudwego/eino/schema"
)

func bindMaterialPlanFiles(t *testing.T, s *DeepServer, run flowSessionContext, paths ...string) context.Context {
	t.Helper()
	ctx := toolsession.WithContext(context.Background(), run.SessionID, run.ProjectPath, "")
	attachments := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		attachments = append(attachments, map[string]any{"path": path})
	}
	message := Message{Content: run.OriginalSource, Data: map[string]any{"attachments": attachments}}
	if err := s.bindTaskReferences(ctx, run.SessionID, run.ProjectPath, message, schema.UserMessage(message.Content)); err != nil {
		t.Fatal(err)
	}
	return ctx
}

// Read precisely the inventory shown to the model; never recreate its ID hash.
func materialPlanReferences(t *testing.T, ctx context.Context) []taskmaterials.Reference {
	t.Helper()
	message, err := taskmaterials.ContextMessage(ctx)
	if err != nil || message == nil {
		t.Fatalf("missing real attachment context: %v %v", message, err)
	}
	_, body, found := strings.Cut(message.Content, "\n")
	if !found {
		t.Fatal("material context has no JSON inventory")
	}
	var refs []taskmaterials.Reference
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&refs); err != nil {
		t.Fatal(err)
	}
	return refs
}

func materialSourcePlan(refs ...string) *plan.Plan {
	p := &plan.Plan{APIVersion: plan.APIVersion, ID: "cp-material-input", Version: 1, Goal: "按用户提供的史料抽取路线"}
	step := plan.Step{ID: "extract", Title: "抽取路线知识", Capabilities: []string{"route_knowledge"}, Instructions: "忠于已选原文", Produces: []plan.Product{{ID: "route", Kind: "dataset", Format: "route_dataset"}}}
	step.Parameters = map[string]string{"route_style": "红蓝对比", "symbols": "军标"}
	for i, ref := range refs {
		id := fmt.Sprintf("source_%d", i+1)
		p.Data = append(p.Data, plan.Data{ID: id, Name: id, Kind: "file", Source: "user", Ref: ref})
		step.Inputs = append(step.Inputs, id)
	}
	p.Steps = []plan.Step{step}
	return p
}

func proposeMaterialSource(t *testing.T, s *DeepServer, run flowSessionContext, ctx context.Context, refs ...string) *preparedCartographicPlan {
	t.Helper()
	raw, err := json.Marshal(materialSourcePlan(refs...))
	if err != nil {
		t.Fatal(err)
	}
	answer, err := (sessionPlanService{server: s, run: run}).ProposePlan(ctx, raw)
	if err != nil || answer["status"] != "ready" {
		t.Fatalf("provided source did not reach executable plan: answer=%v err=%v", answer, err)
	}
	prepared := s.takeCartographicPlan(run.SessionID)
	if prepared == nil {
		t.Fatal("accepted source was not queued for execution")
	}
	return prepared
}

func knowledgeMaterialRefs(t *testing.T, prepared *preparedCartographicPlan) []string {
	t.Helper()
	messages, err := flowrun.NewRouteKnowledgeExecutor().BuildInput(prepared.Executable.Steps[0], &flowrun.RunState{CartoPlan: prepared.Snapshot.Plan, Plan: prepared.Executable, Inputs: prepared.Inputs})
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Ref  string   `json:"source_ref"`
		Refs []string `json:"source_refs"`
	}
	if err := json.Unmarshal([]byte(messages[0].Content), &request); err != nil {
		t.Fatal(err)
	}
	if request.Ref != "" {
		return []string{request.Ref}
	}
	return request.Refs
}

func TestMaterialPlanSingleFileIdentityReadsExactBody(t *testing.T) {
	for _, kind := range []string{"attachment_id", "session_file_id", "name", "attachment_name", "path", "fresh_context", "large_attachment_id"} {
		t.Run(kind, func(t *testing.T) {
			s, _, run, _ := cartoRuntimeFixture(t)
			run.OriginalSource = "根据上传史料制作路线图"
			path := filepath.Join(t.TempDir(), "史料原文.txt")
			body := "  红军由何家冲出发，经桐柏至枣阳。\n这句仅存在于文件中。\n"
			if kind == "large_attachment_id" {
				body = strings.Repeat(body, 600)
			}
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			ctx := bindMaterialPlanFiles(t, s, run, path)
			ctx, err := taskmaterials.Prepare(ctx)
			if err != nil {
				t.Fatal(err)
			}
			inventory := materialPlanReferences(t, ctx)
			if len(inventory) != 1 || (kind != "large_attachment_id" && inventory[0].Text != body) || (kind == "large_attachment_id" && inventory[0].Text != "") {
				t.Fatalf("source inventory did not contain actual file bytes: %+v", inventory)
			}
			ref := inventory[0].ID
			switch kind {
			case "session_file_id":
				store, err := sessionfiles.New(run.ProjectPath, run.SessionID)
				if err != nil {
					t.Fatal(err)
				}
				item, err := store.GetPath(path)
				if err != nil {
					t.Fatal(err)
				}
				ref = item.ID
				if ref == inventory[0].ID {
					t.Fatal("fixture did not distinguish the two ID namespaces")
				}
			case "name":
				ref = filepath.Base(path)
			case "attachment_name":
				ref = "附件:" + filepath.Base(path)
			case "path":
				ref = path
			case "fresh_context":
				// Recreate the request context using only persisted user bindings.
				ctx = toolsession.WithContext(context.Background(), run.SessionID, run.ProjectPath, "")
			}
			prepared := proposeMaterialSource(t, s, run, ctx, ref)
			canonical, err := filepath.EvalSymlinks(path)
			if err != nil {
				t.Fatal(err)
			}
			data := prepared.Snapshot.Plan.Data[0]
			if data.Ref != canonical || prepared.Inputs[data.ID] != canonical {
				t.Fatalf("plan did not bind the canonical selected source: data=%+v input=%v", data, prepared.Inputs[data.ID])
			}
			refs := knowledgeMaterialRefs(t, prepared)
			fresh := toolsession.WithContext(context.Background(), run.SessionID, run.ProjectPath, "")
			actual, err := taskmaterials.ResolveSource(fresh, refs)
			if err != nil || actual != body || actual == run.OriginalSource {
				t.Fatalf("selected file was replaced/truncated: refs=%v actual=%q err=%v", refs, actual, err)
			}
		})
	}
}

func TestMaterialPlanFolderUsesOnlySelectedChild(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprintf("nested_%t", nested), func(t *testing.T) {
			s, _, run, _ := cartoRuntimeFixture(t)
			folder := t.TempDir()
			path := filepath.Join(folder, "选中的史料.txt")
			if nested {
				path = filepath.Join(folder, "第二卷", "附件", "选中的史料.txt")
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
			}
			body := "  仅选中的文件：从甲地向乙地转移，再返回甲地。\n"
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(folder, "不应读取.txt"), []byte("不属于这次抽取"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx := bindMaterialPlanFiles(t, s, run, folder)
			inventory := materialPlanReferences(t, ctx)
			ref := path
			if !nested {
				ref = ""
				for _, child := range inventory[0].Entries {
					if child.Name == filepath.Base(path) {
						ref = child.ID
					}
				}
				if ref == "" {
					t.Fatal("folder inventory did not expose selected child")
				}
			}
			prepared := proposeMaterialSource(t, s, run, ctx, ref)
			actual, err := taskmaterials.ResolveSource(ctx, knowledgeMaterialRefs(t, prepared))
			if err != nil || actual != body {
				t.Fatalf("folder input did not select exact child: actual=%q err=%v", actual, err)
			}
		})
	}
}

func TestMaterialPlanDuplicateNamesDoNotGuess(t *testing.T) {
	s, _, run, _ := cartoRuntimeFixture(t)
	paths := []string{filepath.Join(t.TempDir(), "史料.txt"), filepath.Join(t.TempDir(), "史料.txt")}
	for i, path := range paths {
		if err := os.WriteFile(path, []byte(fmt.Sprintf("不同原文 %d", i)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx := bindMaterialPlanFiles(t, s, run, paths...)
	inventory := materialPlanReferences(t, ctx)
	if len(inventory) != 2 || inventory[0].ID == inventory[1].ID {
		t.Fatal("distinct files lost their identity")
	}
	raw, _ := json.Marshal(materialSourcePlan("史料.txt"))
	answer, err := (sessionPlanService{server: s, run: run}).ProposePlan(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if answer["status"] != "needs_data" || s.hasPendingCartographicPlan(run.SessionID) {
		t.Fatalf("ambiguous name guessed an executable file: %v", answer)
	}
	store, err := planStore(run)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.Load("cp-material-input")
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Precheck) != 1 || saved.Precheck[0].Status != "unavailable" || saved.Precheck[0].Reason == "" {
		t.Fatalf("ambiguous source has no actionable precheck: %+v", saved.Precheck)
	}
}

func TestMaterialPlanDefaultUsesCurrentDocumentsNotOldAttachment(t *testing.T) {
	s, _, run, _ := cartoRuntimeFixture(t)
	folder := t.TempDir()
	old := filepath.Join(folder, "旧材料.txt")
	first, second := filepath.Join(folder, "本次甲.txt"), filepath.Join(folder, "本次乙.txt")
	texts := map[string]string{old: "不属于本轮的旧史料", first: "  本轮甲：甲地到乙地。\n", second: "本轮乙：丙地到丁地。\n"}
	for path, text := range texts {
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	bindMaterialPlanFiles(t, s, run, old)
	ctx := bindMaterialPlanFiles(t, s, run, first, second)
	inventory := materialPlanReferences(t, ctx)
	current := 0
	for _, item := range inventory {
		if item.Current {
			current++
		}
	}
	if len(inventory) != 3 || current != 2 {
		t.Fatalf("wrong fixture current set: %+v", inventory)
	}
	prepared := proposeMaterialSource(t, s, run, ctx)
	refs := knowledgeMaterialRefs(t, prepared)
	if len(refs) != 2 {
		t.Fatalf("default plan lost current documents: %v", refs)
	}
	canonicalFirst, _ := filepath.EvalSymlinks(first)
	canonicalSecond, _ := filepath.EvalSymlinks(second)
	if refs[0] != canonicalFirst || refs[1] != canonicalSecond {
		t.Fatalf("wrong default source order/identity: %v", refs)
	}
	actual, err := taskmaterials.ResolveSource(ctx, refs)
	want := fmt.Sprintf("--- 来源文件：%s ---\n%s\n\n--- 来源文件：%s ---\n%s", canonicalFirst, texts[first], canonicalSecond, texts[second])
	if err != nil || actual != want || strings.Contains(actual, texts[old]) {
		t.Fatalf("default extraction changed its source: actual=%q want=%q err=%v", actual, want, err)
	}
}

func TestMaterialPlanMessageReferenceDoesNotBecomeUploadedText(t *testing.T) {
	for _, token := range []string{"消息原文", "current_user_message"} {
		t.Run(token, func(t *testing.T) {
			s, _, run, _ := cartoRuntimeFixture(t)
			run.OriginalSource = "用户直接粘贴的原文：从甲地行军到乙地。"
			path := filepath.Join(t.TempDir(), "另一份材料.txt")
			if err := os.WriteFile(path, []byte("附件原文完全不同，不得替换当前消息"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx := bindMaterialPlanFiles(t, s, run, path)
			prepared := proposeMaterialSource(t, s, run, ctx, token)
			data := prepared.Snapshot.Plan.Data[0]
			binding, ok := prepared.Inputs[data.ID].(map[string]any)
			if !ok || binding["text"] != run.OriginalSource {
				t.Fatalf("message source was replaced: %v", prepared.Inputs)
			}
			refs := knowledgeMaterialRefs(t, prepared)
			if len(refs) != 1 || refs[0] != "current_user_message" {
				t.Fatalf("message became a file reference: %v", refs)
			}
		})
	}
}
