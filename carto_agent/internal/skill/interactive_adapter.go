package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"

	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/toolresult"
)

type InteractiveToolAdapter struct {
	name        string
	description string
	inputSchema *jsonschema.Schema
	invoker     QGISInvoker
	argsMutator ArgsMutator
}

func NewInteractiveToolAdapter(item middlewareclient.ToolCatalogItem, invoker QGISInvoker, mutator ArgsMutator) einotool.BaseTool {
	adapter := &InteractiveToolAdapter{
		name:        strings.TrimSpace(item.Name),
		description: strings.TrimSpace(item.Description),
		inputSchema: toJSONSchema(item.InputSchema),
		invoker:     invoker,
		argsMutator: mutator,
	}
	info, _ := adapter.Info(context.Background())
	return toolutils.NewEnhancedTool(info, func(ctx context.Context, args map[string]interface{}) (*schema.ToolResult, error) {
		return adapter.runToolResult(ctx, args)
	})
}

func (t *InteractiveToolAdapter) Info(ctx context.Context) (*schema.ToolInfo, error) {
	_ = ctx
	return &schema.ToolInfo{
		Name:        t.name,
		Desc:        t.description,
		ParamsOneOf: schema.NewParamsOneOfByJSONSchema(t.inputSchema),
	}, nil
}

func (t *InteractiveToolAdapter) runToolResult(ctx context.Context, args map[string]interface{}) (*schema.ToolResult, error) {
	if t == nil || t.invoker == nil {
		return nil, fmt.Errorf("interactive tool invoker is nil")
	}
	cloned := cloneArgs(args)
	if t.argsMutator != nil {
		t.argsMutator(ctx, cloned)
	}
	result, err := t.invoker(ctx, t.name, cloned)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return &schema.ToolResult{}, nil
	}
	output, err := toolresult.JSON(result)
	if err != nil {
		return toolresult.Text(fmt.Sprintf("%v", result)), nil
	}
	return output, nil
	}

func cloneArgs(args map[string]interface{}) map[string]interface{} {
	if len(args) == 0 {
		return map[string]interface{}{}
	}
	raw, err := json.Marshal(args)
	if err != nil {
		out := make(map[string]interface{}, len(args))
		for key, value := range args {
			out[key] = value
		}
		return out
	}
	out := make(map[string]interface{})
	if err := json.Unmarshal(raw, &out); err != nil {
		out = make(map[string]interface{}, len(args))
		for key, value := range args {
			out[key] = value
		}
	}
	return out
}

func toJSONSchema(rawSchema map[string]any) *jsonschema.Schema {
	raw, err := json.Marshal(rawSchema)
	if err != nil {
		return &jsonschema.Schema{Type: "object"}
	}
	s := &jsonschema.Schema{}
	if err := json.Unmarshal(raw, s); err != nil {
		return &jsonschema.Schema{Type: "object"}
	}
	if strings.TrimSpace(s.Type) == "" {
		s.Type = "object"
	}
	return s
}
