package dataservice

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

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

// Permission belongs to the explicitly selected query, never the latest find.
func (s *Service) AuthorizeFallback(ctx context.Context, name string) error {
	if !FallbackNeedsAuthorization(name) {
		return nil
	}
	if s.options.Config.NetworkPolicy == "deny" || s.options.Config.FallbackPolicy == "deny" {
		return failure("DATA_NETWORK_NOT_ALLOWED", "设置禁止兜底联网取数")
	}
	selection, ok := AcquisitionSelection(ctx)
	if !ok || !selection.RequiresDiscovery() {
		return failure("DATA_QUERY_SCOPE_REQUIRED", "先通过 data_get 选择本次 find_id，或由计划获取步骤传入查询；不能使用其他任务的最近查询")
	}
	kind := "web_search"
	if strings.HasPrefix(name, "osm_") {
		kind = "custom_osm"
	}
	if strings.HasPrefix(name, "browser_") {
		kind = "portal"
	}
	allowed := false
	for _, c := range selection.Fallback {
		// Reading/downloading a concrete portal URL is transport for that
		// portal, not permission to perform an unrelated public web search.
		portalTransfer := c.Kind == "portal" && (name == "web_read_page" || name == "web_download_file")
		allowed = allowed || (c.Available && (c.Kind == kind || portalTransfer))
	}
	if !allowed {
		return failure("DATA_NO_CANDIDATE", "本次查询未提供可用于 "+name+" 的兜底来源")
	}
	if s.fallbackApproved(selection.FindID) {
		return nil
	}
	result, err := hitl.Invoke(ctx, map[string]any{
		"type": "confirmation", "title": "使用兜底数据来源",
		"description": "确定性来源暂不可用，是否允许为“" + selection.Request.Need.Name + "”通过本次查询列出的门户、公开搜索或 OSM 获取真实数据？",
		"select_mode": "single", "options": []map[string]any{{"label": "允许本次兜底", "value": "allow"}, {"label": "暂不获取", "value": "deny"}}, "default": "deny", "required": true,
	})
	if err != nil {
		return err
	}
	if !result.IsSelected("allow") {
		return failure("DATA_NETWORK_NOT_ALLOWED", "用户未允许本次兜底获取")
	}
	return atomicJSON(s.permissionPath(selection.FindID), true)
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
	if !findPattern.MatchString(id) {
		return false
	}
	raw, err := os.ReadFile(s.permissionPath(id))
	if err != nil {
		return false
	}
	var approved bool
	return json.Unmarshal(raw, &approved) == nil && approved
}
