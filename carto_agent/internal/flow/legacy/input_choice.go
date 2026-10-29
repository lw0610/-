package legacy

import (
	"fmt"
	"strings"
)

// InputChoice decodes source selections stored by the v4 input form. It is only used during migration.
type InputChoice struct {
	Mode, Path, Source string
	Sources, Files     map[string]string
}

func ParseInputChoice(value any) (InputChoice, bool, error) {
	choice := InputChoice{Sources: map[string]string{}, Files: map[string]string{}}
	object, ok := value.(map[string]any)
	if !ok {
		return choice, false, nil
	}
	_, hasAcquire := object["$acquire"]
	_, hasFill := object["$fill"]
	if !hasAcquire && !hasFill {
		return choice, false, nil
	}
	fail := func(message string) (InputChoice, bool, error) { return choice, true, fmt.Errorf("%s", message) }
	acquire, aok := object["$acquire"].(bool)
	fill, fok := object["$fill"].(bool)
	if hasAcquire && (!aok || !acquire) || hasFill && (!fok || !fill) || acquire == fill {
		return fail("自动获取和补齐必须只选择一种，标记值必须为 true")
	}
	choice.Mode = "auto"
	if fill {
		choice.Mode = "fill"
	}
	for key, value := range object {
		switch key {
		case "$acquire", "$fill":
		case "path", "source":
			text, valid := value.(string)
			if !valid || strings.ContainsAny(text, "\x00\r\n") {
				return fail(key + " 必须为有效文字")
			}
			if key == "path" {
				choice.Path = strings.TrimSpace(text)
			} else {
				choice.Source = strings.TrimSpace(text)
			}
		case "sources", "files":
			out := choice.Sources
			if key == "files" {
				out = choice.Files
			}
			switch values := value.(type) {
			case map[string]string:
				for k, v := range values {
					out[k] = v
				}
			case map[string]any:
				for k, v := range values {
					str, valid := v.(string)
					if !valid {
						return fail(key + " 的每个值必须为文字")
					}
					out[k] = str
				}
			default:
				return fail(key + " 必须为键值对象")
			}
			for k, v := range out {
				if strings.TrimSpace(k) == "" || strings.ContainsAny(k+v, "\x00\r\n") {
					return fail(key + " 包含无效内容")
				}
			}
		default:
			return fail("来源选择含未知字段：" + key)
		}
	}
	if fill && choice.Path == "" {
		return fail("补齐缺项需要选择文件夹")
	}
	if acquire && choice.Path != "" {
		return fail("自动获取不能同时指定文件夹路径，请使用补齐缺项")
	}
	return choice, true, nil
}
func (c InputChoice) Value() map[string]any {
	value := map[string]any{}
	if c.Mode == "fill" {
		value["$fill"] = true
		value["path"] = c.Path
	} else {
		value["$acquire"] = true
	}
	if c.Source != "" {
		value["source"] = c.Source
	}
	if len(c.Sources) > 0 {
		sources := map[string]any{}
		for k, v := range c.Sources {
			sources[k] = v
		}
		value["sources"] = sources
	}
	if len(c.Files) > 0 {
		files := map[string]any{}
		for k, v := range c.Files {
			files[k] = v
		}
		value["files"] = files
	}
	return value
}
