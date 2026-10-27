package acquire

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"
)

type OfflineAdapter struct{ Invoke ToolCaller }

func (OfflineAdapter) Kind() string { return "offline_asset" }
func (a OfflineAdapter) Fetch(ctx context.Context, r FetchRequest) (FetchResult, error) {
	if a.Invoke == nil || r.Provide == nil || r.Provide.Tool != "admin_boundary_extract" {
		return FetchResult{}, fmt.Errorf("内置离线来源没有可用的提取工具")
	}
	level := r.Item.Params["level"]
	if level == "" {
		level = "省"
	}
	within := r.Item.Params["within"]
	if within == "" && r.Item.Extent != nil {
		within = r.Item.Extent.Region
	}
	if within == "" {
		within = "全国"
	}
	args := map[string]any{"level": level, "within": within, "load": false, "output": filepath.Join(r.WorkDir, "boundary.geojson")}
	value, err := a.Invoke(ctx, r.Provide.Tool, args)
	if err != nil {
		return FetchResult{}, err
	}
	payload, err := UnwrapToolResult(value)
	if err != nil {
		return FetchResult{}, err
	}
	path, _ := payload["output"].(string)
	if path == "" {
		return FetchResult{}, fmt.Errorf("行政区提取没有返回文件路径")
	}
	digest, size, err := fileDigest(path)
	if err != nil {
		return FetchResult{}, err
	}
	query, _ := json.Marshal(map[string]string{"level": level, "within": within})
	return FetchResult{Files: []string{path}, Format: "geojson", Query: string(query), RetrievedAt: time.Now().UTC(), Bytes: size, SHA256: map[string]string{path: digest}, Notes: []string{"内置行政区原始数据；来源声明为天地图，完整疆域表达未核实"}}, nil
}
