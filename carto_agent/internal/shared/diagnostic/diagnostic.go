// Package diagnostic 定义解析、资源校验和编译共用的诊断结构。
package diagnostic

type Diagnostic struct {
	Level   string         `json:"level"`
	Code    string         `json:"code"`
	Step    string         `json:"step,omitempty"`
	Line    int            `json:"line,omitempty"`
	Message string         `json:"message"`
	Fix     map[string]any `json:"fix,omitempty"`
}
