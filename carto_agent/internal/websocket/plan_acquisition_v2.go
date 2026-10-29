package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/layers"
	bridgepkg "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/stats"
	"github.com/cloudwego/eino/schema"
)

func (s *DeepServer) getPlanDataV2(ctx context.Context, run *flowrun.RunState, d plan.Data) (flowrun.DataResult, error) {
	details := map[string]any{"name": d.Name, "source": d.Source, "style": d.Style, "label_field": d.LabelField, "geometry": d.Geometry, "kind": d.Kind}
	if d.Source == "user" {
		if d.Ref == "消息原文" {
			return flowrun.DataResult{Value: flowrun.EffectiveInputs(run)[d.ID], Details: details}, nil
		}
		if _, err := os.Stat(strings.Split(d.Ref, "|layername=")[0]); err != nil {
			return flowrun.DataResult{}, err
		}
		if d.Kind == "table" && d.Need != nil && d.Need.Indicator != "" {
			service, e := s.dataServiceForPlan(ctx, run)
			if e != nil {
				return flowrun.DataResult{}, e
			}
			n := d.Need
			need := dataservice.AcquisitionNeed{ID: run.CartoPlan.ID + "/" + d.ID, Need: v2ServiceNeed(d, nil), Statistics: &stats.Need{Region: n.Region, Level: n.Level, Year: n.Year, Indicator: n.Indicator, Caliber: n.Caliber, Unit: n.Unit, Exclude: n.Exclude}}
			if need.Statistics.Region == "" && d.Extent != nil {
				need.Statistics.Region = d.Extent.Region
			}
			need, e = service.PrepareAcquisitionNeed(need)
			if e != nil {
				return flowrun.DataResult{}, e
			}
			got, e := service.ValidateStatistics(ctx, need, d.Ref, stats.Source{Title: d.Name, Method: "user", RetrievedAt: time.Now().UTC()})
			if e != nil {
				return flowrun.DataResult{}, e
			}
			return acquiredV2(d, got, details, nil), nil
		}
		details["path"] = d.Ref
		details["load_method"] = "layer_load"
		return flowrun.DataResult{Value: d.Ref, Details: details}, nil
	}
	if d.Source == "project" {
		details["layer_id"] = d.Ref
		return flowrun.DataResult{Value: d.Ref, Details: details}, nil
	}
	extent, err := s.planDataExtent(ctx, run, d)
	if err != nil {
		return flowrun.DataResult{}, err
	}
	if d.Source == "builtin" {
		library, err := layers.NewDefaultLibrary()
		if err != nil {
			return flowrun.DataResult{}, err
		}
		item, err := library.ResolveName(d.Ref)
		if err != nil {
			return flowrun.DataResult{}, err
		}
		if item.Status != "ready" {
			return flowrun.DataResult{}, fmt.Errorf("BUILTIN_LAYER_UNAVAILABLE: %s：%s", d.Ref, strings.Join(item.Issues, "；"))
		}
		if extent != nil && layers.CoverageRatio(item.Coverage.BBox, extent.BBox) < .9 {
			return flowrun.DataResult{}, fmt.Errorf("BUILTIN_COVERAGE_INSUFFICIENT: %s 未覆盖本次范围，请确认其他来源", d.Name)
		}
		details["path"], details["crs"], details["load_method"], details["builtin_name"] = item.Path(), item.CRS, "layer_load_builtin", item.Name
		if extent != nil {
			details["extent"] = extent.BBox
		}
		details["provenance"] = map[string]any{"source": item.Source, "compliance": item.Compliance}
		// Loading belongs to the consuming cartography step, so preparation does not
		// create project layers or couple concurrent acquisition to the QGIS canvas.
		return flowrun.DataResult{Value: item.Path(), Details: details, Notes: []string{item.Name + "：" + item.Source.Title + "；" + item.Source.License + "；" + item.Source.Attribution + "；" + item.Compliance}}, nil
	}
	service, err := s.dataServiceForPlan(ctx, run)
	if err != nil {
		return flowrun.DataResult{}, err
	}
	if raw := run.DataNeeds[d.ID]; len(raw) > 0 {
		var need dataservice.AcquisitionNeed
		if err = json.Unmarshal(raw, &need); err != nil {
			return flowrun.DataResult{}, err
		}
		got, e := service.RegisteredNeed(ctx, need.ID)
		if e == nil {
			return acquiredV2(d, got, details, nil), nil
		}
		var diagnostic *dataservice.Error
		if !errors.As(e, &diagnostic) || diagnostic.Code != "DATA_DISCOVERY_INCOMPLETE" {
			return flowrun.DataResult{}, e
		}
		return s.v2Discovery(ctx, run, d, need, details, nil)
	}
	need := v2ServiceNeed(d, extent)
	layoutResources := make([]*resource.ResolvedResource, 0, len(run.Plan.Resources))
	for _, r := range run.Plan.Resources {
		layoutResources = append(layoutResources, r.ResolvedResource())
	}
	need.Paper, need.MapFrameWidthMM, err = planMapFrame(run.CartoPlan, layoutResources)
	if err != nil {
		return flowrun.DataResult{}, err
	}
	// The same requirement and stable ID survive both agent repair and replan.
	cfg := s.runtimeAcquisitionConfig().WithDefaults()
	handoff := dataservice.AcquisitionNeed{ID: run.CartoPlan.ID + "/" + d.ID, Goal: run.CartoPlan.Goal, Need: need, Acceptance: []string{"实际文件非空，几何与需要的字段匹配", "每项结果记录原始来源和读取方式"}}
	if d.Need != nil && (d.Kind == "table" || d.Need.Indicator != "") {
		n := d.Need
		handoff.Statistics = &stats.Need{Region: n.Region, Level: n.Level, Year: n.Year, Indicator: n.Indicator, Caliber: n.Caliber, Unit: n.Unit, Exclude: n.Exclude}
		if handoff.Statistics.Region == "" && d.Extent != nil {
			handoff.Statistics.Region = d.Extent.Region
		}
		var e error
		handoff, e = service.PrepareAcquisitionNeed(handoff)
		if e != nil {
			return flowrun.DataResult{}, e
		}
		got, hit, e := service.LookupStatistics(ctx, handoff)
		if e != nil {
			return flowrun.DataResult{}, e
		}
		if hit {
			return acquiredV2(d, got, details, nil), nil
		}
	}
	found, err := service.Find(ctx, dataservice.FindRequest{Need: need, IncludeFallback: true})
	if err != nil {
		return flowrun.DataResult{}, err
	}
	ledger := []flowrun.PreparationAttempt{}
	discoveryAvailable := false
	for _, row := range found.Rows {
		for _, candidate := range row.Candidates {
			if !candidate.Available {
				continue
			}
			selection, e := service.ValidateChoice(ctx, found.FindID, map[string]string{row.Row: candidate.ID})
			if e != nil {
				return flowrun.DataResult{}, e
			}
			if len(selection.Rows) > 0 {
				handoff.Item = selection.Rows[0].Item
				handoff.Handbook = candidate.Handbook
			}
			if selection.RequiresDiscovery() {
				discoveryAvailable = true
				continue
			}
			for retry := 0; retry <= 2; retry++ {
				started := time.Now().UTC()
				got, e := service.GetSelection(ctx, selection, dataservice.GetRequest{FindID: found.FindID, Choice: map[string]string{row.Row: candidate.ID}, Target: dataservice.Target{FolderName: d.Name}})
				record := flowrun.PreparationAttempt{Method: candidate.Method, Params: map[string]any{"extent": extent, "source": candidate.SourceID}, StartedAt: started, FinishedAt: time.Now().UTC(), Result: "completed"}
				if record.Method == "" {
					record.Method = candidate.Kind
				}
				if e != nil {
					record.Result = "failed"
					record.Reason = e.Error()
				} else {
					for _, r := range got.Rows {
						record.Paths = append(record.Paths, r.Paths...)
					}
				}
				var processingError *acquire.ProcessingError
				if errors.As(e, &processingError) {
					record.Paths = append(record.Paths, processingError.Paths...)
					ledger = append(ledger, record)
					return flowrun.DataResult{Ledger: ledger}, e
				}
				ledger = append(ledger, record)
				var toolError *bridgepkg.ToolInvokeError
				if errors.As(e, &toolError) && (toolError.Code == bridgepkg.ToolErrorCodeNotReady || toolError.Code == bridgepkg.ToolErrorCodeProviderMissing || toolError.Code == bridgepkg.ToolErrorCodeInstanceRequired) {
					return flowrun.DataResult{Ledger: ledger}, e
				}
				if e == nil && got.Status == "completed" && len(got.Rows) > 0 {
					return acquiredV2(d, got, details, ledger), nil
				}
				if e == nil || !flowrun.TransientFailure(e) || retry == 2 {
					break
				}
				timer := time.NewTimer(time.Duration(1<<retry) * time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return flowrun.DataResult{Ledger: ledger}, ctx.Err()
				case <-timer.C:
				}
			}
		}
	}
	if handoff.Item.Category == "" {
		handoff.Item = datasource.AcquisitionItem{Key: d.ID, Entry: d.Name, Requirement: d.Name, Category: found.Resolved.Category, Params: need.Params, Fields: need.Fields, Geometry: d.Geometry, Required: true, Acquirable: true, Extent: extent, Scale: found.Resolved.ScaleDenominator}
	}
	if d.Method != "" && !discoveryAvailable {
		return flowrun.DataResult{Ledger: ledger}, fmt.Errorf("DATA_METHOD_FAILED: 指定方式 %s 未取得合格数据，请确认其他方式", d.Method)
	}
	if cfg.FallbackPolicy == "deny" || cfg.NetworkPolicy == "deny" {
		return flowrun.DataResult{Ledger: ledger}, fmt.Errorf("DATA_ACQUISITION_UNAVAILABLE: 确定方式未取得数据，设置未允许联网获取数据")
	}
	for _, entry := range ledger {
		raw, _ := json.Marshal(entry)
		var item map[string]any
		_ = json.Unmarshal(raw, &item)
		handoff.Ledger = append(handoff.Ledger, item)
	}
	return s.v2Discovery(ctx, run, d, handoff, details, ledger)
}
func acquiredV2(d plan.Data, got dataservice.GetResult, details map[string]any, ledger []flowrun.PreparationAttempt) flowrun.DataResult {
	paths := []string{}
	notes := []string{}
	var value any
	for _, row := range got.Rows {
		paths = append(paths, row.Paths...)
		if row.Kind == "builtin_layer" {
			details["load_method"] = "layer_load_builtin"
			details["builtin_ref"] = row.SourceID
		}
		if row.LayerID != "" {
			value = row.LayerID
			details["layer_id"] = row.LayerID
		}
		notes = append(notes, row.Notes...)
		notes = append(notes, row.Warnings...)
		notes = append(notes, fmt.Sprintf("%s：%s；%s；%s", d.Name, row.SourceID, row.Attribution, row.RetrievedAt.Format("2006-01-02")))
	}
	if value == nil {
		if len(paths) == 1 {
			value = paths[0]
		} else if len(paths) > 1 {
			value = paths
		}
	}
	details["paths"] = paths
	details["provenance"] = notes
	return flowrun.DataResult{Value: value, Details: details, Notes: notes, Ledger: ledger}
}
func (s *DeepServer) v2Discovery(ctx context.Context, run *flowrun.RunState, d plan.Data, need dataservice.AcquisitionNeed, details map[string]any, ledger []flowrun.PreparationAttempt) (flowrun.DataResult, error) {
	role, err := s.planAcquisitionRole(ctx, run, d.ID)
	if err != nil {
		return flowrun.DataResult{Details: details, Ledger: ledger}, err
	}
	raw, err := json.Marshal(need)
	if err != nil {
		return flowrun.DataResult{}, err
	}
	text := "完成当前数据需求。按需求、合格标准和台账继续取数，避免重复失败的方法。使用当前角色已配置的取数与读取工具核验源文件，实际取得并检查内容后用 data_register 提交路径、来源标题、网址、表号/页码和读取方式。需求编号由系统上下文绑定，无需 find_id。\n" + string(raw)
	return flowrun.DataResult{Details: details, Ledger: ledger, Preparation: &flowrun.DataPreparation{DataID: d.ID, Need: raw, Role: role, Messages: []*schema.Message{schema.UserMessage(text)}}}, nil
}
func (s *DeepServer) planDataExtent(ctx context.Context, run *flowrun.RunState, d plan.Data) (*datasource.ExtentPlan, error) {
	if d.Extent == nil {
		return nil, nil
	}
	spec := d.Extent
	if len(spec.BBox) == 4 {
		return &datasource.ExtentPlan{BBox: append([]float64{}, spec.BBox...), Source: d.ID}, nil
	}
	sources := []string{}
	if spec.Region != "" {
		sources = append(sources, spec.Region)
	}
	if spec.From != "" {
		values := flowrun.EffectiveInputs(run)
		source := values[spec.From]
		if o, step, ok := run.CartoPlan.ProductByID(spec.From); ok {
			for _, state := range run.Steps {
				if state.ID == step && state.Status == "completed" {
					source = state.Outputs[o.ID]
				}
			}
		}
		var collect func(any)
		collect = func(value any) {
			switch v := value.(type) {
			case string:
				if v != "" {
					sources = append(sources, v)
				}
			case []string:
				sources = append(sources, v...)
			case []any:
				for _, child := range v {
					collect(child)
				}
			case map[string]any:
				for _, key := range []string{"path", "layer_id", "id", "layers", "layer_ids", "paths"} {
					if child, ok := v[key]; ok {
						collect(child)
						return
					}
				}
			}
		}
		collect(source)
		if len(sources) == 0 {
			return nil, fmt.Errorf("DATA_EXTENT_UNRESOLVED: %s 的范围数据 %s 尚未就绪", d.Name, spec.From)
		}
	}
	binding := flowRuntimeBinding{server: s}
	var catalog *datasource.Catalog
	if spec.Region != "" && s.cartoRuntime != nil {
		env, e := s.cartoRuntime.options.LoadEnvironment(ctx, run.Scheme)
		if e != nil {
			return nil, e
		}
		catalog = env.DataSources
	}
	inspect := func(ctx context.Context, source string) (*flow.ExtentPlan, error) {
		args := map[string]any{}
		if _, e := os.Stat(strings.Split(source, "|layername=")[0]); e == nil {
			args["path"] = source
		} else {
			args["layer_id"] = source
		}
		result, e := binding.Invoke(ctx, "layer_inspect", args)
		if e != nil {
			return nil, e
		}
		payload, e := acquire.UnwrapToolResult(result)
		if e != nil {
			return nil, e
		}
		metadata, ok := payload["data"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("layer_inspect 未返回图层元数据 data")
		}
		raw, _ := json.Marshal(metadata["coverage"])
		var coverage struct {
			BBox []float64 `json:"bbox"`
		}
		if e = json.Unmarshal(raw, &coverage); e != nil {
			return nil, e
		}
		if !plan.ValidBBox(coverage.BBox) {
			return nil, fmt.Errorf("图层范围不可用：%s", source)
		}
		return &flow.ExtentPlan{BBox: coverage.BBox, Source: source}, nil
	}
	var extent *datasource.ExtentPlan
	for _, source := range sources {
		bounds, err := acquire.ResolveExtent(ctx, source, catalog, inspect)
		if err != nil {
			return nil, err
		}
		if extent == nil {
			extent = bounds
			continue
		}
		extent.BBox[0] = math.Min(extent.BBox[0], bounds.BBox[0])
		extent.BBox[1] = math.Min(extent.BBox[1], bounds.BBox[1])
		extent.BBox[2] = math.Max(extent.BBox[2], bounds.BBox[2])
		extent.BBox[3] = math.Max(extent.BBox[3], bounds.BBox[3])
	}
	if extent == nil {
		return nil, fmt.Errorf("DATA_EXTENT_UNRESOLVED: %s 未提供范围", d.Name)
	}
	extent.ExpandPercent = spec.Expand * 100
	if err := acquire.PrepareExtent(extent); err != nil {
		return nil, err
	}
	return extent, nil
}
