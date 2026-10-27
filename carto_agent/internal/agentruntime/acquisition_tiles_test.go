package agentruntime

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/discovery"
	bridgepkg "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/autocarto/carto_agent/internal/toolsession"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestFactoryTileSystemChannelKeepsCredentialsOutOfBaseToolArguments(t *testing.T) {
	calls := 0
	target := &fakeEnhancedTool{info: &schema.ToolInfo{Name: "data_load_tiles"}, runFn: func(ctx context.Context, arg *schema.ToolArgument, _ ...tool.Option) (*schema.ToolResult, error) {
		calls++
		var args map[string]any
		if err := json.Unmarshal([]byte(arg.Text), &args); err != nil {
			t.Fatal(err)
		}
		if len(args) != 2 || args["confirmed"] != true || args["__private_credentials"] != nil || strings.Contains(arg.Text, "fake-key") || toolsession.InstanceIDFromContext(ctx) != "fixture-instance" {
			t.Fatal("public BaseTool arguments carried authority/credentials incorrectly")
		}
		// The Factory capability survives BaseTool invocation. A disconnected
		// bound instance fails before reading a credential, not at authorization.
		provider := bridgepkg.NewProvider("fixture", discovery.NewInstanceRegistry())
		defer provider.Close()
		if _, err := provider.InvokeTool(ctx, "data_load_tiles", args); err == nil || !strings.Contains(err.Error(), "fixture-instance") {
			t.Fatalf("system capability did not reach final bridge: %v", err)
		}
		return &schema.ToolResult{Parts: []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: `{"success":true,"data":{"layer_id":"fixture-layer"}}`}}}, nil
	}}
	factory, _ := NewFactory(&FactoryConfig{DefaultChatModel: newFlowProbe(), BaseTools: []tool.BaseTool{target}})
	args := map[string]any{"definition_path": filepath.Join(t.TempDir(), "tiles.layer.json"), "confirmed": true}
	if _, err := factory.InvokeAcquisitionTool(context.Background(), "data_load_tiles", args); err == nil || calls != 0 {
		t.Fatal("unbound invocation reached BaseTool")
	}
	ctx := toolsession.WithContext(context.Background(), "fixture-session", "", "fixture-instance")
	if _, err := factory.InvokeAcquisitionTool(ctx, "data_load_tiles", args); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
	if _, err := factory.InvokeFlowTool(ctx, "data_load_tiles", args); err == nil || calls != 1 {
		t.Fatal("readonly flow tool channel widened")
	}
	args["__private_credentials"] = map[string]any{"tianditu": "fake-key"}
	if _, err := factory.InvokeAcquisitionTool(ctx, "data_load_tiles", args); err == nil || calls != 1 {
		t.Fatal("public key reached BaseTool")
	}
}
