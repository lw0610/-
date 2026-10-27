// Package osmdata provides deterministic OpenStreetMap acquisition without a model.
package osmdata

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Ref struct {
	Type string
	ID   int64
}

func ParseRef(s string) (Ref, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return Ref{}, fmt.Errorf("OSM 编号应为 R/W/N 加正整数")
	}
	types := map[byte]string{'R': "relation", 'W': "way", 'N': "node"}
	typ := types[strings.ToUpper(s)[0]]
	id, e := strconv.ParseInt(s[1:], 10, 64)
	if e != nil || id <= 0 || typ == "" {
		return Ref{}, fmt.Errorf("OSM 编号无效: %s", s)
	}
	return Ref{typ, id}, nil
}
func (r Ref) String() string {
	if r.Type == "" {
		return "?"
	}
	return strings.ToUpper(r.Type[:1]) + strconv.FormatInt(r.ID, 10)
}
func quote(s string) string { return strconv.Quote(s) }

var bracketTag = regexp.MustCompile(`^(\[\s*"(?:[^"\\]|\\.)+"\s*(?:[!~]?=|~)?\s*(?:"(?:[^"\\]|\\.)*"\s*(?:,\s*i\s*)?)?\])+$`)

func TagClause(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("标签不能为空")
	}
	if strings.HasPrefix(s, "[") {
		if !bracketTag.MatchString(s) {
			return "", fmt.Errorf("标签括号表达式无效")
		}
		return s, nil
	}
	for _, op := range []string{"=", "~"} {
		if i := strings.Index(s, op); i > 0 {
			return "[" + quote(strings.TrimSpace(s[:i])) + op + quote(strings.TrimSpace(s[i+1:])) + "]", nil
		}
	}
	return "[" + quote(s) + "]", nil
}
func TagKey(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, `["`) {
		var key string
		for i := 2; i < len(s); i++ {
			if s[i] == '"' && s[i-1] != '\\' {
				key, _ = strconv.Unquote(s[1 : i+1])
				return key
			}
		}
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '=' || r == '~' })
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}
func Types(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return []string{"nwr"}, nil
	}
	seen := map[string]bool{}
	var result []string
	for _, s := range raw {
		for _, v := range strings.Split(s, ",") {
			v = strings.ToLower(strings.TrimSpace(v))
			if v != "node" && v != "way" && v != "relation" && v != "nwr" {
				return nil, fmt.Errorf("要素类型无效: %s", v)
			}
			if !seen[v] {
				result = append(result, v)
				seen[v] = true
			}
		}
	}
	return result, nil
}
func BuildQuery(ext Extent, tags, types []string, timeout int, mode string) (string, error) {
	if len(tags) == 0 {
		return "", fmt.Errorf("至少指定一个标签")
	}
	var lines []string
	lines = append(lines, fmt.Sprintf("[out:json][timeout:%d];", timeout), ext.Prelude, "(")
	queryBytes := len(strings.Join(lines, "\n")) + len(mode) + 4
	for _, filter := range ext.Filters {
		for _, typ := range types {
			for _, tag := range tags {
				clause, e := TagClause(tag)
				if e != nil {
					return "", e
				}
				line := typ + clause + filter + ";"
				queryBytes += len(line) + 1
				if queryBytes > 1<<20 {
					return "", fmt.Errorf("OSM 查询超过 1 MiB，请减少标签或显式简化查询边界并保留原边界用于精裁；未改用外接矩形")
				}
				lines = append(lines, line)
			}
		}
	}
	lines = append(lines, ");", mode)
	return strings.Join(lines, "\n"), nil
}
func NameQuery(query, category string, timeout int) (string, error) {
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("查询名称不能为空")
	}
	cat := ""
	var e error
	if category != "" {
		cat, e = TagClause(category)
		if e != nil {
			return "", e
		}
	}
	flag := ""
	if regexp.MustCompile(`[a-zA-Z]`).MatchString(query) {
		flag = ",i"
	}
	var lines = []string{fmt.Sprintf("[out:json][timeout:%d];", timeout)}
	for _, v := range []struct {
		typ string
		n   int
	}{{"rel", 15}, {"way", 15}, {"node", 10}} {
		lines = append(lines, v.typ+`["name"~`+quote(regexp.QuoteMeta(query))+flag+"]"+cat+";", fmt.Sprintf("out tags center %d;", v.n))
	}
	return strings.Join(lines, "\n"), nil
}
func ObjectsQuery(refs []Ref, timeout int) string {
	var parts []string
	for _, r := range refs {
		parts = append(parts, fmt.Sprintf("%s(%d);", r.Type, r.ID))
	}
	return fmt.Sprintf("[out:json][timeout:%d];(%s);out geom;", timeout, strings.Join(parts, ""))
}

func SiteQuery(name string, timeout int) string {
	return fmt.Sprintf("[out:json][timeout:%d];rel[\"name\"~%s][\"type\"=\"site\"];out body center 20;", timeout, quote(regexp.QuoteMeta(name)))
}
