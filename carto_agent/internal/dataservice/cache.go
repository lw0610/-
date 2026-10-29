package dataservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type query struct {
	Request FindRequest                           `json:"request"`
	Result  FindResult                            `json:"result"`
	Items   map[string]datasource.AcquisitionItem `json:"items"`
}

var findPattern = regexp.MustCompile(`^df-[a-f0-9]{32}$`)

func digest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func atomicJSON(path string, value any) error {
	if err := bindings.RejectSymlinks(path); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".data-service-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func (s *Service) load(id string) (query, error) {
	var q query
	if strings.TrimSpace(id) == "" {
		return q, failure("DATA_QUERY_REQUIRED", "缺少 find_id；请使用 data_find 成功返回的编号。已取得文件可用 data_register 的 need 明确登记需求")
	}
	if !findPattern.MatchString(id) {
		return q, failure("DATA_QUERY_INVALID", "find_id 格式无效；只能使用 data_find 成功返回的编号，不要自行构造")
	}
	if s.options.SessionDir == "" {
		return q, fmt.Errorf("数据服务缺少当前会话目录")
	}
	path := filepath.Join(s.options.SessionDir, "data", "finds", id+".json")
	if err := bindings.RejectSymlinks(path); err != nil {
		return q, err
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return q, failure("DATA_QUERY_NOT_FOUND", "当前会话中没有此查询；data_find 提示 correct_need 时须先更正需求，不能使用未完成查询的编号")
	}
	if err != nil {
		return q, err
	}
	if len(raw) > 16<<20 {
		return q, failure("DATA_QUERY_INVALID", "数据来源查询文件过大")
	}
	if err = json.Unmarshal(raw, &q); err != nil {
		return q, err
	}
	if !s.options.Now().Before(q.Result.ExpiresAt) {
		return q, failure("DATA_FIND_EXPIRED", "数据来源查询已超过30分钟，请重新查询")
	}
	return q, nil
}
func (s *Service) FindResult(ctx context.Context, id string) (FindResult, error) {
	if err := ctx.Err(); err != nil {
		return FindResult{}, err
	}
	q, err := s.load(id)
	return q.Result, err
}
func (s *Service) ValidateChoice(ctx context.Context, id string, choices map[string]string) (Selection, error) {
	selection := Selection{FindID: id, Rows: []SelectedRow{}, SelectedAt: s.options.Now().UTC()}
	if err := ctx.Err(); err != nil {
		return selection, err
	}
	q, err := s.load(id)
	if err != nil {
		return selection, err
	}
	selection.Request = q.Request
	selection.Fallback = append([]Candidate(nil), q.Result.Fallback...)
	if len(q.Result.Rows) == 0 {
		return selection, failure("DATA_NEED_UNRESOLVED", "查询尚未形成有效数据需求，请先按诊断补齐需求")
	}
	known := map[string]bool{"*": true}
	for _, row := range q.Result.Rows {
		known[row.Row] = true
	}
	for key := range choices {
		if !known[key] {
			return selection, failure("DATA_CHOICE_INVALID", "选择引用了不存在的数据项："+key)
		}
	}
	for _, row := range q.Result.Rows {
		choice := choices[row.Row]
		if choice == "" {
			choice = choices["*"]
		}
		automatic := choice == "" || choice == "recommended" || choice == "auto"
		if automatic {
			choice = row.Recommended
		}
		found := false
		hasDirect := false
		for _, candidate := range row.Candidates {
			hasDirect = hasDirect || candidate.Available
			if candidate.ID == choice && candidate.Available {
				selection.Rows = append(selection.Rows, SelectedRow{Route: RouteDirect, Row: row.Row, Candidate: candidate, Item: q.Items[row.Row]})
				found = true
				break
			}
		}
		if !found && !hasDirect {
			for _, candidate := range q.Result.Fallback {
				if candidate.Available && (automatic || candidate.ID == choice) {
					selection.Rows = append(selection.Rows, SelectedRow{Route: RouteExplore, Row: row.Row, Candidate: candidate, Item: q.Items[row.Row]})
					found = true
					break
				}
			}
		}
		if !found {
			return selection, failure("DATA_CHOICE_INVALID", "来源不属于此次查询的可用候选："+row.Row)
		}
	}
	return selection, nil
}
func (s *Service) saveQuery(q query) error {
	if s.options.SessionDir == "" {
		return nil
	}
	return atomicJSON(filepath.Join(s.options.SessionDir, "data", "finds", q.Result.FindID+".json"), q)
}
func candidateID(row, kind, identity string) string {
	return "c-" + digest([]string{row, kind, identity})[:16]
}

const queryTTL = 30 * time.Minute
