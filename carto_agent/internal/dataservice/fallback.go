package dataservice

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/autocarto/carto_agent/internal/hitl"
)

// Environment inspection and cleanup do not fetch external data.
func FallbackNeedsAuthorization(name string) bool {
	switch name {
	case "browser_check_environment", "browser_list_tools", "browser_close":
		return false
	default:
		return true
	}
}

// Network permission is a setting/session decision. The current plan's source
// suggestions do not restrict which configured search tools an expert may use.
func (s *Service) AuthorizeFallback(ctx context.Context, name string) error {
	if !FallbackNeedsAuthorization(name) {
		return nil
	}
	if s.options.Config.NetworkPolicy == "deny" || s.options.Config.FallbackPolicy == "deny" {
		return failure("DATA_NETWORK_NOT_ALLOWED", "设置禁止兜底联网取数")
	}
	if s.fallbackApproved("") {
		return nil
	}
	result, err := hitl.Invoke(ctx, map[string]any{"type": "confirmation", "title": "允许联网获取数据", "description": "允许本会话使用设置中已开启的门户、搜索和数据服务获取真实数据。", "select_mode": "single", "options": []map[string]any{{"label": "允许本会话取数", "value": "allow"}, {"label": "暂不获取", "value": "deny"}}, "default": "deny", "required": true})
	if err != nil {
		return err
	}
	if !result.IsSelected("allow") {
		return failure("DATA_NETWORK_NOT_ALLOWED", "用户未允许联网获取")
	}
	return atomicJSON(s.sessionPermissionPath(), true)
}
func (s *Service) sessionPermissionPath() string {
	return filepath.Join(s.options.SessionDir, "data", "acquisition-approved.json")
}

func (s *Service) permissionPath(id string) string {
	return filepath.Join(s.options.SessionDir, "data", "finds", id+".approved.json")
}
func (s *Service) fallbackApproved(id string) bool {
	if s.options.Config.NetworkPolicy == "deny" || s.options.Config.FallbackPolicy == "deny" {
		return false
	}
	if s.options.FallbackConfirmed || s.options.Config.FallbackPolicy == "allow" {
		return true
	}
	raw, err := os.ReadFile(s.sessionPermissionPath())
	if err != nil && findPattern.MatchString(id) {
		raw, err = os.ReadFile(s.permissionPath(id))
	}
	if err != nil {
		return false
	}
	var approved bool
	return json.Unmarshal(raw, &approved) == nil && approved
}
