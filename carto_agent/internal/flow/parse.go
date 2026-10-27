package flow

import (
	"fmt"
	"github.com/autocarto/carto_agent/internal/resource"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"gopkg.in/yaml.v3"
)

var (
	sectionAliases = map[string]string{
		"适用场景": "applicability", "什么时候使用": "applicability", "触发条件": "applicability", "适用范围": "applicability",
		"需要准备": "inputs", "输入": "inputs", "需要用户提供": "inputs", "准备内容": "inputs",
		"使用的资源": "resources", "资源": "resources", "引用资源": "resources",
		"参与角色": "roles", "角色分工": "roles", "需要的角色": "roles",
		"数据需求": "requirements", "数据来源": "requirements", "数据获取": "requirements",
		"步骤": "steps", "流程步骤": "steps", "操作步骤": "steps",
		"必须遵守": "rules", "注意事项": "rules", "禁止事项": "rules", "通用规则": "rules", "约束": "rules",
		"交付": "deliverables", "交付成果": "deliverables", "输出": "deliverables", "成果": "deliverables",
	}
	fieldAliases = map[string]string{
		"负责": "role", "执行者": "role", "负责角色": "role", "角色": "role",
		"能力": "capabilities", "需要能力": "capabilities",
		"条件": "condition", "执行条件": "condition", "何时执行": "condition", "仅当": "condition",
		"做法": "instructions", "操作": "instructions", "要求": "instructions", "内容": "instructions", "具体做法": "instructions",
		"完成标准": "completion", "完成条件": "completion", "产出": "completion", "验收": "completion",
		"失败时": "failure", "失败处理": "failure", "出错时": "failure",
	}
	stepPrefix         = regexp.MustCompile(`^(?:第\s*[0-9]+\s*步\s*[:：.、]?|[0-9]+[.、]\s*|步骤\s*[0-9]+\s*[:：])\s*`)
	listMarker         = regexp.MustCompile(`^([ \t]*)(?:[-+*]|[0-9]+[.)])[ \t]+(.*)$`)
	inputPattern       = regexp.MustCompile(`(?s)^([^（(:：]+?)\s*(?:[（(]([^）)]*)[）)])?\s*(?:[:：]\s*(.*))?$`)
	resourcePattern    = regexp.MustCompile(`^([^「]+)「([^」]+)」\s*[:：]\s*(.*)$`)
	folderInputPattern = regexp.MustCompile(`[（(]文件所在文件夹[：:]\s*「([^」]+)」[）)]\s*$`)
	attributeSplit     = regexp.MustCompile(`[;；,，]`)
	quotedNames        = regexp.MustCompile(`「([^」]+)」`)
	alternative        = regexp.MustCompile(`^与\s*「([^」]+)」\s*二选一$`)
	// 显式类型属性优先于说明中的关键词，也不允许被模型改写。
	inputTypeAttributes = map[string]string{"文件": "file", "文件夹": "folder", "图层": "layer", "对话原文": "conversation_text", "文本": "text", "数字": "number"}
)

// Parse 把 FLOW.md 解析为可检查的文档结构。文档错误收集在 Diagnostics，
// 不读取文件、不绑定方案、不调用模型；相同原文始终产生相同结果。
func Parse(raw string) *FlowDoc {
	raw = normalizeMarkdown(raw)
	meta, body, offset, diagnostics := parseFrontmatter(raw)
	doc := &FlowDoc{
		RoleSlots: []RoleSlotDoc{}, Requirements: []RequirementDoc{}, Meta: meta, Inputs: []InputDoc{}, Resources: []ResourceDoc{}, Steps: []StepDoc{},
		Rules: []string{}, Notes: []DocSection{}, Diagnostics: diagnostics,
	}
	seen := map[string]bool{}
	for _, section := range headings(body, 2) {
		kind := sectionAliases[normalizeName(section.title)]
		line := offset + section.line
		if kind == "" {
			doc.Notes = append(doc.Notes, DocSection{Title: section.title, Content: section.content, Line: line})
			doc.Diagnostics = append(doc.Diagnostics, diagnostic("info", "FLOW_SECTION_UNKNOWN", "", line, "分节“"+section.title+"”保留为补充说明。"))
			continue
		}
		if seen[kind] {
			doc.Diagnostics = append(doc.Diagnostics, diagnostic("info", "FLOW_SECTION_DUPLICATE", "", line, "同类分节“"+section.title+"”已合并。"))
		}
		seen[kind] = true
		contentLine := offset + section.contentLine
		switch kind {
		case "applicability":
			doc.Applicability = joinMarkdown(doc.Applicability, section.content)
		case "inputs":
			parseInputs(doc, section.content, contentLine)
		case "resources":
			parseResources(doc, section.content, contentLine)
		case "roles":
			parseRoleSlots(doc, section.content, contentLine)
		case "requirements":
			parseRequirements(doc, section.content, contentLine)
		case "steps":
			for _, stepSection := range headings(section.content, 3) {
				index := len(doc.Steps) + 1
				stepLine := contentLine + stepSection.line - 1
				step := parseStep(doc, stepSection, index, stepLine)
				doc.Steps = append(doc.Steps, step)
			}
		case "rules":
			for _, item := range listItems(section.content) {
				doc.Rules = append(doc.Rules, item.content)
			}
		case "deliverables":
			doc.Deliverables = joinMarkdown(doc.Deliverables, section.content)
		}
	}
	if len(doc.Steps) == 0 {
		doc.Diagnostics = append(doc.Diagnostics, diagnostic("error", "FLOW_NO_STEPS", "", 0, "流程没有步骤，请在“## 步骤”下添加三级标题。"))
	}
	for _, r := range doc.Resources {
		if r.FolderInput != "" {
			valid := r.Type == "data_list"
			found := false
			for _, input := range doc.Inputs {
				if input.Label == r.FolderInput {
					typ, _ := deterministicInputType(input)
					found = typ == "folder"
					break
				}
			}
			if !valid || !found {
				doc.Diagnostics = append(doc.Diagnostics, diagnostic("error", "FLOW_DATA_INPUT_INVALID", "", r.Line, "数据清单关联的输入必须存在且为文件夹："+r.FolderInput))
			}
		}
	}
	validateRoleSlotUses(doc, seen["roles"])
	resolveReferences(doc)
	return doc
}

// ParseMetadata 读取头部信息，供存储层确定流程编号；不执行正文校验。
func ParseMetadata(raw string) (FlowMetadata, []Diagnostic) {
	meta, _, _, diagnostics := parseFrontmatter(normalizeMarkdown(raw))
	return meta, diagnostics
}

func normalizeMarkdown(raw string) string {
	return strings.ReplaceAll(strings.TrimPrefix(raw, "\ufeff"), "\r\n", "\n")
}

func normalizeName(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

func diagnostic(level, code, step string, line int, message string) Diagnostic {
	return Diagnostic{Level: level, Code: code, Step: step, Line: line, Message: message}
}

func parseFrontmatter(raw string) (FlowMetadata, string, int, []Diagnostic) {
	meta := FlowMetadata{Version: "1.0.0"}
	diagnostics := []Diagnostic{}
	body, offset := raw, 0
	lines := strings.SplitAfter(raw, "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		end := -1
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				end = i
				break
			}
		}
		if end < 0 {
			diagnostics = append(diagnostics, diagnostic("error", "FLOW_PARSE_FAILED", "", 1, "头部信息缺少结束标记 ---。"))
		} else {
			body, offset = strings.Join(lines[end+1:], ""), end+1
			var root yaml.Node
			if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "")), &root); err != nil {
				diagnostics = append(diagnostics, diagnostic("error", "FLOW_PARSE_FAILED", "", 2, "头部 YAML 无法解析："+err.Error()))
			} else if len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
				diagnostics = append(diagnostics, diagnostic("error", "FLOW_PARSE_FAILED", "", 2, "头部 YAML 必须是键值列表。"))
			} else {
				values, seen := map[string]string{}, map[string]bool{}
				mapping := root.Content[0]
				for i := 0; i+1 < len(mapping.Content); i += 2 {
					key, value := mapping.Content[i], mapping.Content[i+1]
					canonical := metadataKey(key.Value)
					if canonical == "" {
						continue
					}
					if seen[canonical] {
						diagnostics = append(diagnostics, diagnostic("error", "FLOW_PARSE_FAILED", "", key.Line+1, "头部字段“"+key.Value+"”重复声明。"))
						continue
					}
					seen[canonical] = true
					if value.Kind != yaml.ScalarNode || value.Tag == "!!null" {
						if value.Tag != "!!null" {
							diagnostics = append(diagnostics, diagnostic("error", "FLOW_PARSE_FAILED", "", value.Line+1, "头部字段“"+key.Value+"”必须是文字。"))
						}
						continue
					}
					values[canonical] = strings.TrimSpace(value.Value)
				}
				meta.ID, meta.Name, meta.Description, meta.Author = values["id"], values["name"], values["description"], values["author"]
				if values["version"] != "" {
					meta.Version = values["version"]
				}
			}
		}
	}
	if meta.Name == "" {
		diagnostics = append(diagnostics, diagnostic("error", "FLOW_NO_NAME", "", 1, "流程缺少名称，请在头部填写“名称”或 name。"))
	}
	return meta, body, offset, diagnostics
}

func metadataKey(key string) string {
	switch normalizeName(key) {
	case "名称", "name":
		return "name"
	case "编号", "id":
		return "id"
	case "说明", "description":
		return "description"
	case "版本", "version":
		return "version"
	case "作者", "author":
		return "author"
	}
	return ""
}

type markdownSection struct {
	title, content    string
	line, contentLine int
}

func markdownAST(raw string) ast.Node {
	return goldmark.New(
		goldmark.WithExtensions(extension.Table),
		goldmark.WithParserOptions(parser.WithBlockParsers(util.Prioritized(&sourceHeadingParser{BlockParser: parser.NewATXHeadingParser()}, 599))),
	).Parser().Parse(text.NewReader([]byte(raw)))
}

const headingOffsetAttribute = "flow-source-start"

// 空 ATX 标题没有 Lines 片段，因此在标准解析器识别标题时补记位置。
// 其余解析行为仍由 goldmark 的标准 ATX 解析器负责。
type sourceHeadingParser struct{ parser.BlockParser }

func (p *sourceHeadingParser) Open(parent ast.Node, reader text.Reader, context parser.Context) (ast.Node, parser.State) {
	_, segment := reader.PeekLine()
	node, state := p.BlockParser.Open(parent, reader, context)
	if node != nil {
		node.SetAttributeString(headingOffsetAttribute, segment.Start)
	}
	return node, state
}

// headings 只检查文档顶层 AST，围栏、引用和嵌套列表里的伪标题不会划分结构。
func headings(raw string, level int) []markdownSection {
	source := []byte(raw)
	type marker struct {
		title      string
		start, end int
	}
	markers := []marker{}
	root := markdownAST(raw)
	for node := root.FirstChild(); node != nil; node = node.NextSibling() {
		heading, ok := node.(*ast.Heading)
		if !ok || heading.Level != level {
			continue
		}
		start := lineStart(raw, nodeStart(node, len(raw)))
		offset, atx := node.AttributeString(headingOffsetAttribute)
		if atx {
			start = lineStart(raw, offset.(int))
		}
		end := lineEnd(raw, start)
		// Setext 二级标题还有一行下划线；正文从下划线之后开始。
		if !atx {
			if lines := node.Lines(); lines.Len() > 0 {
				end = lineEnd(raw, lines.At(lines.Len()-1).Start)
			}
			end = lineEnd(raw, end)
		}
		markers = append(markers, marker{title: strings.TrimSpace(inlineText(heading, source)), start: start, end: end})
	}
	sections := make([]markdownSection, 0, len(markers))
	for i, marker := range markers {
		end := len(raw)
		if i+1 < len(markers) {
			end = markers[i+1].start
		}
		sections = append(sections, markdownSection{title: marker.title, content: strings.TrimSpace(raw[marker.end:end]), line: lineNumber(raw, marker.start), contentLine: lineNumber(raw, marker.end) + leadingBlankLines(raw[marker.end:end])})
	}
	return sections
}

func inlineText(node ast.Node, source []byte) string {
	var result strings.Builder
	_ = ast.Walk(node, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch value := child.(type) {
		case *ast.Text:
			result.Write(value.Segment.Value(source))
			if value.SoftLineBreak() {
				result.WriteByte(' ')
			}
		case *ast.String:
			result.Write(value.Value)
		}
		return ast.WalkContinue, nil
	})
	return result.String()
}

func nodeStart(node ast.Node, fallback int) int {
	start := fallback
	if node.Type() == ast.TypeBlock {
		for i := 0; i < node.Lines().Len(); i++ {
			if pos := node.Lines().At(i).Start; pos < start {
				start = pos
			}
		}
	}
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if pos := nodeStart(child, fallback); pos < start {
			start = pos
		}
	}
	return start
}

func lineStart(raw string, pos int) int {
	if pos <= 0 {
		return 0
	}
	if pos > len(raw) {
		pos = len(raw)
	}
	return strings.LastIndex(raw[:pos], "\n") + 1
}

func lineEnd(raw string, start int) int {
	if start >= len(raw) {
		return len(raw)
	}
	if end := strings.IndexByte(raw[start:], '\n'); end >= 0 {
		return start + end + 1
	}
	return len(raw)
}

func lineNumber(raw string, pos int) int { return strings.Count(raw[:pos], "\n") + 1 }

func leadingBlankLines(raw string) int {
	count := 0
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) != "" {
			break
		}
		count++
	}
	return count
}

type markdownItem struct {
	content     string
	start, line int
}

func listItems(raw string) []markdownItem {
	root := markdownAST(raw)
	items := []markdownItem{}
	for node := root.FirstChild(); node != nil; node = node.NextSibling() {
		if _, ok := node.(*ast.List); !ok {
			continue
		}
		for item := node.FirstChild(); item != nil; item = item.NextSibling() {
			start := lineStart(raw, nodeStart(item, len(raw)))
			firstEnd := lineEnd(raw, start)
			match := listMarker.FindStringSubmatch(strings.TrimRight(raw[start:firstEnd], "\n"))
			if match == nil {
				continue
			}
			end := len(raw)
			if next := item.NextSibling(); next != nil {
				end = lineStart(raw, nodeStart(next, len(raw)))
			} else if next := node.NextSibling(); next != nil {
				end = lineStart(raw, nodeStart(next, len(raw)))
			}
			// 续行只去掉列表容器缩进，代码块和表格本身的 Markdown 保留。
			content := strings.TrimSpace(match[2] + "\n" + deindent(raw[firstEnd:end], markerWidth(raw[start:firstEnd])))
			items = append(items, markdownItem{content: content, start: start, line: lineNumber(raw, start)})
		}
	}
	return items
}

func markerWidth(line string) int {
	match := listMarker.FindStringSubmatchIndex(strings.TrimRight(line, "\n"))
	if match == nil {
		return 0
	}
	return match[4]
}

func deindent(raw string, width int) string {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		removed := 0
		for removed < len(line) && removed < width && line[removed] == ' ' {
			removed++
		}
		lines[i] = line[removed:]
	}
	return strings.Join(lines, "\n")
}

func joinMarkdown(left, right string) string {
	if strings.TrimSpace(left) == "" {
		return strings.TrimSpace(right)
	}
	if strings.TrimSpace(right) == "" {
		return strings.TrimSpace(left)
	}
	return strings.TrimSpace(left) + "\n\n" + strings.TrimSpace(right)
}

func parseInputs(doc *FlowDoc, raw string, baseLine int) {
	for _, item := range listItems(raw) {
		match := inputPattern.FindStringSubmatch(item.content)
		if match == nil {
			doc.Diagnostics = append(doc.Diagnostics, diagnostic("error", "FLOW_PARSE_FAILED", "", baseLine+item.line-1, "输入声明无法识别，请使用“名称（属性）：说明”。"))
			continue
		}
		input := InputDoc{Label: strings.TrimSpace(match[1]), Description: strings.TrimSpace(match[3]), Options: []string{}, Alternatives: []string{}, Attributes: []string{}, UnknownAttributes: []string{}, Line: baseLine + item.line - 1}
		for _, attr := range attributeSplit.Split(match[2], -1) {
			attr = strings.TrimSpace(attr)
			if attr == "" {
				continue
			}
			input.Attributes = append(input.Attributes, attr)
			switch {
			case attr == "必填":
				input.Required = true
			case attr == "可选":
				input.Required = false
			case attr == "是/否":
				input.Boolean = true
			case inputTypeAttributes[attr] != "":
				input.Type = inputTypeAttributes[attr]
			case attr == "不记住":
				input.NoRemember = true
			case strings.HasPrefix(attr, "默认"):
				input.Default = strings.TrimSpace(strings.TrimLeft(strings.TrimPrefix(attr, "默认"), ":："))
			case strings.HasPrefix(attr, "可选值：") || strings.HasPrefix(attr, "可选值:"):
				options := strings.TrimLeft(strings.TrimPrefix(attr, "可选值"), ":：")
				for _, option := range strings.Split(strings.TrimSpace(options), "、") {
					if option = strings.TrimSpace(option); option != "" {
						input.Options = append(input.Options, option)
					}
				}
			case alternative.MatchString(attr):
				input.Alternatives = append(input.Alternatives, strings.TrimSpace(alternative.FindStringSubmatch(attr)[1]))
			default:
				input.UnknownAttributes = append(input.UnknownAttributes, attr)
			}
		}
		// 输入名称就是输入键，同名输入会让表单值、条件和运行记录无法区分。
		for _, existing := range doc.Inputs {
			if existing.Label == input.Label {
				doc.Diagnostics = append(doc.Diagnostics, diagnostic("error", "FLOW_INPUT_DUPLICATE", "", input.Line, "输入名称“"+input.Label+"”重复，请为每个输入使用不同的名称。"))
				break
			}
		}
		doc.Inputs = append(doc.Inputs, input)
	}
}

func parseResources(doc *FlowDoc, raw string, baseLine int) {
	for _, item := range listItems(raw) {
		match := resourcePattern.FindStringSubmatch(item.content)
		line := baseLine + item.line - 1
		if match == nil {
			doc.Diagnostics = append(doc.Diagnostics, diagnostic("error", "FLOW_PARSE_FAILED", "", line, "资源声明无法识别，请使用“类型「名称」：位置”。"))
			continue
		}
		location := strings.TrimSpace(match[3])
		folderInput := ""
		if attribute := folderInputPattern.FindStringSubmatch(location); attribute != nil {
			folderInput = strings.TrimSpace(attribute[1])
			location = strings.TrimSpace(folderInputPattern.ReplaceAllString(location, ""))
		}
		typeName := resourceType(strings.TrimSpace(match[1]), location)
		if typeName == "" {
			doc.Diagnostics = append(doc.Diagnostics, diagnostic("error", "FLOW_PARSE_FAILED", "", line, "无法识别资源类型“"+strings.TrimSpace(match[1])+"”。"))
		}
		doc.Resources = append(doc.Resources, ResourceDoc{Label: strings.TrimSpace(match[2]), Type: typeName, Location: location, FolderInput: folderInput, Line: line})
	}
}

func resourceType(raw, location string) string {
	typ, _ := resource.TypeForWord(raw)
	return typ
}

func parseStep(doc *FlowDoc, section markdownSection, index, line int) StepDoc {
	step := StepDoc{ID: fmt.Sprintf("s%d", index), Index: index, Title: strings.TrimSpace(stepPrefix.ReplaceAllString(section.title, "")), ExpectedOutputs: []OutputDeclaration{}, Refs: DocRefs{Resources: []string{}, Inputs: []string{}, Outputs: []OutputRef{}, Unresolved: []string{}}, OnFailure: "retry_then_ask", Line: line}
	type field struct {
		kind, first       string
		start, end, width int
	}
	fields := []field{}
	for _, item := range listItems(section.content) {
		firstEnd := lineEnd(section.content, item.start)
		first := listMarker.FindStringSubmatch(strings.TrimRight(section.content[item.start:firstEnd], "\n"))
		if first == nil {
			continue
		}
		label, value, found := splitField(first[2])
		kind := fieldAliases[normalizeName(label)]
		if !found || kind == "" {
			continue
		}
		fields = append(fields, field{kind: kind, first: value, start: item.start, end: firstEnd, width: markerWidth(section.content[item.start:firstEnd])})
	}
	values := map[string]string{}
	if len(fields) == 0 {
		values["instructions"] = strings.TrimSpace(section.content)
	} else {
		values["instructions"] = strings.TrimSpace(section.content[:fields[0].start])
		for i, field := range fields {
			end := len(section.content)
			if i+1 < len(fields) {
				end = fields[i+1].start
			}
			value := strings.TrimSpace(field.first + "\n" + deindent(section.content[field.end:end], field.width))
			values[field.kind] = joinMarkdown(values[field.kind], value)
		}
	}
	step.Role, step.Instructions, step.Completion = values["role"], values["instructions"], values["completion"]
	step.Capabilities, step.OptionalCapabilities = parseStepCapabilities(values["capabilities"])
	if raw := values["condition"]; raw != "" {
		step.Condition = &ConditionDoc{Text: raw, Rule: ParseCondition(raw)}
		if step.Condition.Rule == nil {
			doc.Diagnostics = append(doc.Diagnostics, diagnostic("info", "FLOW_CONDITION_UNPARSED", step.ID, line, "本步条件保留原文，需在编译时继续转换。"))
		}
	}
	if failure := values["failure"]; failure != "" {
		switch strings.TrimSpace(failure) {
		case "停止流程":
			step.OnFailure = "stop"
		case "跳过本步":
			step.OnFailure = "skip"
		case "询问用户":
			step.OnFailure = "ask"
		case "重试一次后询问用户":
		default:
			doc.Diagnostics = append(doc.Diagnostics, diagnostic("error", "FLOW_PARSE_FAILED", step.ID, line, "失败处理“"+failure+"”无法识别，请使用规范列出的四种写法。"))
		}
	}
	if step.Instructions == "" {
		doc.Diagnostics = append(doc.Diagnostics, diagnostic("error", "FLOW_STEP_NO_INSTRUCTIONS", step.ID, line, "本步没有做法。"))
	}
	if step.Role == "" {
		d := diagnostic("info", "FLOW_STEP_NO_ROLE", step.ID, line, "本步未指定负责角色，将由系统按能力挑选。")
		d.Fix = map[string]any{"action": "set_role", "step": step.ID}
		doc.Diagnostics = append(doc.Diagnostics, d)
	}
	if step.Completion == "" {
		doc.Diagnostics = append(doc.Diagnostics, diagnostic("warning", "FLOW_STEP_NO_COMPLETION", step.ID, line, "本步没有完成标准，运行时只检查回执状态。"))
	}
	step.ExpectedOutputs = parseOutputs(doc, step)
	return step
}

func splitField(raw string) (string, string, bool) {
	index := strings.IndexAny(raw, ":：")
	if index < 0 {
		return "", "", false
	}
	label := strings.TrimSpace(raw[:index])
	for _, marker := range []string{"**", "__"} {
		if len(label) >= 2*len(marker) && strings.HasPrefix(label, marker) && strings.HasSuffix(label, marker) {
			label = strings.TrimSpace(label[len(marker) : len(label)-len(marker)])
		}
	}
	width := 1
	if strings.HasPrefix(raw[index:], "：") {
		width = len("：")
	}
	return label, strings.TrimSpace(raw[index+width:]), true
}

func parseOutputs(doc *FlowDoc, step StepDoc) []OutputDeclaration {
	outputs := []OutputDeclaration{}
	raw := step.Completion
	for _, quoted := range quotedNames.FindAllStringSubmatchIndex(raw, -1) {
		tail := strings.TrimLeft(raw[quoted[1]:], " \t")
		if tail == "" || (tail[0] != '(' && !strings.HasPrefix(tail, "（")) {
			continue
		}
		attrs, consumed := parenthesized(tail)
		if consumed == 0 {
			continue
		}
		parts := attributeSplit.Split(attrs, -1)
		output := OutputDeclaration{Label: strings.TrimSpace(raw[quoted[2]:quoted[3]]), Required: true}
		format := ""
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "可选" {
				output.Required = false
				continue
			}
			if output.Type == "" {
				output.Type = part
			} else if format == "" {
				format = part
			}
		}
		switch output.Type {
		case "图层":
			output.Type = "layers"
		case "数据集":
			output.Type = "dataset"
			switch format {
			case "路线格式":
				format = "route_dataset"
			case "统计格式":
				format = "statistical_dataset"
			}
		case "文件":
			output.Type = "file"
			format = strings.TrimPrefix(strings.ToLower(format), ".")
		case "版面":
			output.Type = "layout"
		case "文本":
			output.Type = "text"
		case "审查结果", "审查结果（初审）", "审查结果(初审)", "审查结果（终审）", "审查结果(终审)":
			if format == "初审" || strings.Contains(output.Type, "初审") {
				output.Type, format = "review_initial", ""
			} else if format == "终审" || strings.Contains(output.Type, "终审") {
				output.Type, format = "review_final", ""
			} else {
				output.Type = ""
			}
		case "确认结果":
			output.Type = "confirmation"
		case "其他":
			output.Type = "any"
		default:
			output.Type = ""
		}
		output.Format = format
		description := tail[consumed:]
		if end := strings.IndexByte(description, '\n'); end >= 0 {
			description = description[:end]
		}
		output.Description = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(description), ":："))
		if output.Type == "" {
			doc.Diagnostics = append(doc.Diagnostics, diagnostic("error", "FLOW_PARSE_FAILED", step.ID, step.Line, "产出“"+output.Label+"”的类型无法识别。"))
		}
		outputs = append(outputs, output)
	}
	return outputs
}

// parenthesized 接受两种括号及嵌套括号，避免把“审查结果（初审）”截断。
func parenthesized(raw string) (string, int) {
	depth, contentStart := 0, 0
	for pos, value := range raw {
		switch value {
		case '(', '（':
			depth++
			if depth == 1 {
				contentStart = pos + len(string(value))
			}
		case ')', '）':
			depth--
			if depth == 0 {
				return raw[contentStart:pos], pos + len(string(value))
			}
		}
	}
	return "", 0
}

func resolveReferences(doc *FlowDoc) {
	resources, inputs := map[string]bool{}, map[string]bool{}
	for _, resource := range doc.Resources {
		resources[resource.Label] = true
	}
	for _, input := range doc.Inputs {
		inputs[input.Label] = true
	}
	allOutputs, previous := map[string]int{}, map[string]OutputRef{}
	for i, step := range doc.Steps {
		for _, output := range step.ExpectedOutputs {
			if _, exists := allOutputs[output.Label]; exists {
				doc.Diagnostics = append(doc.Diagnostics, diagnostic("error", "FLOW_OUTPUT_DUPLICATE", step.ID, step.Line, "产出名称“"+output.Label+"”重复，请为每项产出使用唯一名称。"))
			} else {
				allOutputs[output.Label] = i
			}
		}
	}
	for i := range doc.Steps {
		step := &doc.Steps[i]
		own := map[string]bool{}
		for _, output := range step.ExpectedOutputs {
			own[output.Label] = true
		}
		seen := map[string]bool{}
		raw := step.Instructions + "\n" + step.Completion
		if step.Condition != nil {
			raw = step.Condition.Text + "\n" + raw
		}
		for _, match := range quotedNames.FindAllStringSubmatch(raw, -1) {
			name := strings.TrimSpace(match[1])
			if name == "数据来源说明" {
				continue
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			matched := false
			if resources[name] {
				step.Refs.Resources = append(step.Refs.Resources, name)
				matched = true
			}
			if inputs[name] {
				step.Refs.Inputs = append(step.Refs.Inputs, name)
				matched = true
			}
			if output, exists := previous[name]; exists {
				step.Refs.Outputs = append(step.Refs.Outputs, output)
				matched = true
			}
			if matched || own[name] {
				continue
			}
			step.Refs.Unresolved = append(step.Refs.Unresolved, name)
			if index, exists := allOutputs[name]; exists && index > i {
				doc.Diagnostics = append(doc.Diagnostics, diagnostic("error", "FLOW_REF_FORWARD", step.ID, step.Line, "引用“"+name+"”由后续步骤产出，请调整步骤顺序。"))
			} else {
				doc.Diagnostics = append(doc.Diagnostics, diagnostic("warning", "FLOW_REF_UNRESOLVED", step.ID, step.Line, "引用“"+name+"”未匹配到输入、资源或前序产出。"))
			}
		}
		for _, output := range step.ExpectedOutputs {
			if _, exists := previous[output.Label]; !exists {
				previous[output.Label] = OutputRef{Step: step.ID, Label: output.Label}
			}
		}
	}
}
