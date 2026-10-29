package flowrun

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/flow"
)

type projectLayer struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Source string `json:"source"`
}

func (c *Controller) projectLayers(ctx context.Context) ([]projectLayer, error) {
	raw, err := c.options.Tools.Invoke(ctx, "get_layers_info", map[string]any{"include_invisible": true})
	if err != nil {
		return nil, err
	}
	value, err := acquire.UnwrapToolResult(raw)
	if err != nil {
		return nil, err
	}
	if info, ok := value["info"].(map[string]any); ok {
		value = info
	}
	bytes, err := json.Marshal(value["layers"])
	if err != nil {
		return nil, err
	}
	var layers []projectLayer
	if err = json.Unmarshal(bytes, &layers); err != nil {
		return nil, err
	}
	return layers, nil
}
func (c *Controller) cleanupAttemptLayers(ctx context.Context, attempt *Attempt, step flow.StepPlan, receipt *Receipt) error {
	if !attempt.LayersTracked || c.options.Tools == nil {
		return nil
	}
	current, err := c.projectLayers(ctx)
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, id := range attempt.BeforeLayers {
		keep[id] = true
	}
	if receipt != nil {
		for _, value := range receipt.Outputs {
			for _, v := range layerOutputValues(value) {
				switch ref := v.(type) {
				case string:
					keep[ref] = true
				case map[string]any:
					for _, key := range []string{"id", "layer_id", "name", "path", "source"} {
						if text, ok := ref[key].(string); ok {
							keep[text] = true
						}
					}
				}
			}
		}
	}
	remove := []string{}
	for _, layer := range current {
		if !keep[layer.ID] && !keep[layer.Name] && !keep[layer.Source] {
			remove = append(remove, layer.ID)
		}
	}
	if len(remove) == 0 {
		return nil
	}
	layouts := []string{}
	if receipt != nil {
		for _, o := range step.ExpectedOutputs {
			if o.Type == "layout" {
				if name, ok := receipt.Outputs[o.Label].(string); ok {
					layouts = append(layouts, name)
				}
			}
		}
	}
	result, err := c.options.Tools.Invoke(ctx, "layer_remove_created", map[string]any{"layer_ids": remove, "keep_layouts": layouts})
	if err != nil {
		return fmt.Errorf("清理本次尝试新增图层失败：%w", err)
	}
	return checkToolSuccess(result)
}

// The step's declared work, not every tool its role owns, determines whether a
// QGIS snapshot is needed. File and table work can proceed while QGIS is offline.
func stepUsesQGIS(step flow.StepPlan, capabilities []string) bool {
	if step.Executor == "route_knowledge" {
		return false
	}
	for _, capability := range capabilities {
		switch capability {
		case "data-loading", "project-state", "pyqgis-scripting", "print-layout", "cartography", "spatial-analysis", "symbology", "route-cartography", "vector-fields", "vector-geometry", "quality-feedback":
			return true
		}
	}
	for _, output := range step.ExpectedOutputs {
		if output.Type == "layers" || output.Type == "layer" || output.Type == "layout" {
			return true
		}
	}
	return false
}
