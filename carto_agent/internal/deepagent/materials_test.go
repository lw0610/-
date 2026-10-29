package deepagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
	"github.com/autocarto/carto_agent/internal/taskmaterials"
	"github.com/autocarto/carto_agent/internal/toolsession"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

type materialsProbeState struct {
	sync.Mutex
	path, text, delegate  string
	rootCalls, childCalls int
}

type materialsProbeModel struct {
	state *materialsProbeState
	tools []*schema.ToolInfo
}

func (m *materialsProbeModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return &materialsProbeModel{state: m.state, tools: tools}, nil
}

func (m *materialsProbeModel) Generate(_ context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.state.Lock()
	defer m.state.Unlock()
	available := map[string]bool{}
	for _, info := range model.GetCommonOptions(&model.Options{Tools: m.tools}, opts...).Tools {
		available[info.Name] = true
	}
	if !available["file_read"] || available["file_write"] || available["shell_execute"] {
		return nil, fmt.Errorf("attachment read baseline changed other permissions: %v", available)
	}
	count, materialAt, lastUser := 0, -1, -1
	for index, message := range input {
		if message.Role == schema.User {
			lastUser = index
		}
		if message.Extra[taskmaterials.MessageMarker] != true {
			continue
		}
		count++
		materialAt = index
		if message.Role != schema.User || !strings.Contains(message.Content, m.state.path) || !strings.Contains(message.Content, m.state.text) {
			return nil, fmt.Errorf("agent lost registered attachment or cached source: %s", message.Content)
		}
	}
	if count != 1 || materialAt >= lastUser {
		return nil, fmt.Errorf("material must occur once before the actual task: count=%d material=%d task=%d", count, materialAt, lastUser)
	}
	if available["task"] && m.state.delegate != "" {
		m.state.rootCalls++
		if m.state.rootCalls == 1 {
			// A child must reuse this run's source snapshot, rather than perform
			// another read or depend on the root copying text into the task.
			if err := os.WriteFile(m.state.path, []byte("changed after root preparation"), 0o600); err != nil {
				return nil, err
			}
			return schema.AssistantMessage("", []schema.ToolCall{{ID: "material-delegation", Type: "function", Function: schema.FunctionCall{
				Name: "task", Arguments: fmt.Sprintf(`{"subagent_type":%q,"description":"处理用户提供的材料"}`, m.state.delegate),
			}}}), nil
		}
		if m.state.childCalls != 1 {
			return nil, fmt.Errorf("child did not complete with inherited attachment")
		}
		return schema.AssistantMessage("completed", nil), nil
	}
	m.state.childCalls++
	return schema.AssistantMessage("attachment received", nil), nil
}

func (m *materialsProbeModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func materialFixture(t *testing.T) (context.Context, *Config, *materialsProbeState) {
	t.Helper()
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	project := filepath.Join(t.TempDir(), "material-test.qgz")
	sessionID := "materials-test"
	path := filepath.Join(t.TempDir(), "user-source.txt")
	text := "用户附件原文：第一站甲地，第二站乙地。"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := sessionfiles.New(project, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.RegisterProvided(path, sessionfiles.Origin{Type: "provided"}); err != nil {
		t.Fatal(err)
	}
	if err = dataservice.SaveBindings(store.Dir, []dataservice.Binding{{Explicit: true, Current: true, Name: "source", Path: path}}); err != nil {
		t.Fatal(err)
	}
	state := &materialsProbeState{path: path, text: text}
	cfg := &Config{Name: "orchestrator", Description: "test root", Instruction: "分配并处理用户任务", ChatModel: &materialsProbeModel{state: state}, MaxIteration: 4,
		SkillRuntimeConfig: &subagents.RuntimeSkillConfig{Agents: map[string]subagents.RuntimeSkillAgentConfig{
			"orchestrator": {IsMain: true, Enabled: true, Kind: "agent"},
			"reader":       {Enabled: true, Kind: "agent", DisplayName: "材料阅读", Description: "处理给定材料"},
		}},
	}
	for _, name := range []string{"file_read", "file_write", "shell_execute"} {
		cfg.Tools = append(cfg.Tools, utils.NewEnhancedTool(&schema.ToolInfo{Name: name, Desc: name}, func(context.Context, map[string]any) (*schema.ToolResult, error) {
			return nil, fmt.Errorf("small text should already be available; no fixture tool call expected")
		}))
	}
	return toolsession.WithContext(context.Background(), sessionID, project, ""), cfg, state
}

func runMaterialAgent(t *testing.T, ctx context.Context, agent adk.Agent) {
	t.Helper()
	messages := []adk.Message{schema.UserMessage("处理这个附件")}
	iter := agent.Run(ctx, &adk.AgentInput{Messages: messages})
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
	}
	if len(messages) != 1 || messages[0].Content != "处理这个附件" || messages[0].Extra[taskmaterials.MessageMarker] != nil {
		t.Fatal("attachment injection mutated caller conversation history")
	}
}

func TestRootDelegationSharesAttachmentsWithoutCopyingSourceIntoTask(t *testing.T) {
	ctx, cfg, state := materialFixture(t)
	state.delegate = "reader"
	agent, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	runMaterialAgent(t, ctx, agent)
	if state.rootCalls != 2 || state.childCalls != 1 {
		t.Fatalf("unexpected agent path root=%d child=%d", state.rootCalls, state.childCalls)
	}
}

func TestStandaloneConfiguredRolesReceiveMaterials(t *testing.T) {
	for _, kind := range []string{"flow_role", "reader"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cfg, state := materialFixture(t)
			var agent adk.Agent
			var err error
			if kind == "flow_role" {
				role := cfg.SkillRuntimeConfig.Agents["reader"]
				role.FlowOnly = true
				cfg.SkillRuntimeConfig.Agents["reader"] = role
				agent, err = BuildRoleAgent(ctx, cfg, "reader")
			} else {
				// A plain configured reader keeps attachment access without receiving
				// write or shell tools from the catalog.
				agent, err = BuildRoleAgent(ctx, cfg, "reader")
			}
			if err != nil {
				t.Fatal(err)
			}
			runMaterialAgent(t, ctx, agent)
			if state.childCalls != 1 {
				t.Fatalf("role model was not invoked once: %d", state.childCalls)
			}
		})
	}
}
