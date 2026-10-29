package dataservice

import (
	"context"

	"github.com/autocarto/carto_agent/internal/acquire"
)

// ResolveSelectionExtent completes only unresolved bounds using the same
// resolver as direct acquisition. The trusted executor persists the returned
// selection before handing it to an expert, so registration and resume use the
// same bounds rather than re-reading a changed upstream file.
func (s *Service) ResolveSelectionExtent(ctx context.Context, selection Selection, resolvedData map[string]string) (Selection, error) {
	selection.Rows = append([]SelectedRow{}, selection.Rows...)
	for i := range selection.Rows {
		if err := ctx.Err(); err != nil {
			return Selection{}, err
		}
		selected := selection.Rows[i]
		// Existing local data can be used without choosing a download region.
		// Its normal geometry/field checks still run in GetSelection.
		if selected.Route != RouteExplore && (selected.Candidate.Path != "" || selected.Candidate.LayerID != "") {
			continue
		}
		item := &selection.Rows[i].Item
		if item.Extent != nil && !item.Extent.Pending && len(item.Extent.BBox) == 4 {
			continue
		}
		extent, err := s.extent(ctx, item.ExtentExpression, resolvedData)
		if err != nil {
			return Selection{}, failure("DATA_EXTENT_UNRESOLVED", err.Error())
		}
		if extent == nil || extent.Pending || len(extent.BBox) != 4 {
			return Selection{}, failure("DATA_EXTENT_UNRESOLVED", "获取范围尚未具备可核验的上游数据："+selection.Rows[i].Row)
		}
		item.Extent = extent
		if item.Scale == 0 {
			item.Scale = acquire.EstimateScale(extent, nil)
		}
	}
	return selection, nil
}
