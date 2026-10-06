package websocket

import (
	"strings"

	"github.com/gorilla/websocket"

	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/settings"
)

func (s *DeepServer) handleSkillMessage(conn *websocket.Conn, msg Message) bool {
	requestID, _ := msg.Data["request_id"].(string)
	data, _ := msg.Data["data"].(map[string]any)
	assets := s.assetService()
	mutations := s.settingsService()
	realtime := s.realtimeService()
	if assets == nil && mutations == nil {
		return false
	}

	switch msg.Type {
	case MsgTypeSkillList:
		skills, err := assets.ListSkills()
		if err != nil {
			s.sendSkillResponse(conn, requestID, false, nil, err, nil)
		} else {
			s.sendSkillResponse(conn, requestID, true, map[string]any{"skills": skills}, nil, nil)
		}
		return true

	case MsgTypeSkillGet:
		skillName, _ := data["skill_name"].(string)
		content, err := assets.GetSkillContent(skillName)
		if err != nil {
			s.sendSkillResponse(conn, requestID, false, nil, err, nil)
		} else {
			s.sendSkillResponse(conn, requestID, true, content, nil, nil)
		}
		return true

	case MsgTypeSkillSave:
		skillName, _ := data["skill_name"].(string)
		description, _ := data["description"].(string)
		content, _ := data["content"].(string)
		scope, _ := data["scope"].(string)
		configName, _ := data["config_name"].(string)
		mutation, err := mutations.SaveSkillWithScope(skillName, scope, configName, description, content)
		if err != nil {
			s.sendSkillResponse(conn, requestID, false, nil, err, nil)
		} else {
			s.sendSkillResponse(conn, requestID, true, map[string]any{"mutation": mutation}, nil, nil)
			realtime.BroadcastSkillCatalogChanged("refresh", "api", strings.TrimSpace(skillName))
		}
		return true

	case MsgTypeSkillDelete:
		skillName, _ := data["skill_name"].(string)
		scope, _ := data["scope"].(string)
		configName, _ := data["config_name"].(string)
		mutation, err := mutations.DeleteSkillWithScope(skillName, scope, configName)
		if err != nil {
			s.sendSkillResponse(conn, requestID, false, nil, err, nil)
		} else {
			s.sendSkillResponse(conn, requestID, true, map[string]any{"mutation": mutation}, nil, nil)
			realtime.BroadcastSkillCatalogChanged("refresh", "api", strings.TrimSpace(skillName))
		}
		return true

	case MsgTypeSkillImport:
		sourcePath, _ := data["source_path"].(string)
		overwrite, _ := data["overwrite"].(bool)
		scope, _ := data["scope"].(string)
		configName, _ := data["config_name"].(string)
		result, err := mutations.ImportSkillWithScope(sourcePath, overwrite, scope, configName)
		if err != nil {
			s.sendSkillResponse(conn, requestID, false, map[string]any{"action": "import"}, err, nil)
		} else {
			s.sendSkillResponse(conn, requestID, true, result, nil, nil)
			importedCount := 0
			if importedRaw, ok := result["imported"].([]map[string]any); ok {
				importedCount = len(importedRaw)
			}
			if importedCount > 0 {
				realtime.BroadcastSkillCatalogChanged("refresh", "api", "")
			}
		}
		return true
	}

	return false
}

func (s *DeepServer) sendSkillResponse(conn *websocket.Conn, requestID string, success bool, data map[string]any, err error, fallback *control.ErrorEnvelope) {
	respData := map[string]any{
		"request_id": requestID,
		"success":    success,
	}
	if data != nil {
		for k, v := range data {
			respData[k] = v
		}
	}
	applyWebSocketErrorPayload(respData, err, fallback, map[string]any{
		"message_type": MsgTypeSkillResponse,
		"request_id":   strings.TrimSpace(requestID),
	})
	s.sendMessage(conn, Message{
		Type: MsgTypeSkillResponse,
		Data: respData,
	})
}

func sanitizeSkillName(raw string) string {
	return settings.SanitizeSkillName(raw)
}

func defaultSkillNameFromPath(path string) string {
	return settings.DefaultSkillNameFromPath(path)
}

func buildStandardSkillMarkdown(skillName, description, content string) string {
	return settings.BuildStandardSkillMarkdown(skillName, description, content)
}

func parseSkillFile(raw string) (string, string, string) {
	return settings.ParseSkillFile(raw)
}

func (s *DeepServer) listSkills() ([]map[string]any, error) {
	return s.assetService().ListSkills()
}

func (s *DeepServer) getSkillContent(skillName string) (map[string]any, error) {
	return s.assetService().GetSkillContent(skillName)
}

func (s *DeepServer) saveSkill(skillName, description, content string) error {
	return s.saveSkillWithScope(skillName, "", "", description, content)
}

func (s *DeepServer) saveSkillWithScope(skillName, scope, configName, description, content string) error {
	_, err := s.settingsService().SaveSkillWithScope(skillName, scope, configName, description, content)
	return err
}

func (s *DeepServer) importSkill(sourcePath string, overwrite bool) (map[string]any, error) {
	return s.importSkillWithScope(sourcePath, overwrite, "", "")
}

func (s *DeepServer) importSkillWithScope(sourcePath string, overwrite bool, scope, configName string) (map[string]any, error) {
	return s.settingsService().ImportSkillWithScope(sourcePath, overwrite, scope, configName)
}

func (s *DeepServer) deleteSkill(skillName string) error {
	return s.deleteSkillWithScope(skillName, "", "")
}

func (s *DeepServer) deleteSkillWithScope(skillName, scope, configName string) error {
	_, err := s.settingsService().DeleteSkillWithScope(skillName, scope, configName)
	return err
}
