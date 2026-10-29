package dataservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
	"github.com/cloudwego/eino/adk"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var provenanceLocks sync.Map

const onlineTilesUnsupported = "当前统一数据服务尚不支持安全加载此类在线瓦片：加载前须单独确认密钥写入 QGIS 工程。请使用已加载的工程图层或已下载的栅格文件；图层定义 JSON 不是已取得的数据。"

func (s *Service) Get(ctx context.Context, request GetRequest) (GetResult, error) {
	if pinned, ok := AcquisitionSelection(ctx); ok {
		if request.FindID != pinned.FindID {
			return GetResult{}, failure("DATA_QUERY_SCOPE_MISMATCH", "当前获取任务只能使用已绑定的查询："+pinned.FindID)
		}
		return s.GetSelection(ctx, pinned, request)
	}
	selection, err := s.ValidateChoice(ctx, request.FindID, request.Choice)
	if err != nil {
		return GetResult{}, err
	}
	if selection.RequiresDiscovery() {
		// Explicitly bind this selection, never infer it from the latest query.
		selection, err = s.ResolveSelectionExtent(ctx, selection, request.ResolvedData)
		if err != nil {
			return GetResult{}, err
		}
		raw, _ := json.Marshal(selection)
		if err := adk.SetRunLocalValue(ctx, "data-service.selection", string(raw)); err != nil {
			return GetResult{}, failure("DATA_QUERY_SCOPE_REQUIRED", "探索获取需要计划获取步骤或系统数据获取智能体")
		}
		return GetResult{FindID: selection.FindID, Status: "exploration_required", Preparation: &selection, Rows: []GetRow{}}, nil
	}
	return s.GetSelection(ctx, selection, request)
}

// GetSelection accepts only a selection pinned by the trusted plan executor.
// Tool callers cannot supply this struct; they must pass an unexpired find_id.
func (s *Service) GetSelection(ctx context.Context, selection Selection, request GetRequest) (GetResult, error) {
	if !findPattern.MatchString(selection.FindID) || len(selection.Rows) == 0 {
		return GetResult{FindID: selection.FindID, Status: "failed", Rows: []GetRow{}}, failure("DATA_CHOICE_INVALID", "已确认的查询选择无效，请重新查找并选择数据来源")
	}
	if selection.RequiresDiscovery() {
		return s.registeredSelection(ctx, selection)
	}
	var err error
	selection, err = s.ResolveSelectionExtent(ctx, selection, request.ResolvedData)
	if err != nil {
		return GetResult{}, err
	}
	result := GetResult{FindID: selection.FindID, Status: "running", Rows: []GetRow{}}
	if s.options.Engine == nil || s.options.SessionDir == "" {
		return result, fmt.Errorf("数据获取执行环境不可用")
	}
	folder := request.Target.FolderName
	if folder == "" {
		folder = selection.Request.Need.Name
	}
	if !safeLeaf(folder) {
		return result, failure("DATA_CHOICE_INVALID", "获取目录须为单个文件夹名称")
	}
	result.Folder = filepath.Join(s.options.SessionDir, "output", "acquired", folder)
	if err := bindings.RejectSymlinks(result.Folder); err != nil {
		return result, err
	}
	for _, selected := range selection.Rows {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		candidate := selected.Candidate
		item := selected.Item
		if candidate.Network {
			if s.options.Config.NetworkPolicy == "deny" {
				return result, failure("DATA_NETWORK_NOT_ALLOWED", "设置禁止联网获取")
			}
			if s.options.Config.NetworkPolicy == "confirm" && !s.options.NetworkConfirmed {
				return result, failure("DATA_NETWORK_NOT_ALLOWED", "请先确认包含联网获取的制图计划")
			}
		}
		var got acquire.ItemResult
		var err error
		if candidate.LayerID != "" {
			got, err = s.validateLayer(ctx, item, candidate)
		} else if candidate.Path != "" {
			localPath := candidate.Path
			if candidate.Sublayer != "" {
				localPath += "|layername=" + candidate.Sublayer
			}
			_, err = ValidateLocalData(ctx, localPath, item.Geometry, item.Fields)
			got = acquire.ItemResult{Status: "user_provided", Output: acquire.OutputRecord{Paths: []string{localPath}}, Notes: []string{"用户提供；本地核验几何与字段，原文件保持只读"}}
		} else if candidate.Choice != nil {
			item.Chosen = candidate.Choice
			item.Alternatives = nil
			item.Mode = "auto"
			engine := *s.options.Engine
			if s.options.Config.NetworkPolicy == "deny" || (s.options.Config.NetworkPolicy == "confirm" && !s.options.NetworkConfirmed) {
				engine.Adapters = map[string]acquire.Adapter{}
				for kind, adapter := range s.options.Engine.Adapters {
					if kind == "local_folder" || kind == "offline_asset" {
						engine.Adapters[kind] = adapter
					} else {
						engine.Adapters[kind] = deniedAdapter{kind: kind}
					}
				}
			}
			got, err = engine.RunItem(ctx, item, func(p acquire.Progress) {
				if s.options.Progress != nil {
					s.options.Progress(selected.Row, p)
				}
			})
		} else {
			err = failure("DATA_CHOICE_INVALID", "此候选需要数据获取智能体兜底并登记")
		}
		if err != nil {
			result.Status = "failed"
			return result, err
		}
		// RunItem may return a successful preparation that still awaits consent.
		// Do not copy, register or remember that intermediate definition as data.
		if got.Output.LayerDefinition != nil {
			result.Status = "failed"
			return result, failure("DATA_ONLINE_TILES_UNSUPPORTED", onlineTilesUnsupported)
		}
		switch got.Status {
		case "completed", "reused", "user_provided":
		default:
			result.Status = "failed"
			return result, failure("DATA_RESULT_INCOMPLETE", "数据获取尚未完成："+got.Status)
		}
		row := GetRow{Row: selected.Row, Category: item.Category, Kind: candidate.Kind, Paths: append([]string{}, got.Output.Paths...), LayerID: got.Output.LayerID, SourceID: got.SourceID, StoreKey: got.StoreKey, Count: got.Output.Count, RetrievedAt: s.options.Now().UTC(), SHA256: map[string]string{}, Notes: append([]string{}, got.Notes...), Warnings: []string{}}
		if row.SourceID == "" {
			row.SourceID = candidate.SourceID
		}
		if source := s.options.Environment.Catalog.Source(row.SourceID); source != nil {
			row.License = source.License
			row.Attribution = source.Attribution
		}
		if candidate.Choice != nil {
			for i, path := range row.Paths {
				base := filepath.Base(path)
				if i == 0 {
					stem := strings.TrimSuffix(filepath.Base(item.Filename), filepath.Ext(item.Filename))
					if stem == "" || stem == "." {
						stem = selected.Row
					}
					if !safeLeaf(stem) {
						stem = "data-" + digest(selected.Row)[:12]
					}
					base = stem + filepath.Ext(path)
				}
				target := filepath.Join(result.Folder, base)
				if err = copyFile(path, target); err != nil {
					return result, err
				}
				row.Paths[i] = target
			}
		}
		for _, path := range row.Paths {
			sha, err := fileSHA(strings.Split(path, "|layername=")[0])
			if err != nil {
				return result, err
			}
			row.SHA256[path] = sha
		}
		if err = s.recordRow(&result, row); err != nil {
			return result, err
		}
		if s.options.Memory != nil {
			entry := flow.DataMemoryEntry{Guide: selection.Request.Guide, Name: selected.Row, Category: item.Category, Extent: selection.Request.Need.Extent}
			explicit := request.Choice[selected.Row]
			if explicit == "" {
				explicit = request.Choice["*"]
			}
			if candidate.Kind == "user" {
				entry.Path = candidate.Path
				if candidate.Sublayer != "" {
					entry.Path += "|layername=" + candidate.Sublayer
				}
			} else if explicit != "" && explicit != "recommended" && explicit != "auto" {
				entry.SourceID = candidate.SourceID
			}
			if entry.Path != "" || entry.SourceID != "" {
				if err = s.options.Memory.Remember(entry); err != nil {
					return result, err
				}
			}
		}
	}
	result.Status = "completed"
	return result, nil
}

type deniedAdapter struct{ kind string }

func (a deniedAdapter) Kind() string { return a.kind }
func (a deniedAdapter) Fetch(context.Context, acquire.FetchRequest) (acquire.FetchResult, error) {
	return acquire.FetchResult{}, failure("DATA_NETWORK_NOT_ALLOWED", "数据仓结果不可用且设置未允许重新联网获取")
}
func (s *Service) validateLayer(ctx context.Context, item flow.AcquisitionItem, c Candidate) (acquire.ItemResult, error) {
	if s.options.Layers == nil {
		return acquire.ItemResult{}, failure("DATA_REGISTER_MISMATCH", "工程图层无法核验")
	}
	layers, err := s.options.Layers(ctx)
	if err != nil {
		return acquire.ItemResult{}, err
	}
	for _, layer := range layers {
		if layer.LayerID != c.LayerID {
			continue
		}
		expectedGeometry := item.Geometry
		if expectedGeometry == "tiles" {
			expectedGeometry = "raster"
		}
		if NormalizeGeometry(layer.Geometry) != NormalizeGeometry(expectedGeometry) {
			return acquire.ItemResult{}, failure("DATA_REGISTER_MISMATCH", "工程图层几何类型不符")
		}
		for field := range item.Fields {
			found := false
			for _, name := range layer.Fields {
				if name == field {
					found = true
				}
			}
			if !found {
				return acquire.ItemResult{}, failure("DATA_REGISTER_MISMATCH", "工程图层缺少字段："+field)
			}
		}
		return acquire.ItemResult{Status: "user_provided", Output: acquire.OutputRecord{LayerID: c.LayerID, Paths: []string{}}, Notes: []string{"使用当前工程图层"}}, nil
	}
	return acquire.ItemResult{}, failure("DATA_REGISTER_MISMATCH", "工程图层已经不存在")
}
func copyFile(source, target string) error {
	if err := bindings.RejectSymlinks(target); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	f, err := os.CreateTemp(filepath.Dir(target), ".data-copy-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = io.Copy(f, in); err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), target)
}
func fileSHA(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func (s *Service) recordRow(result *GetResult, row GetRow) error {
	row.FindID = result.FindID
	store, err := sessionfiles.NewWithRoots(s.options.SessionID, s.options.SessionDir, s.options.Engine.Store.Root)
	if err != nil {
		return err
	}
	for _, path := range row.Paths {
		path = strings.Split(path, "|layername=")[0]
		origin := sessionfiles.Origin{Type: "data_service", Tool: "data_get", Label: row.Row, SourceID: row.SourceID, License: row.License}
		if _, err = store.Register(path, origin, sessionfiles.WithGroup("data")); err != nil {
			if row.Kind == "user" || row.Kind == "user_memory" || row.Kind == "session" {
				_, err = store.RegisterProvided(path, origin, sessionfiles.WithGroup("data"))
			}
			if err != nil {
				return err
			}
		}
	}
	result.Rows = append(result.Rows, row)
	path := filepath.Join(s.options.SessionDir, "output", "data-sources.json")
	value, _ := provenanceLocks.LoadOrStore(path, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	rows := []GetRow{}
	if raw, err := os.ReadFile(path); err == nil {
		if err = json.Unmarshal(raw, &rows); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	filtered := []GetRow{}
	for _, old := range rows {
		if old.Row != row.Row || old.Category != row.Category {
			filtered = append(filtered, old)
		}
	}
	rows = append(filtered, row)
	if err = atomicJSON(path, rows); err != nil {
		return err
	}
	report := filepath.Join(s.options.SessionDir, "output", "数据来源说明.md")
	lines := []string{"# 数据来源说明", ""}
	for _, r := range rows {
		lines = append(lines, "- "+r.Row+"："+r.SourceID+"（"+r.Kind+"）；"+r.RetrievedAt.Format("2006-01-02 15:04:05")+"；许可："+r.License+"；署名："+r.Attribution)
		for _, note := range r.Notes {
			lines = append(lines, "  - "+note)
		}
	}
	if err = bindings.RejectSymlinks(report); err != nil {
		return err
	}
	if err = os.WriteFile(report, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		return err
	}
	_, err = store.Register(report, sessionfiles.Origin{Type: "data_service", Tool: "data_get", Label: "数据来源说明"})
	result.ProvenancePath = report
	return err
}
