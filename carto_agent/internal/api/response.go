package api

import (
	"encoding/json"
	"net/http"
)

// Response 统一 REST API 响应格式
type Response struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Code    string `json:"code,omitempty"`
	Error   any    `json:"error,omitempty"`
	Details any    `json:"details,omitempty"`
}

// JSON 写入 JSON 响应
func JSON(w http.ResponseWriter, status int, resp Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

// Success 写入成功响应
func Success(w http.ResponseWriter, data any) {
	JSON(w, http.StatusOK, Response{Success: true, Data: data})
}

// Error 写入错误响应
func Error(w http.ResponseWriter, status int, msg string) {
	ErrorWithCode(w, status, "", msg)
}

// ErrorWithCode 写入带错误码的错误响应
func ErrorWithCode(w http.ResponseWriter, status int, code, msg string) {
	ErrorWithCodeAndDetails(w, status, code, msg, nil)
}

func ErrorWithCodeAndDetails(w http.ResponseWriter, status int, code, msg string, details any) {
	JSON(w, status, Response{
		Success: false,
		Code:    code,
		Error:   msg,
		Details: details,
	})
}
