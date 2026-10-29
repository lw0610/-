package deepagent

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/toolresult"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

// Exercise Eino's actual enhanced tool dispatch through the system expert.
// A direct test of the media envelope would miss a guard flattening the result.
type acquisitionMediaModel struct {
	tools map[string]bool
	seen  map[string]string
}

func (m *acquisitionMediaModel) WithTools(infos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	for _, info := range infos {
		m.tools[info.Name] = true
	}
	return m, nil
}

func (m *acquisitionMediaModel) Generate(_ context.Context, messages []*schema.Message, options ...model.Option) (*schema.Message, error) {
	for _, info := range model.GetCommonOptions(nil, options...).Tools {
		m.tools[info.Name] = true
	}
	for _, message := range messages {
		if message.Role != schema.Tool || message.ToolName != "browser_call_tool" {
			continue
		}
		for _, part := range message.UserInputMultiContent {
			if part.Image != nil && part.Image.Base64Data != nil {
				m.seen[part.Image.MIMEType] = *part.Image.Base64Data
			}
			if part.File != nil && part.File.Base64Data != nil {
				m.seen[part.File.MIMEType] = *part.File.Base64Data
			}
		}
		return schema.AssistantMessage("原始媒体已收到", nil), nil
	}
	return schema.AssistantMessage("", []schema.ToolCall{{ID: "source-media", Type: "function", Function: schema.FunctionCall{Name: "browser_call_tool", Arguments: `{"tool_name":"browser_take_screenshot","args":{}}`}}}), nil
}

func (m *acquisitionMediaModel) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	result, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{result}), nil
}

func TestSystemDataExpertPreservesEnhancedMediaThroughGuardAndADK(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	png, pdf := []byte("\x89PNG\r\n\x1a\nfixture"), []byte("%PDF-1.7\nfixture")
	calls := 0
	mediaTool := utils.NewEnhancedTool(&schema.ToolInfo{Name: "browser_call_tool", Desc: "fixture browser"}, func(context.Context, map[string]any) (*schema.ToolResult, error) {
		calls++
		result := toolresult.AppendImageData(toolresult.Text("original source"), png, "image/png")
		return toolresult.AppendFileData(result, pdf, "application/pdf", nil), nil
	})
	tools := []tool.BaseTool{mediaTool}
	for _, name := range []string{"data_find", "data_get", "file_read", "file_write", "shell_execute"} {
		tools = append(tools, utils.NewTool(&schema.ToolInfo{Name: name, Desc: name}, func(context.Context, map[string]any) (string, error) {
			return "", fmt.Errorf("unexpected fixture tool execution")
		}))
	}
	capture := &acquisitionMediaModel{tools: map[string]bool{}, seen: map[string]string{}}
	ctx := dataservice.WithService(context.Background(), dataservice.New(dataservice.Options{
		SessionDir: t.TempDir(), Config: config.AcquisitionConfig{NetworkPolicy: "allow", FallbackPolicy: "allow"},
	}))
	ctx = dataservice.WithAcquisitionSelection(ctx, dataservice.Selection{
		FindID: "df-fixture", Rows: []dataservice.SelectedRow{{Row: "population", Route: dataservice.RouteExplore}},
		Fallback: []dataservice.Candidate{{Kind: "portal", Available: true}},
	})
	agents, err := createSystemExperts(ctx, &Config{ChatModel: capture, Tools: tools, MaxIteration: 3})
	if err != nil || len(agents) != 1 {
		t.Fatalf("system expert: %v %v", agents, err)
	}
	iter := agents[0].Run(ctx, &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage("读取真实来源")}})
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
	}
	if calls != 1 {
		t.Fatalf("browser invoked %d times", calls)
	}
	for mime, bytes := range map[string][]byte{"image/png": png, "application/pdf": pdf} {
		if capture.seen[mime] != base64.StdEncoding.EncodeToString(bytes) {
			t.Fatalf("%s lost before model: %v", mime, capture.seen)
		}
	}
	for _, name := range []string{"file_read", "file_write", "shell_execute"} {
		if !capture.tools[name] {
			t.Errorf("expert lacks actual tool %s", name)
		}
	}
	// Keeping enhanced results must not remove the query authorization boundary.
	guard := wrapAcquisitionGuard(&acquisitionGuard{BaseTool: mediaTool, name: "browser_call_tool"}).(tool.EnhancedInvokableTool)
	if _, err := guard.InvokableRun(context.Background(), &schema.ToolArgument{Text: `{}`}); err == nil || calls != 1 {
		t.Fatal("unscoped browser call bypassed the guard")
	}
}
