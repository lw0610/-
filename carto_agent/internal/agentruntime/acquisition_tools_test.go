package agentruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestAcquisitionToolChannelAllowsOnlyDeterministicOperations(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"admin_boundary_extract", "archive_extract", "data_normalize", "data_derive", "data_load_vector", "data_load_raster"} {
		target := &flowInvokable{name: name, result: `{"success":true,"output":"fixture-output"}`}
		factory, _ := NewFactory(&FactoryConfig{DefaultChatModel: newFlowProbe(), BaseTools: []tool.BaseTool{target}})
		if _, err := factory.InvokeAcquisitionTool(ctx, name, map[string]any{"fixture": true}); err != nil || target.calls != 1 {
			t.Fatalf("%s: calls=%d err=%v", name, target.calls, err)
		}
		if _, err := factory.InvokeFlowTool(ctx, name, nil); err == nil || target.calls != 1 {
			t.Fatalf("acquisition widened readonly flow channel: %s", name)
		}
	}
	for _, name := range []string{"pyqgis_execute", "file_delete", "osm_get_features", "get_layers_info", "flow_start"} {
		target := &flowInvokable{name: name, result: `{"success":true}`}
		factory, _ := NewFactory(&FactoryConfig{DefaultChatModel: newFlowProbe(), BaseTools: []tool.BaseTool{target}})
		if _, err := factory.InvokeAcquisitionTool(ctx, name, nil); err == nil || target.calls != 0 {
			t.Fatalf("untrusted acquisition operation allowed: %s", name)
		}
	}
}

func TestAcquisitionGEESystemTemplateCannotAcceptArbitraryCode(t *testing.T) {
	root := t.TempDir()
	templates := filepath.Join(root, "tools/system/data-acquisition/assets/gee")
	if err := os.MkdirAll(filepath.Join(root, "skills/system"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(templates, 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"common.py": "# trusted-common\n", "dem.py": "# trusted-dem\n"} {
		if err := os.WriteFile(filepath.Join(templates, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AUTOCARTO_ASSETS_ROOT", root)
	calls := 0
	target := &fakeEnhancedTool{info: &schema.ToolInfo{Name: "gee_run_script"}, runFn: func(_ context.Context, args *schema.ToolArgument, _ ...tool.Option) (*schema.ToolResult, error) {
		calls++
		var payload map[string]any
		if err := json.Unmarshal([]byte(args.Text), &payload); err != nil {
			t.Fatal(err)
		}
		code, _ := payload["code"].(string)
		if len(payload) != 1 || !strings.Contains(code, "trusted-dem") || !strings.Contains(code, "trusted-common") || strings.Contains(code, "UNTRUSTED_CODE") {
			t.Fatalf("bad template execution payload: %+v", payload)
		}
		return &schema.ToolResult{Parts: []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: `{"success":true,"artifact_paths":[]}`}}}, nil
	}}
	factory, _ := NewFactory(&FactoryConfig{DefaultChatModel: newFlowProbe(), BaseTools: []tool.BaseTool{target}})
	ctx := context.Background()
	for _, args := range []map[string]any{{"code": "UNTRUSTED_CODE"}, {"script": "/tmp/custom.py"}, {"template": "../custom.py", "params": map[string]any{}}, {"template": "dem.py", "params": map[string]any{}, "code": "UNTRUSTED_CODE"}, {"template": "dem.py", "params": "not-object"}} {
		if _, err := factory.InvokeAcquisitionTool(ctx, "gee_run_script", args); err == nil {
			t.Fatal("arbitrary GEE code accepted", args)
		}
	}
	if calls != 0 {
		t.Fatal("invalid calls reached runtime", calls)
	}
	if _, err := factory.InvokeAcquisitionTool(ctx, "gee_run_script", map[string]any{"template": "dem.py", "params": map[string]any{"label": "UNTRUSTED_CODE\nprint('should be data only')"}}); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
}
