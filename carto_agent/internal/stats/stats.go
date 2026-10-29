// Package stats validates statistical observations once, then stores verified tables by meaning.
package stats

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/acquire"
	_ "github.com/mattn/go-sqlite3"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Unit struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Level string `json:"level,omitempty"`
}
type Need struct {
	RegionCode   string   `json:"region_code"`
	Region       string   `json:"region"`
	Level        string   `json:"level"`
	Year         int      `json:"year"`
	Indicator    string   `json:"indicator"`
	Caliber      string   `json:"caliber"`
	Unit         string   `json:"unit"`
	Exclude      []string `json:"exclude,omitempty"`
	Total        *float64 `json:"total,omitempty"`
	RoundingUnit float64  `json:"rounding_unit,omitempty"`
}
type Source struct {
	Title        string    `json:"title"`
	URL          string    `json:"url"`
	TableRef     string    `json:"table_ref"`
	Method       string    `json:"method"`
	RetrievedAt  time.Time `json:"retrieved_at"`
	OriginalPath string    `json:"original_path,omitempty"`
	Official     bool      `json:"official"`
}
type Observation struct {
	Unit
	Year      int     `json:"year"`
	Indicator string  `json:"indicator"`
	Caliber   string  `json:"caliber"`
	Value     float64 `json:"value"`
	ValueUnit string  `json:"unit"`
}
type CheckResult struct {
	OK        bool     `json:"ok"`
	Issues    []string `json:"issues"`
	Sum       float64  `json:"sum"`
	Total     *float64 `json:"total,omitempty"`
	Tolerance float64  `json:"tolerance"`
}
type Dataset struct {
	Need         Need          `json:"need"`
	Source       Source        `json:"source"`
	Observations []Observation `json:"observations"`
	Checks       CheckResult   `json:"checks"`
	Path         string        `json:"path"`
}

// Known aliases are exact identities. Removing arbitrary administrative suffixes
// would conflate different places, so only aliases of the expected units are admitted.
func aliases(unit Unit) []string {
	values := []string{unit.Code, unit.Name}
	known := map[string][]string{
		"阿坝藏族羌族自治州": {"阿坝州"}, "甘孜藏族自治州": {"甘孜州"}, "凉山彝族自治州": {"凉山州"},
		"延边朝鲜族自治州": {"延边州"}, "恩施土家族苗族自治州": {"恩施州"}, "湘西土家族苗族自治州": {"湘西州"},
		"黔东南苗族侗族自治州": {"黔东南州"}, "黔南布依族苗族自治州": {"黔南州"}, "黔西南布依族苗族自治州": {"黔西南州"},
		"西双版纳傣族自治州": {"西双版纳州"}, "大理白族自治州": {"大理州"}, "德宏傣族景颇族自治州": {"德宏州"},
		"红河哈尼族彝族自治州": {"红河州"}, "文山壮族苗族自治州": {"文山州"}, "楚雄彝族自治州": {"楚雄州"},
		"怒江傈僳族自治州": {"怒江州"}, "迪庆藏族自治州": {"迪庆州"}, "巴音郭楞蒙古自治州": {"巴州"}, "博尔塔拉蒙古自治州": {"博州"},
		"伊犁哈萨克自治州": {"伊犁州"}, "昌吉回族自治州": {"昌吉州"}, "克孜勒苏柯尔克孜自治州": {"克州"},
	}
	values = append(values, known[unit.Name]...)
	if unit.Name == "济源市" {
		values = append(values, "济源", "济源示范区", "济源产城融合示范区")
	}
	for _, suffix := range []string{"市", "地区", "盟", "自治州"} {
		if strings.HasSuffix(unit.Name, suffix) {
			values = append(values, strings.TrimSuffix(unit.Name, suffix))
		}
	}
	return values
}
func normalizedName(s string) string { return strings.Join(strings.Fields(strings.TrimSpace(s)), "") }

// CheckFile is the trust boundary for downloaded or user-supplied CSV/XLSX.
// A blank number is an error, never silently converted to zero.
func CheckFile(path string, need Need, expected []Unit, source Source) (Dataset, error) {
	rows, err := acquire.ReadTable(path)
	if err != nil {
		return Dataset{}, err
	}
	data := Dataset{Need: need, Source: source, Path: path, Checks: CheckResult{Issues: []string{}}}
	if len(rows) < 2 {
		return data, fmt.Errorf("统计表没有数据行")
	}
	if need.Indicator == "" || need.Unit == "" {
		return data, fmt.Errorf("统计需求需要确定年份、指标和单位；最新年份应由原始来源确定")
	}
	if source.Title == "" || source.Method == "" || (source.Method != "user" && (source.URL == "" || source.TableRef == "")) {
		return data, fmt.Errorf("统计来源须提供标题、网址、表号/页码和读取方式")
	}
	names := map[string]Unit{}
	required := map[string]Unit{}
	for _, unit := range expected {
		excluded := false
		for _, v := range need.Exclude {
			if v == unit.Code || v == unit.Name {
				excluded = true
			}
		}
		if excluded {
			continue
		}
		required[unit.Code] = unit
		for _, alias := range aliases(unit) {
			names[normalizedName(alias)] = unit
		}
	}
	if len(required) == 0 {
		return data, fmt.Errorf("统计需求缺少期望行政单元清单")
	}
	columns := map[string]int{}
	for i, name := range rows[0] {
		columns[strings.TrimPrefix(strings.TrimSpace(name), "\ufeff")] = i
	}
	find := func(candidates ...string) int {
		for _, name := range candidates {
			if i, ok := columns[name]; ok {
				return i
			}
		}
		return -1
	}
	regionColumn := find("region_code", "adcode", "行政区代码", "region_name", "name", "地区", "城市", "市名", "行政区", "地市")
	valueColumn := find("value", need.Indicator, "人口", "常住人口", "常住人口（万人）", "常住人口(万人)", "population", strconv.Itoa(need.Year), strconv.Itoa(need.Year)+"年")
	yearColumn := find("year", "年份")
	if need.Year == 0 {
		if yearColumn >= 0 && yearColumn < len(rows[1]) {
			need.Year, _ = strconv.Atoi(strings.TrimSpace(rows[1][yearColumn]))
		}
		if need.Year == 0 {
			for header := range columns {
				year, _ := strconv.Atoi(strings.TrimSuffix(header, "年"))
				if year >= 1900 && year <= 2100 && year > need.Year {
					need.Year = year
				}
			}
		}
		if need.Year == 0 {
			return data, fmt.Errorf("表格未包含可确认的年份，请在成果表添加 year 列")
		}
		data.Need.Year = need.Year
		if valueColumn < 0 {
			valueColumn = find(strconv.Itoa(need.Year), strconv.Itoa(need.Year)+"年")
		}
	}

	unitColumn := find("unit", "单位")
	indicatorColumn := find("indicator", "指标")
	caliberColumn := find("caliber", "口径")
	if regionColumn < 0 || valueColumn < 0 {
		return data, fmt.Errorf("统计表缺少地区列或数值列（region_name/value 或对应中文列）")
	}
	read := func(row []string, col int) string {
		if col < 0 || col >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[col])
	}
	seen := map[string]bool{}
	total := need.Total
	for index, row := range rows[1:] {
		name := normalizedName(read(row, regionColumn))
		if name == "" {
			continue
		}
		raw := strings.ReplaceAll(read(row, valueColumn), ",", "")
		value, parseErr := strconv.ParseFloat(raw, 64)
		if parseErr != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			data.Checks.Issues = append(data.Checks.Issues, fmt.Sprintf("第%d行 %s 的数值为空或不是有限数字", index+2, name))
			continue
		}
		if name == "全省" || name == "全省Total" || name == need.Region || name == need.RegionCode {
			copy := value
			total = &copy
			continue
		}
		unit, ok := names[name]
		if !ok {
			data.Checks.Issues = append(data.Checks.Issues, "非预期统计单元："+name)
			continue
		}
		if seen[unit.Code] {
			data.Checks.Issues = append(data.Checks.Issues, "重复统计单元："+unit.Name)
			continue
		}
		seen[unit.Code] = true
		if yearColumn >= 0 && read(row, yearColumn) != strconv.Itoa(need.Year) {
			data.Checks.Issues = append(data.Checks.Issues, "年份不符："+unit.Name)
		}
		if unitColumn >= 0 && read(row, unitColumn) != need.Unit {
			data.Checks.Issues = append(data.Checks.Issues, "单位不符："+unit.Name)
		}
		if indicatorColumn >= 0 && read(row, indicatorColumn) != need.Indicator {
			data.Checks.Issues = append(data.Checks.Issues, "指标不符："+unit.Name)
		}
		if caliberColumn >= 0 && read(row, caliberColumn) != need.Caliber {
			data.Checks.Issues = append(data.Checks.Issues, "统计口径不符："+unit.Name)
		}
		data.Observations = append(data.Observations, Observation{Unit: unit, Year: need.Year, Indicator: need.Indicator, Caliber: need.Caliber, Value: value, ValueUnit: need.Unit})
		data.Checks.Sum += value
	}
	for _, unit := range expected {
		if _, ok := required[unit.Code]; ok && !seen[unit.Code] {
			data.Checks.Issues = append(data.Checks.Issues, "缺少统计单元："+unit.Name)
		}
	}
	data.Checks.Total = total
	if total != nil && len(need.Exclude) == 0 {
		if need.RoundingUnit > 0 {
			data.Checks.Tolerance = float64(len(required)) * .5 * need.RoundingUnit
		} else {
			data.Checks.Tolerance = math.Abs(*total) * .005
		}
		if math.Abs(data.Checks.Sum-*total) > data.Checks.Tolerance {
			data.Checks.Issues = append(data.Checks.Issues, fmt.Sprintf("各单元合计 %g 与全省 %g 不符，允许差 %g %s", data.Checks.Sum, *total, data.Checks.Tolerance, need.Unit))
		}
	}
	data.Checks.OK = len(data.Checks.Issues) == 0
	if !data.Checks.OK {
		return data, fmt.Errorf("统计表未通过检查：%s", strings.Join(data.Checks.Issues, "；"))
	}
	if data.Source.RetrievedAt.IsZero() {
		data.Source.RetrievedAt = time.Now().UTC()
	}
	return data, nil
}

type Store struct{ Root string }

func New(root string) *Store { return &Store{Root: root} }
func (s *Store) open() (*sql.DB, error) {
	if err := os.MkdirAll(s.Root, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", filepath.Join(s.Root, "stats.sqlite"))
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS datasets (id TEXT PRIMARY KEY,region_code TEXT,level TEXT,year INTEGER,indicator TEXT,caliber TEXT,unit TEXT,official INTEGER,metadata TEXT,path TEXT);
 CREATE INDEX IF NOT EXISTS stats_lookup ON datasets(region_code,level,year,indicator,caliber,unit);
 CREATE TABLE IF NOT EXISTS observations (dataset_id TEXT,region_code TEXT,region_name TEXT,level TEXT,year INTEGER,indicator TEXT,caliber TEXT,unit TEXT,value REAL,PRIMARY KEY(dataset_id,region_code));`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
func (s *Store) Put(ctx context.Context, data Dataset) (Dataset, error) {
	if !data.Checks.OK {
		return data, fmt.Errorf("只接受通过检查的统计表")
	}
	db, err := s.open()
	if err != nil {
		return data, err
	}
	defer db.Close()
	identity, _ := json.Marshal([]any{data.Need.RegionCode, data.Need.Level, data.Need.Year, data.Need.Indicator, data.Need.Caliber, data.Need.Unit, data.Source.URL, data.Source.TableRef, data.Need.Exclude})
	sum := sha256.Sum256(identity)
	id := hex.EncodeToString(sum[:])
	data.Path = filepath.Join(s.Root, id+".csv")
	file, err := os.Create(data.Path)
	if err != nil {
		return data, err
	}
	writer := csv.NewWriter(file)
	err = writer.Write([]string{"region_code", "region_name", "level", "year", "indicator", "caliber", "unit", "value"})
	for _, o := range data.Observations {
		if err == nil {
			err = writer.Write([]string{o.Code, o.Name, o.Level, strconv.Itoa(o.Year), o.Indicator, o.Caliber, o.ValueUnit, strconv.FormatFloat(o.Value, 'f', -1, 64)})
		}
	}
	writer.Flush()
	if err == nil {
		err = writer.Error()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return data, err
	}
	metadata, err := json.Marshal(data)
	if err != nil {
		return data, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return data, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO datasets VALUES(?,?,?,?,?,?,?,?,?,?)`, id, data.Need.RegionCode, data.Need.Level, data.Need.Year, data.Need.Indicator, data.Need.Caliber, data.Need.Unit, data.Source.Official, string(metadata), data.Path)
	if err != nil {
		return data, err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM observations WHERE dataset_id=?", id); err != nil {
		return data, err
	}
	for _, o := range data.Observations {
		_, err = tx.ExecContext(ctx, `INSERT INTO observations VALUES(?,?,?,?,?,?,?,?,?)`, id, o.Code, o.Name, o.Level, o.Year, o.Indicator, o.Caliber, o.ValueUnit, o.Value)
		if err != nil {
			return data, err
		}
	}
	return data, tx.Commit()
}
func (s *Store) Lookup(ctx context.Context, need Need) (Dataset, bool, error) {
	if _, err := os.Stat(filepath.Join(s.Root, "stats.sqlite")); os.IsNotExist(err) {
		return Dataset{}, false, nil
	}
	db, err := s.open()
	if err != nil {
		return Dataset{}, false, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT metadata FROM datasets WHERE region_code=? AND level=? AND (?=0 OR year=?) AND indicator=? AND caliber=? AND unit=? ORDER BY year DESC,official DESC`, need.RegionCode, need.Level, need.Year, need.Year, need.Indicator, need.Caliber, need.Unit)
	if err != nil {
		return Dataset{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return Dataset{}, false, err
		}
		var data Dataset
		if err = json.Unmarshal([]byte(raw), &data); err != nil {
			return data, false, err
		}
		if strings.Join(data.Need.Exclude, "|") != strings.Join(need.Exclude, "|") {
			continue
		}
		if _, err = os.Stat(data.Path); err == nil {
			return data, true, nil
		}
	}
	return Dataset{}, false, rows.Err()
}
