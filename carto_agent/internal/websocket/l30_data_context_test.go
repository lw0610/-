package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/cloudwego/eino/adk"
	einoskill "github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestL30LiteralPathAndAttachmentBoundWithoutPrefix(t *testing.T) {
	s, _, run, _ := cartoRuntimeFixture(t)
	base := t.TempDir()
	prefix := filepath.Join(base, "given")
	os.Mkdir(prefix, 0700)
	path := prefix + " file.geojson"
	os.WriteFile(path, []byte("{}"), 0600)
	user := schema.UserMessage("读取 `" + path + "`，保留原文")
	if e := s.bindTaskReferences(context.Background(), run.SessionID, run.ProjectPath, Message{Data: map[string]any{"layers": []map[string]any{{"id": "project-layer", "name": "河流"}}}}, user); e != nil {
		t.Fatal(e)
	}
	dir, _ := config.ResolveSessionDir(run.ProjectPath, run.SessionID)
	bs, e := dataservice.LoadBindings(dir)
	if e != nil || len(bs) != 1 || bs[0].Path != path || !bs[0].Explicit {
		t.Fatal(bs, e)
	}
	if user.Content != "读取 `"+path+"`，保留原文" || user.Extra["layers"] == nil {
		t.Fatal(user)
	}
}
func TestL30WeakFolderMatchRequiresConfirmationAndRetainsSource(t *testing.T) {
	s, _, run, p := cartoRuntimeFixture(t)
	s.configService = config.NewConfigService(nil, &config.Config{}, "", filepath.Join(t.TempDir(), "config.yaml"))
	dir := t.TempDir()
	file := filepath.Join(dir, "海岸线.geojson")
	os.WriteFile(file, []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"LineString","coordinates":[]},"properties":{}}]}`), 0600)
	user := schema.UserMessage("使用 `" + dir + "`")
	if e := s.bindTaskReferences(context.Background(), run.SessionID, run.ProjectPath, Message{}, user); e != nil {
		t.Fatal(e)
	}
	service, e := s.dataService(context.Background(), run.SessionID)
	if e != nil {
		t.Fatal(e)
	}
	found, e := service.Find(context.Background(), dataservice.FindRequest{Need: dataservice.Need{Name: "河流", What: dataservice.What{Description: "河流线"}, Geometry: "line"}, Provided: []string{dir}})
	if e != nil || len(found.ProvidedMatches) != 1 {
		t.Fatal(found, e)
	}
	c := found.ProvidedMatches[0]
	if !c.RequiresConfirmation {
		t.Fatal(c)
	}
	p.Data = []plan.Data{{ID: "river", Name: "河流", From: "user", Path: file, Geometry: "line", LayerName: "河流线", FindID: found.FindID, Choice: c.ID}}
	p.Steps[0].Uses = []string{"river"}
	prepared, e := s.prepareCartographicPlan(context.Background(), run, p, nil)
	if e != nil {
		t.Fatal(e)
	}
	if prepared.Snapshot.Status != "awaiting_confirmation" || len(prepared.Snapshot.FindResults) != 1 || prepared.Snapshot.Plan.Data[0].Path != file {
		t.Fatal(prepared.Snapshot)
	}
	p.Data[0].Geometry = "polygon"
	if _, e = s.prepareCartographicPlan(context.Background(), run, p, nil); e == nil {
		t.Fatal("wrong geometry accepted")
	}
}

type l30GuideModel struct {
	t             *testing.T
	calls         int
	schemaChecked bool
	result        bool
}

func (m *l30GuideModel) WithTools(infos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	for _, i := range infos {
		if i.Name == "skill" {
			j, e := i.ParamsOneOf.ToJSONSchema()
			if e != nil {
				return nil, e
			}
			b, _ := json.Marshal(j)
			var v map[string]any
			json.Unmarshal(b, &v)
			props := v["properties"].(map[string]any)
			if props["skill"] == nil || props["name"] != nil {
				m.t.Fatal(string(b))
			}
			m.schemaChecked = true
		}
	}
	return m, nil
}
func (m *l30GuideModel) Generate(_ context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	if _, e := m.WithTools(model.GetCommonOptions(&model.Options{}, opts...).Tools); e != nil {
		return nil, e
	}
	m.calls++
	if m.calls == 1 {
		return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "read-guide", Type: "function", Function: schema.FunctionCall{Name: "skill", Arguments: `{"skill":"guide-statistical-choropleth"}`}}}}, nil
	}
	for _, msg := range msgs {
		if msg.Role == schema.Tool && strings.Contains(msg.Content, "统计专题") {
			m.result = true
		}
	}
	return schema.AssistantMessage("已读全文", nil), nil
}
func (m *l30GuideModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, fmt.Errorf("not streaming")
}
func TestL30SelectedGuideFullTextAndActualSkillArgument(t *testing.T) {
	s, _, run, _ := cartoRuntimeFixture(t)
	u := schema.UserMessage("按选择的流程制图")
	u.Extra = map[string]any{"guide_id": "statistical-choropleth"}
	input, err := s.injectCartographicRules(context.Background(), []*schema.Message{u}, run.Scheme, run.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	body, e := s.cartoRuntime.options.Guides.ReadContext(context.Background(), "statistical-choropleth", s.cartoRuntime.options.Resources)
	if e != nil {
		t.Fatal(e)
	}
	joined := ""
	for _, m := range input {
		joined += m.Content
	}
	if !strings.Contains(joined, body) || strings.Contains(joined, "skill(name=") {
		t.Fatal("selected guide full text or correct skill contract missing")
	}
	middleware, e := einoskill.NewMiddleware(context.Background(), &einoskill.Config{Backend: s.runtimeGuideBackend()})
	if e != nil {
		t.Fatal(e)
	}
	m := &l30GuideModel{t: t}
	a, e := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{Name: "guide_reader", Description: "test", Model: m, Handlers: []adk.ChatModelAgentMiddleware{middleware}})
	if e != nil {
		t.Fatal(e)
	}
	iter := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: a}).Run(context.Background(), []*schema.Message{schema.UserMessage("read")})
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
	}
	if !m.schemaChecked || !m.result {
		t.Fatalf("schema=%v result=%v", m.schemaChecked, m.result)
	}
}

func TestL30ExplicitUserAndProjectPlanDoesNotAcquire(t *testing.T) {
	s, f, run, p := cartoRuntimeFixture(t)
	file := filepath.Join(t.TempDir(), "provided.geojson")
	os.WriteFile(file, []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"LineString","coordinates":[]},"properties":{}}]}`), 0600)
	user := schema.UserMessage("使用 `" + file + "` 和工程里的河流图层")
	if e := s.bindTaskReferences(context.Background(), run.SessionID, run.ProjectPath, Message{Data: map[string]any{"layers": []map[string]any{{"id": "existing-river", "name": "河流"}}}}, user); e != nil {
		t.Fatal(e)
	}
	calls := []string{}
	f.invoke = func(_ context.Context, name string, _ map[string]any) (any, error) {
		calls = append(calls, name)
		if name != "get_layers_info" {
			return nil, fmt.Errorf("unexpected work %s", name)
		}
		return map[string]any{"layers": []map[string]any{{"id": "existing-river", "name": "河流", "geometry_type": "LineString"}}}, nil
	}
	p.Data = []plan.Data{{ID: "provided", Name: "用户河流", From: "user", Path: file, Geometry: "line"}, {ID: "project", Name: "工程河流", From: "project", Layer: "existing-river", Geometry: "line"}}
	p.Steps[0].Uses = []string{"provided", "project"}
	prepared, e := s.prepareCartographicPlan(context.Background(), run, p, nil)
	if e != nil {
		t.Fatal(e)
	}
	for _, step := range prepared.Executable.Steps {
		if step.Kind == "acquire" {
			t.Fatal("existing data scheduled acquisition")
		}
	}
	if len(calls) != 1 || calls[0] != "get_layers_info" || prepared.Snapshot.Plan.Data[0].Path != file || prepared.Snapshot.Plan.Data[1].Layer != "existing-river" {
		t.Fatal(prepared.Snapshot, calls)
	}
}
