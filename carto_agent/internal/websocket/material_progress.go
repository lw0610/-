package websocket

import (
	"context"

	"github.com/autocarto/carto_agent/internal/taskmaterials"
	"github.com/gorilla/websocket"
)

func (s *DeepServer) withMaterialProgress(ctx context.Context, conn *websocket.Conn, sid, phase string) context.Context {
	return taskmaterials.WithProgress(ctx, func(p taskmaterials.Progress) {
		if ctx.Err() != nil {
			return
		}
		s.sendAgentWorkflowPhase(conn, sid, "progress", p.Message, withModePhaseData(map[string]any{
			"source": "materials", "stage_id": p.ID, "material_name": p.Name, "material_status": p.Status,
		}, phase))
	})
}
