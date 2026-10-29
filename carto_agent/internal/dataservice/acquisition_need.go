package dataservice

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/layers"
	"github.com/autocarto/carto_agent/internal/stats"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type AcquisitionNeed struct {
	ID            string                     `json:"id"`
	Goal          string                     `json:"goal"`
	Need          Need                       `json:"need"`
	Item          datasource.AcquisitionItem `json:"item"`
	Statistics    *stats.Need                `json:"statistics,omitempty"`
	ExpectedUnits []stats.Unit               `json:"expected_units,omitempty"`
	Acceptance    []string                   `json:"acceptance"`
	Handbook      string                     `json:"handbook"`
	Ledger        []map[string]any           `json:"ledger"`
}
type acquisitionNeedKey struct{}

func WithAcquisitionNeed(ctx context.Context, need AcquisitionNeed) context.Context {
	return context.WithValue(ctx, acquisitionNeedKey{}, need)
}
func AcquisitionNeedFromContext(ctx context.Context) (AcquisitionNeed, bool) {
	value, ok := ctx.Value(acquisitionNeedKey{}).(AcquisitionNeed)
	return value, ok
}
func (s *Service) needReceiptPath(id string) string {
	return filepath.Join(s.options.SessionDir, "data", "preparations", digest(id)+".json")
}
func (s *Service) RegisteredNeed(ctx context.Context, id string) (GetResult, error) {
	raw, err := os.ReadFile(s.needReceiptPath(id))
	if os.IsNotExist(err) {
		return GetResult{}, failure("DATA_DISCOVERY_INCOMPLETE", "尚未提交通过内容检查的数据："+id)
	}
	if err != nil {
		return GetResult{}, err
	}
	var result GetResult
	if err = json.Unmarshal(raw, &result); err != nil {
		return result, err
	}
	// A new filesystem boundary: detect a deleted or edited registered result.
	for _, row := range result.Rows {
		for _, path := range row.Paths {
			sha, err := fileSHA(path)
			if err != nil || sha != row.SHA256[path] {
				return result, failure("DATA_REGISTER_MISMATCH", "已登记文件不存在或内容已改变")
			}
		}
	}
	return result, ctx.Err()
}
func statisticsStore() (*stats.Store, error) {
	home, err := config.ResolveAutoCartoHome()
	if err != nil {
		return nil, err
	}
	return stats.New(filepath.Join(home, "data", "stats")), nil
}
func expectedStatistics(need AcquisitionNeed) ([]stats.Unit, error) {
	if len(need.ExpectedUnits) > 0 {
		return need.ExpectedUnits, nil
	}
	if need.Statistics == nil {
		return nil, nil
	}
	assets, err := config.ResolveAssetsRoot()
	if err != nil {
		return nil, err
	}
	region := need.Statistics.RegionCode
	if region == "" {
		region = need.Statistics.Region
	}
	units, err := layers.AdministrativeUnits(assets, region, need.Statistics.Level)
	if err != nil {
		return nil, err
	}
	result := []stats.Unit{}
	for _, unit := range units {
		result = append(result, stats.Unit{Code: unit.Code, Name: unit.Name, Level: unit.Level})
	}
	return result, nil
}
func (s *Service) LookupStatistics(ctx context.Context, need AcquisitionNeed) (GetResult, bool, error) {
	if need.Statistics == nil {
		return GetResult{}, false, nil
	}
	store, err := statisticsStore()
	if err != nil {
		return GetResult{}, false, err
	}
	data, found, err := store.Lookup(ctx, *need.Statistics)
	if err != nil || !found {
		return GetResult{}, found, err
	}
	result, err := s.recordStatistics(need, data, "统计库复用")
	return result, err == nil, err
}
func (s *Service) recordStatistics(need AcquisitionNeed, data stats.Dataset, note string) (GetResult, error) {
	sha, err := fileSHA(data.Path)
	if err != nil {
		return GetResult{}, err
	}
	result := GetResult{Status: "completed", Rows: []GetRow{}}
	row := GetRow{Row: need.Need.Name, Category: need.Item.Category, Paths: []string{data.Path}, Kind: "stats_store", SourceID: data.Source.URL, RetrievedAt: data.Source.RetrievedAt, SHA256: map[string]string{data.Path: sha}, Count: len(data.Observations), Notes: []string{note, data.Source.Title, data.Source.TableRef}, Warnings: []string{}}
	if err = s.recordRow(&result, row); err != nil {
		return result, err
	}
	return result, atomicJSON(s.needReceiptPath(need.ID), result)
}
func (s *Service) registerNeed(ctx context.Context, need AcquisitionNeed, request RegisterRequest) (GetResult, error) {
	if request.NeedID != "" && request.NeedID != need.ID {
		return GetResult{}, failure("DATA_REGISTER_MISMATCH", "提交需求编号与当前取数任务不一致")
	}
	path, err := filepath.EvalSymlinks(request.Path)
	if err != nil {
		return GetResult{}, err
	}
	root, err := filepath.EvalSymlinks(s.options.SessionDir)
	if err != nil {
		return GetResult{}, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return GetResult{}, failure("DATA_REGISTER_MISMATCH", "取数成果必须位于当前会话目录")
	}
	method := request.Method
	if method == "" {
		method = request.Kind
	}
	parsedURL, parseErr := url.Parse(request.URL)
	if parseErr != nil || parsedURL.Host == "" || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") || parsedURL.User != nil {
		return GetResult{}, failure("DATA_REGISTER_MISMATCH", "原始来源需要不含凭据的HTTP(S)网址")
	}
	if request.URL == "" || request.SourceTitle == "" || method == "" {
		return GetResult{}, failure("DATA_REGISTER_MISMATCH", "提交成果需要原始网址、来源标题和读取方式")
	}
	if need.Statistics != nil {
		return s.ValidateStatistics(ctx, need, path, stats.Source{Title: request.SourceTitle, URL: request.URL, TableRef: request.TableRef, Method: method, RetrievedAt: s.options.Now().UTC(), OriginalPath: request.OriginalPath, Official: strings.HasSuffix(strings.ToLower(parsedURL.Hostname()), ".gov.cn")})
	}
	if s.options.Engine == nil {
		return GetResult{}, fmt.Errorf("数据获取执行器不可用")
	}
	sha, err := fileSHA(path)
	if err != nil {
		return GetResult{}, err
	}
	sourceID := "acquired." + digest([]string{method, acquire.RedactURL(request.URL)})[:24]
	provide := datasource.Provide{Category: need.Item.Category, Path: filepath.Base(path), Fields: map[string]datasource.Strings{}}
	for field := range need.Item.Fields {
		provide.Fields[field] = datasource.Strings{field}
	}
	source := datasource.Source{ID: sourceID, Kind: "local_folder", Root: filepath.Dir(path), Name: request.SourceTitle, Version: sha, Method: method, License: request.License, Attribution: request.Attribution, RefreshDays: 30, Provides: []datasource.Provide{provide}, Notes: append([]string{"来源方式：" + method, "来源网址：" + acquire.RedactURL(request.URL), "来源表/页：" + request.TableRef}, request.Notes...)}
	engine := *s.options.Engine
	catalog := *engine.Catalog
	catalog.Sources = append(append([]datasource.Source{}, catalog.Sources...), source)
	engine.Catalog = &catalog
	engine.Adapters = map[string]acquire.Adapter{"local_folder": acquire.LocalAdapter{}}
	item := need.Item
	item.Mode = "auto"
	item.Alternatives = nil
	item.Chosen = &datasource.SourceChoice{SourceID: sourceID, Name: source.Name, Provide: provide}
	got, err := engine.RunItem(ctx, item, nil)
	if err != nil {
		return GetResult{}, failure("DATA_CONTENT_INVALID", err.Error())
	}
	row := GetRow{Row: need.Need.Name, Category: item.Category, Paths: got.Output.Paths, Kind: method, SourceID: sourceID, StoreKey: got.StoreKey, RetrievedAt: s.options.Now().UTC(), SHA256: map[string]string{}, License: request.License, Attribution: request.Attribution, Count: got.Output.Count, Notes: source.Notes, Warnings: got.Notes}
	for _, p := range row.Paths {
		sum, err := fileSHA(p)
		if err != nil {
			return GetResult{}, err
		}
		row.SHA256[p] = sum
	}

	result := GetResult{Status: "completed", Rows: []GetRow{}}
	if err = s.recordRow(&result, row); err != nil {
		return result, err
	}
	return result, atomicJSON(s.needReceiptPath(need.ID), result)
}

// ValidateStatistics imports an executor-authorized file through the same content
// checks as an acquired table. User files remain unchanged; the verified copy is reusable.
func (s *Service) ValidateStatistics(ctx context.Context, need AcquisitionNeed, path string, source stats.Source) (GetResult, error) {
	if need.Statistics == nil {
		return GetResult{}, fmt.Errorf("统计需求尚未定义")
	}
	if source.Method == "" {
		source.Method = "user"
	}
	if source.Title == "" {
		source.Title = filepath.Base(path)
	}
	expected, err := expectedStatistics(need)
	if err != nil {
		return GetResult{}, err
	}
	data, err := stats.CheckFile(path, *need.Statistics, expected, source)
	if err != nil {
		return GetResult{}, failure("DATA_CONTENT_INVALID", err.Error())
	}
	store, err := statisticsStore()
	if err != nil {
		return GetResult{}, err
	}
	data, err = store.Put(ctx, data)
	if err != nil {
		return GetResult{}, err
	}
	return s.recordStatistics(need, data, "统计表内容检查通过")
}

// PrepareAcquisitionNeed resolves statistical identities once before the handoff.
func (s *Service) PrepareAcquisitionNeed(need AcquisitionNeed) (AcquisitionNeed, error) {
	if need.Statistics == nil {
		return need, nil
	}
	copy := *need.Statistics
	need.Statistics = &copy
	if copy.RegionCode == "" {
		if s.options.Environment.Catalog == nil {
			return need, fmt.Errorf("统计地区目录不可用")
		}
		region, err := s.options.Environment.Catalog.ResolveRegion(copy.Region)
		if err != nil {
			return need, err
		}
		need.Statistics.RegionCode = region.Code
		if region.Name == "全国" {
			need.Statistics.RegionCode = "CN"
		}
	}
	expected, err := expectedStatistics(need)
	if err != nil {
		return need, err
	}
	need.ExpectedUnits = expected
	need.Acceptance = append(need.Acceptance, "期望行政单元齐全且唯一，年份、指标、口径和单位一致；有上级合计时检查取整误差")
	return need, nil
}
