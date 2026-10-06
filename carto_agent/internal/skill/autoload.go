package skill

import (
	"context"
	"strings"

	"github.com/autocarto/carto_agent/internal/compute"
	"github.com/autocarto/carto_agent/internal/shared/toolutils"
)

func AutoLoadOutputLayersViaQGIS(invoker QGISInvoker) AutoLoadFunc {
	if invoker == nil {
		return nil
	}
	return func(ctx context.Context, args map[string]any, layers []compute.OutputLayer) {
		for _, layer := range layers {
			if !layer.AutoLoad || strings.TrimSpace(layer.Path) == "" {
				continue
			}
			params := map[string]any{"path": strings.TrimSpace(layer.Path)}
			if args != nil {
				if targetInstanceID, ok := args["target_instance_id"].(string); ok && strings.TrimSpace(targetInstanceID) != "" {
					params["instance_id"] = strings.TrimSpace(targetInstanceID)
				} else if instanceID, ok := args["instance_id"].(string); ok && strings.TrimSpace(instanceID) != "" {
					params["instance_id"] = strings.TrimSpace(instanceID)
				}
			}
			toolName := "data_load_vector"
			if toolutils.IsRasterPath(layer.Path) {
				toolName = "data_load_raster"
			}
			_, _ = invoker(ctx, toolName, params)
		}
	}
}
