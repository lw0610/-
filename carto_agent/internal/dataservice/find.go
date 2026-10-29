package dataservice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var referenceExtent = regexp.MustCompile(`^「([^」]+)」(?:\s*外扩\s*([0-9]+(?:\.[0-9]+)?)\s*[%％])?$`)
var descriptionYear = regexp.MustCompile(`(19[0-9]{2}|20[0-9]{2})\s*年`)

func (s *Service) extent(ctx context.Context, expression string, data map[string]string) (*datasource.ExtentPlan, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return &datasource.ExtentPlan{Source: expression, Pending: true}, nil
	}
	if match := referenceExtent.FindStringSubmatch(expression); match != nil {
		value := data[match[1]]
		percent, _ := strconv.ParseFloat(match[2], 64)
		if value == "" {
			return &datasource.ExtentPlan{Source: match[1], Pending: true, ExpandPercent: percent}, nil
		}
		extent, err := acquire.ResolveExtent(ctx, value, s.options.Environment.Catalog, s.options.ResolveExtent)
		if err != nil {
			return nil, err
		}
		extent.Source = match[1]
		extent.ExpandPercent = percent
		return extent, acquire.PrepareExtent(extent)
	}
	if strings.HasPrefix(expression, "bbox:") {
		values := strings.Split(strings.TrimPrefix(expression, "bbox:"), ",")
		if len(values) != 4 {
			return nil, fmt.Errorf("bbox 需要南、西、北、东四个坐标")
		}
		coords := make([]float64, 4)
		for i, text := range values {
			v, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
			if err != nil {
				return nil, err
			}
			coords[i] = v
		}
		extent := &datasource.ExtentPlan{Source: expression, BBox: []float64{coords[1], coords[0], coords[3], coords[2]}}
		return extent, acquire.PrepareExtent(extent)
	}
	return acquire.ResolveExtent(ctx, strings.TrimPrefix(expression, "@"), s.options.Environment.Catalog, s.options.ResolveExtent)
}
func (s *Service) resolveNeed(request FindRequest) (*datasource.Category, map[string]string, error) {
	catalog := s.options.Environment.Catalog
	if catalog == nil {
		return nil, nil, fmt.Errorf("数据源目录不可用")
	}
	name := strings.TrimSpace(request.Need.What.Category)
	params := map[string]string{}
	for k, v := range request.Need.Params {
		params[k] = v
	}
	if name != "" {
		return catalog.ResolveAcquisitionCategory(name, params)
	}
	description := request.Need.What.Description
	if match := descriptionYear.FindStringSubmatch(description); match != nil && params["year"] == "" {
		params["year"] = match[1]
	}
	best := ""
	for _, c := range catalog.Categories {
		for _, alias := range append([]datasource.Alias{{Name: c.Name}, {Name: c.ID}}, c.Aliases...) {
			if strings.Contains(description, alias.Name) && len([]rune(alias.Name)) > len([]rune(best)) {
				best = alias.Name
			}
		}
	}
	if best == "" {
		return nil, params, fmt.Errorf("未识别数据类别")
	}
	return catalog.ResolveAcquisitionCategory(best, params)
}
func (s *Service) Find(ctx context.Context, request FindRequest) (result FindResult, err error) {
	now := s.options.Now().UTC()
	result = FindResult{CreatedAt: now, ExpiresAt: now.Add(queryTTL), Rows: []FindRow{}, ProvidedMatches: []Candidate{}, ProvidedUnmatched: []Exclusion{}, Fallback: []Candidate{}, Diagnostics: []Diagnostic{}}
	defer func() { describeFindState(&result) }()
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return result, err
	}
	result.FindID = "df-" + hex.EncodeToString(id[:])
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if strings.TrimSpace(request.Need.What.Category) == "" && strings.TrimSpace(request.Need.What.Description) == "" {
		return result, failure("DATA_NEED_UNRECOGNIZED", `need.what 至少填写 category 或 description 一项，允许同时填写，例如 {"category":"河流","description":"线状河流，需要河名"}`)
	}
	if request.Need.Name == "" {
		request.Need.Name = request.Need.What.Category
		if request.Need.Name == "" {
			request.Need.Name = request.Need.What.Description
		}
	}
	extent, extentErr := s.extent(ctx, request.Need.Extent, nil)
	if extentErr != nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "DATA_EXTENT_UNRESOLVED", Message: extentErr.Error()})
		extent = &datasource.ExtentPlan{Pending: true, Source: request.Need.Extent}
	}
	scaleText := request.Need.Scale
	if scaleText == "auto" {
		scaleText = "自动"
	}
	scale, scaleErr := flow.ParseScale(scaleText)
	if scaleErr != nil {
		return result, failure("DATA_NEED_UNRECOGNIZED", scaleErr.Error())
	}
	if scale == 0 {
		scale = acquire.EstimateScale(extent, nil)
	}
	result.Resolved = Resolved{Extent: extent, ScaleDenominator: scale}
	items := []datasource.AcquisitionItem{}
	{
		category, params, err := s.resolveNeed(request)
		if category != nil {
			result.Resolved.Category = category.ID
		}
		if category == nil || err != nil {
			result.Diagnostics = append(result.Diagnostics, s.needDiagnostic(category, err))
			return result, nil
		}
		items = append(items, datasource.AcquisitionItem{Key: request.Need.Name, Entry: request.Need.Name, Category: category.ID, Params: params, Fields: request.Need.Fields, Geometry: category.Geometry, Required: true, Acquirable: true})
	}
	env := s.options.Environment
	env.AutoAcquire = "allow"
	if s.options.Engine != nil {
		env = acquire.PlanningWithStore(ctx, env, s.options.Engine.Store)
	}
	q := query{Request: request, Items: map[string]datasource.AcquisitionItem{}}
	for _, original := range items {
		item := original
		rowName := item.Entry
		if rowName == "" {
			rowName = item.Key
		}
		if rowName == "" {
			return result, failure("DATA_NEED_UNRECOGNIZED", "数据需求缺少名称")
		}
		if _, exists := q.Items[rowName]; exists {
			return result, failure("DATA_NEED_UNRECOGNIZED", "数据需求名称重复："+rowName)
		}
		item.Key = request.Need.Name + "/" + rowName
		item.Requirement = request.Need.Name
		item.Extent = extent
		item.ExtentExpression = request.Need.Extent
		item.Scale = scale
		item.ScaleExpression = request.Need.Scale
		item.Mode = "auto"
		if item.Params == nil {
			item.Params = map[string]string{}
		}
		for k, v := range request.Need.Params {
			item.Params[k] = v
		}
		category, params, err := env.Catalog.ResolveAcquisitionCategory(item.Category, item.Params)
		if err != nil || category == nil {
			diagnostic := s.needDiagnostic(category, err)
			diagnostic.Row = rowName
			result.Diagnostics = append(result.Diagnostics, diagnostic)
			continue
		}
		item.Category = category.ID
		item.Params = params
		item.Geometry = category.Geometry
		if requested := NormalizeGeometry(request.Need.Geometry); requested != "" && requested != NormalizeGeometry(category.Geometry) {
			return result, failure("DATA_GEOMETRY_MISMATCH", "需求几何与类别不符："+request.Need.Geometry+" / "+category.Geometry)
		}
		if item.Fields == nil {
			item.Fields = request.Need.Fields
		}
		ranked := env.Rank(item)
		row := s.candidates(ctx, request, rowName, ranked)
		providedInvalid := false
		if len(request.Provided) == 1 {
			if info, e := os.Stat(request.Provided[0]); e == nil && info.Mode().IsRegular() {
				suppliedOK := false
				for _, c := range row.Candidates {
					if c.Kind == "user" && c.Path == request.Provided[0] {
						suppliedOK = true
					}
				}
				if !suppliedOK {
					providedInvalid = true
					row.Recommended = ""
					for i := range row.Candidates {
						row.Candidates[i].Available = false
						row.Candidates[i].Notes = append(row.Candidates[i].Notes, "用户明确提供的文件未通过核验，须先澄清，不自动替换来源")
					}
					result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "DATA_PROVIDED_INVALID", Message: "用户提供的文件未通过几何或字段核验，请确认文件用途或重新提供，不自动改用下载", Row: rowName})
				}
			}
		}

		for _, c := range row.Candidates {
			if c.Kind == "user" {
				result.ProvidedMatches = append(result.ProvidedMatches, c)
			}
		}
		for _, ex := range row.Excluded {
			if ex.Path != "" {
				result.ProvidedUnmatched = append(result.ProvidedUnmatched, ex)
			}
		}
		q.Items[rowName] = ranked
		if row.Recommended == "" {
			diagnostic := Diagnostic{Code: "DATA_NO_CANDIDATE", Message: "没有可用的直接来源或探索途径，请提供数据或调整需求", Row: rowName}
			if !providedInvalid && request.IncludeFallback && s.options.Config.FallbackPolicy != "deny" && s.options.Config.NetworkPolicy != "deny" {
				fallback := s.fallbackCandidates(ranked, rowName)
				result.Fallback = append(result.Fallback, fallback...)
				for _, candidate := range fallback {
					if candidate.Available {
						diagnostic.Code = "DATA_EXPLORATION_REQUIRED"
						diagnostic.Message = "暂无确定性来源，但有可用探索途径；沿用本次 find_id 安排 acquire 步骤，由数据获取智能体探索并登记真实数据"
						for i := range row.Excluded {
							if candidate.SourceID != "" && row.Excluded[i].SourceID == candidate.SourceID {
								row.Excluded[i].Reason = "确定性获取路径被排除：" + row.Excluded[i].Reason + "；该来源仍可通过探索获取，见 fallback 中的可用途径"
							}
						}
					}
				}
			}
			if !providedInvalid {
				result.Diagnostics = append(result.Diagnostics, diagnostic)
			}
		}
		result.Rows = append(result.Rows, row)
	}
	describeFindState(&result)
	q.Result = result
	if err := s.saveQuery(q); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Service) needDiagnostic(category *datasource.Category, err error) Diagnostic {
	diagnostic := Diagnostic{Code: "DATA_NEED_UNRECOGNIZED", Message: "无法识别数据需求"}
	if err != nil {
		diagnostic.Message = err.Error()
	}
	var invalid *datasource.ParameterValidationError
	if errors.As(err, &invalid) {
		diagnostic.Code = "DATA_NEED_PARAMS_INVALID"
		for _, issue := range invalid.Issues {
			path := "need.params." + issue.Name
			diagnostic.Parameters = append(diagnostic.Parameters, ParameterRequirement{
				Path: path, Required: issue.Required, AllowedValues: issue.AllowedValues, ProvidedValue: issue.Value,
			})
			suggestion := path + "：按实际需求填写"
			if issue.Required {
				suggestion += "（必填）"
			}
			if len(issue.AllowedValues) > 0 {
				suggestion += "；允许值：" + strings.Join(issue.AllowedValues, "、")
			}
			diagnostic.Suggestions = append(diagnostic.Suggestions, suggestion)
		}
	} else if category == nil && s.options.Environment.Catalog != nil {
		for _, c := range s.options.Environment.Catalog.Categories {
			diagnostic.Suggestions = append(diagnostic.Suggestions, c.Name)
		}
	}
	return diagnostic
}
func safeLeaf(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsAny(name, "/\\\x00\r\n")
}
