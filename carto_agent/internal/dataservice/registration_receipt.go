package dataservice

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/autocarto/carto_agent/internal/resource/bindings"
)

func (s *Service) registrationPath(id, row string) string {
	return filepath.Join(s.options.SessionDir, "data", "finds", id+".registered-"+digest(row)[:16]+".json")
}

// Only Register writes these receipts after validation. A model's reported path
// or another query's same-named file cannot complete this acquisition.
func (s *Service) registeredSelection(ctx context.Context, selection Selection) (GetResult, error) {
	result := GetResult{FindID: selection.FindID, Status: "running", Rows: []GetRow{}}
	if !findPattern.MatchString(selection.FindID) || len(selection.Rows) == 0 {
		return result, failure("DATA_CHOICE_INVALID", "已确认的查询选择无效")
	}
	for _, selected := range selection.Rows {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		path := s.registrationPath(selection.FindID, selected.Row)
		if err := bindings.RejectSymlinks(path); err != nil {
			return result, err
		}
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return result, failure("DATA_DISCOVERY_INCOMPLETE", "尚未登记本次查询的真实数据："+selected.Row)
		}
		if err != nil {
			return result, err
		}
		var row GetRow
		if json.Unmarshal(raw, &row) != nil || row.FindID != selection.FindID || row.Row != selected.Row || row.Category != selected.Item.Category || len(row.Paths) == 0 {
			return result, failure("DATA_REGISTER_MISMATCH", "数据登记回执与已确认查询不一致")
		}
		for _, p := range row.Paths {
			sha, err := fileSHA(p)
			if err != nil || row.SHA256[p] == "" || sha != row.SHA256[p] {
				return result, failure("DATA_REGISTER_MISMATCH", "已登记数据缺失或内容已改变："+selected.Row)
			}
		}
		result.Rows = append(result.Rows, row)
	}
	result.Status = "completed"
	result.ProvenancePath = filepath.Join(s.options.SessionDir, "output", "data-sources.json")
	return result, nil
}
