package flow

import (
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	"regexp"
	"strings"
)

// Legacy structure detection is used by conversion only, never by runtime reads.
var legacyStepField = regexp.MustCompile(`(?m)^\s*[-*]\s*(?:\*\*)?(?:做法|负责|需要能力|使用能力|产出|完成标准|失败时)(?:\*\*)?[：:]`)

func isGuide(raw string) bool {
	sections := map[string]string{}
	for _, s := range headings(raw, 2) {
		sections[s.title] = s.content
	}
	// Headings alone are valid free prose. Require the old paired data/layer
	// tables and method section before interpreting an unmarked old document.
	return sections["制作要点"] != "" && len(guideTable(sections["数据"], 0)) > 0 && len(guideTable(sections["图层处理"], 0)) > 0
}

// guideTable is only used while converting old structured documents.
type guideTableRow struct {
	values map[string]string
	line   int
}

func guideTable(raw string, base int) []guideTableRow {
	var rows []guideTableRow
	source := []byte(raw)
	_ = ast.Walk(markdownAST(raw), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		table, ok := n.(*extast.Table)
		if !ok {
			return ast.WalkContinue, nil
		}
		var headers []string
		for row := table.FirstChild(); row != nil; row = row.NextSibling() {
			var cells []string
			for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
				cells = append(cells, strings.ReplaceAll(strings.TrimSpace(inlineText(cell, source)), `\|`, "|"))
			}
			if _, ok := row.(*extast.TableHeader); ok {
				headers = cells
				continue
			}
			values := map[string]string{}
			for i, cell := range cells {
				if i < len(headers) {
					values[headers[i]] = cell
				}
			}
			rows = append(rows, guideTableRow{values: values, line: base + lineNumber(raw, nodeStart(row, len(raw))) - 1})
		}
		return ast.WalkSkipChildren, nil
	})
	return rows
}
