package dataservice

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const MaxArgumentAttempts = 3

type ArgumentError struct {
	Tool      string `json:"tool"`
	Code      string `json:"code"`
	Path      string `json:"path"`
	Message   string `json:"message"`
	Expected  any    `json:"example"`
	Attempts  int    `json:"attempts"`
	Remaining int    `json:"remaining"`
}

func (e *ArgumentError) Error() string         { raw, _ := json.Marshal(e); return string(raw) }
func (e *ArgumentError) FatalToolError() bool  { return e.Remaining == 0 }
func (e *ArgumentError) ToolErrorCode() string { return e.Code }
func (e *ArgumentError) ToolRetryable() bool   { return e.Remaining > 0 }
func (e *ArgumentError) ToolProviderDiagnostics() map[string]any {
	return map[string]any{"path": e.Path, "example": e.Expected, "remaining": e.Remaining}
}

func (s *Service) decodeArguments(name string, raw []byte, target any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	err := dec.Decode(target)
	if err == nil {
		var extra any
		if e := dec.Decode(&extra); e != io.EOF {
			err = fmt.Errorf("只能提交一个参数对象")
		}
	}
	if err == nil {
		if r, ok := target.(*FindRequest); ok {
			if strings.TrimSpace(r.Need.What.Category) == "" && strings.TrimSpace(r.Need.What.Description) == "" {
				err = fmt.Errorf("need.what 必须包含非空的 category 或 description，允许同时填写；同时填写时按 category 确定类别，description 保留为补充说明")
			}
		}
	}
	if err == nil {
		s.argumentMu.Lock()
		delete(s.argumentFailures, name)
		s.argumentMu.Unlock()
		return nil
	}
	path := "arguments"
	var typed *json.UnmarshalTypeError
	if errors.As(err, &typed) {
		path = typed.Field
	}
	if name == "data_find" && (path == "arguments" || strings.Contains(path, "what")) {
		path = "need.what"
	}
	s.argumentMu.Lock()
	s.argumentFailures[name]++
	attempts := s.argumentFailures[name]
	s.argumentMu.Unlock()
	remaining := MaxArgumentAttempts - attempts
	if remaining < 0 {
		remaining = 0
	}
	example := any(map[string]any{"find_id": "data_find返回的真实编号"})
	if name == "data_find" {
		example = map[string]any{"need": map[string]any{"name": "河流线", "what": map[string]any{"description": "线状河流"}, "geometry": "line", "extent": "用户指定的区域"}, "provided": []string{}}
	}
	code := "DATA_ARGUMENT_INVALID"
	message := err.Error()
	if typed != nil && path == "need.what" {
		message = `need.what 必须是对象，例如 {"description":"线状河流"} 或 {"category":"河流"}；不能直接填写字符串或数字`
	}
	if remaining == 0 {
		code = "DATA_ARGUMENT_RETRY_EXHAUSTED"
		message += "；相同工具参数连续三次无效，本轮停止，不能换智能体继续盲试"
	}
	return &ArgumentError{Tool: name, Code: code, Path: path, Message: message, Expected: example, Attempts: attempts, Remaining: remaining}
}
