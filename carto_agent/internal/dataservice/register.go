package dataservice

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func RequiresRegistration(name string) bool {
	return strings.HasPrefix(name, "osm_get_") || strings.HasPrefix(name, "browser_") || name == "web_download_file" || name == "file_downloader" || name == "gee_run_script"
}
func IsRegistered(sessionDir, path string) bool {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(sessionDir, "output", "data-sources.json"))
	if err != nil {
		return false
	}
	var rows []GetRow
	if json.Unmarshal(raw, &rows) != nil {
		return false
	}
	for _, row := range rows {
		for _, known := range row.Paths {
			value, err := filepath.EvalSymlinks(known)
			if err == nil && value == canonical {
				return true
			}
		}
	}
	return false
}

func (s *Service) Register(ctx context.Context, request RegisterRequest) (GetResult, error) {
	if need, ok := AcquisitionNeedFromContext(ctx); ok {
		return s.registerNeed(ctx, need, request)
	}
	_, bound := AcquisitionSelection(ctx)
	if !bound && request.FindID == "" && request.Need != nil {
		need := *request.Need
		if strings.TrimSpace(need.Name) == "" {
			return GetResult{}, failure("DATA_NEED_UNRECOGNIZED", "直接登记需要 need.name，说明这份文件用于什么数据需求")
		}
		category, params, err := s.resolveNeed(FindRequest{Need: need})
		if err != nil || category == nil {
			diagnostic := s.needDiagnostic(category, err)
			return GetResult{}, failure(diagnostic.Code, diagnostic.Message+"；请更正 need.what 或 need.params，不要编造 find_id")
		}
		if geometry := NormalizeGeometry(need.Geometry); geometry != "" && geometry != NormalizeGeometry(category.Geometry) {
			return GetResult{}, failure("DATA_GEOMETRY_MISMATCH", "需求几何与类别不符："+need.Geometry+" / "+category.Geometry)
		}
		if strings.TrimSpace(need.Extent) == "" {
			return GetResult{}, failure("DATA_EXTENT_UNRESOLVED", "直接登记需要 need.extent；填写实际研究区边界文件、图层编号或 bbox:南,西,北,东")
		}
		extent, err := s.extent(ctx, need.Extent, nil)
		if err != nil {
			return GetResult{}, failure("DATA_EXTENT_UNRESOLVED", "need.extent 无法解析："+err.Error())
		}
		if extent.Pending {
			return GetResult{}, failure("DATA_EXTENT_UNRESOLVED", "need.extent 尚未确定，请提供实际研究区边界文件、图层编号或 bbox:南,西,北,东")
		}
		scaleText := need.Scale
		if scaleText == "auto" {
			scaleText = "自动"
		}
		scale, err := flow.ParseScale(scaleText)
		if err != nil {
			return GetResult{}, failure("DATA_NEED_UNRECOGNIZED", "need.scale 是地图比例尺，不是像元分辨率："+err.Error())
		}
		item := datasource.AcquisitionItem{Key: need.Name, Entry: need.Name, Requirement: need.Name, Category: category.ID, Geometry: category.Geometry, Params: params, Fields: need.Fields, Extent: extent, ExtentExpression: need.Extent, Scale: scale, Method: request.Method, Required: true}
		return s.registerNeed(ctx, AcquisitionNeed{ID: "direct/" + digest(need), Need: need, Item: item}, request)
	}
	missing := []string{}
	for name, value := range map[string]string{"find_id": request.FindID, "row": request.Row, "kind": request.Kind} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return GetResult{}, failure("DATA_REGISTRATION_REQUIRED", "按查询登记缺少 "+strings.Join(missing, "、")+"；沿用 data_get 返回的 preparation。Build 中直接取得的文件可改为填写 need、path 和来源信息，无需重下文件或猜查询编号")
	}
	result := GetResult{FindID: request.FindID, Status: "failed", Rows: []GetRow{}}
	q, err := s.acquisitionQuery(ctx, request.FindID)
	if err != nil {
		return result, err
	}
	item, exists := q.Items[request.Row]
	if !exists {
		return result, failure("DATA_CHOICE_INVALID", "登记数据行不属于此次查询")
	}
	if s.options.Config.NetworkPolicy == "deny" || s.options.Config.FallbackPolicy == "deny" {
		return result, failure("DATA_NETWORK_NOT_ALLOWED", "设置禁止兜底获取")
	}
	if s.options.Config.FallbackPolicy == "ask" && !s.fallbackApproved(request.FindID) {
		return result, failure("DATA_NETWORK_NOT_ALLOWED", "兜底来源需要用户先确认")
	}
	allowed := false
	for _, row := range q.Result.Rows {
		if row.Row == request.Row && row.Recommended != "" {
			return result, failure("DATA_CHOICE_INVALID", "此行有可用确定性来源，不使用兜底登记")
		}
	}
	for _, c := range q.Result.Fallback {
		if c.Kind == request.Kind && c.Available {
			allowed = true
		}
	}
	if !allowed {
		return result, failure("DATA_CHOICE_INVALID", "此次查询没有该兜底方式")
	}
	u, err := url.Parse(request.URL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return result, failure("DATA_REGISTER_MISMATCH", "兜底数据必须提供真实来源网址")
	}
	if strings.TrimSpace(request.License) == "" {
		return result, failure("DATA_REGISTER_MISMATCH", "兜底数据缺少许可说明")
	}
	canonical, err := filepath.EvalSymlinks(request.Path)
	if err != nil {
		return result, err
	}
	root, err := filepath.EvalSymlinks(s.options.SessionDir)
	if err != nil {
		return result, err
	}
	rel, err := filepath.Rel(root, canonical)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return result, failure("DATA_REGISTER_MISMATCH", "兜底文件必须位于当前会话目录")
	}
	if info, err := os.Stat(canonical); err != nil || !info.Mode().IsRegular() {
		return result, failure("DATA_REGISTER_MISMATCH", "兜底文件不是可读普通文件")
	}
	if s.options.Engine == nil || s.options.Engine.Store == nil {
		return result, fmt.Errorf("数据获取执行器不可用")
	}
	if _, err = s.options.Engine.ValidateProvided(ctx, item, canonical); err != nil {
		return result, failure("DATA_REGISTER_MISMATCH", err.Error())
	}
	sha, err := fileSHA(canonical)
	if err != nil {
		return result, err
	}
	sourceID := "fallback." + digest([]string{request.Kind, acquire.RedactURL(request.URL), sha})[:24]
	source := datasource.Source{ID: sourceID, APIVersion: "data_source/v1", Name: "经核验的兜底数据", Kind: "local_folder", Root: filepath.Dir(canonical), License: request.License, Attribution: request.Attribution, RefreshDays: 30, Notes: append([]string{"来源方式：" + request.Kind, "来源网址：" + acquire.RedactURL(request.URL)}, request.Notes...), Version: sha}
	provide := datasource.Provide{Category: item.Category, Path: filepath.Base(canonical), Fields: map[string]datasource.Strings{}}
	for field := range item.Fields {
		provide.Fields[field] = datasource.Strings{field}
	}
	source.Provides = []datasource.Provide{provide}
	catalog := *s.options.Engine.Catalog
	catalog.Sources = append(append([]datasource.Source{}, catalog.Sources...), source)
	engine := *s.options.Engine
	engine.Catalog = &catalog
	engine.Adapters = map[string]acquire.Adapter{"local_folder": acquire.LocalAdapter{}}
	item.Mode = "auto"
	item.Chosen = &datasource.SourceChoice{SourceID: sourceID, Name: source.Name, Provide: provide, Tier: 2, License: source.License, Attribution: source.Attribution}
	item.Alternatives = nil
	got, err := engine.RunItem(ctx, item, nil)
	if err != nil {
		return result, failure("DATA_REGISTER_MISMATCH", err.Error())
	}
	row := GetRow{Row: request.Row, Category: item.Category, Paths: got.Output.Paths, Kind: request.Kind, SourceID: sourceID, RetrievedAt: s.options.Now().UTC(), SHA256: map[string]string{}, License: source.License, Attribution: source.Attribution, Notes: source.Notes, Warnings: got.Notes, StoreKey: got.StoreKey, Count: got.Output.Count}
	for _, path := range row.Paths {
		sha, err := fileSHA(path)
		if err != nil {
			return result, err
		}
		row.SHA256[path] = sha
	}
	if err = s.recordRow(&result, row); err != nil {
		return result, err
	}
	row.FindID = request.FindID
	if err = atomicJSON(s.registrationPath(request.FindID, request.Row), row); err != nil {
		return result, err
	}
	result.Status = "completed"
	return result, nil
}
