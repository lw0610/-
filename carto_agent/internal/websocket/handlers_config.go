package websocket

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/logger"
	"github.com/autocarto/carto_agent/internal/settings"
)

func (s *DeepServer) handleConfigMessage(conn *websocket.Conn, msg Message) bool {
	settingsService := s.settingsService()
	if settingsService == nil {
		return false
	}

	requestID, _ := msg.Data["request_id"].(string)

	switch msg.Type {
	case MsgTypeConfigGet:
		path, _ := msg.Data["path"].(string)
		userID, _ := msg.Data["user_id"].(string)

		value, err := settingsService.GetConfig(path, userID)
		if err != nil {
			s.sendConfigResponse(conn, requestID, false, nil, err, nil)
		} else {
			s.sendConfigResponse(conn, requestID, true, value, nil, nil)
		}
		return true

	case MsgTypeConfigSet:
		path, _ := msg.Data["path"].(string)
		value := msg.Data["value"]
		userID, _ := msg.Data["user_id"].(string)

		if result, err := settingsService.SaveConfig(path, value, userID); err != nil {
			s.sendConfigResponseWithData(conn, requestID, false, nil, err, buildConfigMigrationEnvelope(err, "set_config", map[string]any{"path": path}), buildMutationErrorData(err))
		} else {
			s.sendConfigResponseWithData(conn, requestID, true, nil, nil, nil, map[string]any{"mutation": result})
			s.broadcastConfigChanged(conn, path, value)
		}
		return true

	case MsgTypeConfigGetAll:
		allConfig, err := settingsService.GetAllConfig()
		if err != nil {
			s.sendConfigResponse(conn, requestID, false, nil, err, nil)
		} else {
			s.sendConfigResponse(conn, requestID, true, allConfig, nil, nil)
		}
		return true

	case MsgTypeConfigBatchSet:
		itemsRaw, ok := msg.Data["items"].([]any)
		if !ok {
			s.sendConfigResponse(conn, requestID, false, nil, fmt.Errorf("无效的批量配置格式"), newWebSocketRequestEnvelope(
				"INVALID_CONFIG_REQUEST",
				"无效的批量配置格式",
				string(control.DomainConfig),
				"config_batch_set",
				"请检查批量配置请求体后重试。",
				map[string]any{"message_type": MsgTypeConfigBatchSet},
			))
			return true
		}

		items := make([]config.ConfigItem, 0, len(itemsRaw))
		for _, raw := range itemsRaw {
			if item, ok := raw.(map[string]any); ok {
				path, _ := item["path"].(string)
				value := item["value"]
				if path != "" {
					items = append(items, config.ConfigItem{Path: path, Value: value})
				}
			}
		}

		if result, err := settingsService.BatchSetConfig(items); err != nil {
			s.sendConfigResponseWithData(conn, requestID, false, nil, err, buildConfigMigrationEnvelope(err, "batch_set_config", nil), buildMutationErrorData(err))
		} else {
			s.sendConfigResponseWithData(conn, requestID, true, nil, nil, nil, map[string]any{"mutation": result})
		}
		return true
	}

	return false
}

func (s *DeepServer) broadcastConfigChanged(sender *websocket.Conn, path string, value any) {
	msg := Message{
		Type: MsgTypeConfigChanged,
		Data: map[string]any{
			"path":  path,
			"value": value,
		},
	}

	s.mu.Lock()
	targets := make([]*websocket.Conn, 0, len(s.clients))
	for conn := range s.clients {
		if conn != sender {
			targets = append(targets, conn)
		}
	}
	s.mu.Unlock()

	for _, conn := range targets {
		if err := s.sendMessage(conn, msg); err != nil {
			logger.Debug("广播配置变更消息失败: %v", err)
		}
	}
}

func (s *DeepServer) reloadPlanModeConfig() error {
	if s.configService == nil {
		return fmt.Errorf("configService 未初始化")
	}
	fullCfg, err := s.configService.GetFullConfig()
	if err != nil {
		return err
	}
	if fullCfg == nil {
		return fmt.Errorf("完整配置为空")
	}

	s.mutateAgentRebuildConfig(func(cfg *AgentRebuildConfig) {
		cfg.BaseConfig = fullCfg
	})

	if rebuildConfig := s.agentRebuildConfigSnapshot(); rebuildConfig != nil && rebuildConfig.ChatModel != nil && s.agentConfigManager != nil {
		s.setPendingRuntimeRebuildReason("plan_mode_reload")
		return s.rebuildAgent()
	}

	return nil
}

func (s *DeepServer) sendConfigResponse(conn *websocket.Conn, requestID string, success bool, value any, err error, fallback *control.ErrorEnvelope) {
	s.sendConfigResponseWithData(conn, requestID, success, value, err, fallback, nil)
}

func (s *DeepServer) sendConfigResponseWithData(conn *websocket.Conn, requestID string, success bool, value any, err error, fallback *control.ErrorEnvelope, extra map[string]any) {
	respData := map[string]any{
		"request_id": requestID,
		"success":    success,
		"value":      value,
	}
	for key, val := range extra {
		respData[key] = val
	}
	applyWebSocketErrorPayload(respData, err, fallback, map[string]any{
		"message_type": MsgTypeConfigResponse,
		"request_id":   strings.TrimSpace(requestID),
	})
	s.sendMessage(conn, Message{
		Type: MsgTypeConfigResponse,
		Data: respData,
	})
}

func (s *DeepServer) handleUserMessage(conn *websocket.Conn, msg Message) bool {
	if s.userManager == nil {
		return false
	}

	requestID, _ := msg.Data["request_id"].(string)

	switch msg.Type {
	case MsgTypeUserList:
		users, err := s.userManager.ListUsers()
		if err != nil {
			s.sendUserResponse(conn, requestID, false, nil, err.Error())
		} else {
			s.sendUserResponse(conn, requestID, true, map[string]any{"users": users}, "")
		}
		return true

	case MsgTypeUserCreate:
		data, _ := msg.Data["data"].(map[string]any)
		name, _ := data["name"].(string)
		desc, _ := data["description"].(string)

		user, err := s.userManager.CreateUser(name, desc)
		if err != nil {
			s.sendUserResponse(conn, requestID, false, nil, err.Error())
		} else {
			s.sendUserResponse(conn, requestID, true, map[string]any{"user": user}, "")
		}
		return true

	case MsgTypeUserSwitch:
		data, _ := msg.Data["data"].(map[string]any)
		userID, _ := data["user_id"].(string)

		err := s.userManager.SwitchUser(userID)
		if err != nil {
			s.sendUserResponse(conn, requestID, false, nil, err.Error())
		} else {
			s.sendUserResponse(conn, requestID, true, nil, "")
		}
		return true

	case MsgTypeUserDelete:
		data, _ := msg.Data["data"].(map[string]any)
		userID, _ := data["user_id"].(string)

		err := s.userManager.DeleteUser(userID)
		if err != nil {
			s.sendUserResponse(conn, requestID, false, nil, err.Error())
		} else {
			s.sendUserResponse(conn, requestID, true, nil, "")
		}
		return true
	}

	return false
}

func (s *DeepServer) sendUserResponse(conn *websocket.Conn, requestID string, success bool, data map[string]any, errMsg string) {
	respData := map[string]any{
		"request_id": requestID,
		"success":    success,
		"error":      errMsg,
	}
	for k, v := range data {
		respData[k] = v
	}
	s.sendMessage(conn, Message{
		Type: MsgTypeUserResponse,
		Data: respData,
	})
}

func (s *DeepServer) handleAgentConfigMessage(conn *websocket.Conn, msg Message) bool {
	if !s.canHandleAgentConfigMessage(msg.Type) {
		return false
	}

	requestID, _ := msg.Data["request_id"].(string)
	data, _ := msg.Data["data"].(map[string]any)
	assets := s.assetService()
	mutations := s.settingsService()
	if assets == nil && mutations == nil {
		return false
	}

	switch msg.Type {
	case MsgTypeAgentConfigList:
		userID, _ := data["user_id"].(string)
		configs, err := assets.ListAgentConfigs(userID)
		if err != nil {
			s.sendAgentConfigResponse(conn, requestID, false, nil, err, nil)
		} else {
			s.sendAgentConfigResponse(conn, requestID, true, map[string]any{"configs": configs}, nil, nil)
		}
		return true

	case MsgTypeAgentConfigSave:
		userID, _ := data["user_id"].(string)
		configName, _ := data["config_name"].(string)
		configData, _ := data["config_data"].(map[string]any)

		acData, decodeErr := settings.DecodeAgentConfigData(configData)
		if decodeErr != nil {
			s.sendAgentConfigResponse(conn, requestID, false, nil, decodeErr, newWebSocketRequestEnvelope(
				"INVALID_AGENT_CONFIG_REQUEST",
				"智能体配置请求格式无效",
				string(control.DomainConfig),
				"agent_config_save",
				"请检查智能体配置字段后重试。",
				map[string]any{"message_type": MsgTypeAgentConfigSave},
			))
			return true
		}
		result, err := mutations.SaveAgentConfigData(userID, configName, acData)
		if err != nil {
			s.sendAgentConfigResponse(conn, requestID, false, buildMutationErrorData(err), err, nil)
		} else {
			s.sendAgentConfigResponse(conn, requestID, true, map[string]any{"mutation": result}, nil, nil)
		}
		return true

	case MsgTypeAgentConfigSwitch:
		userID, _ := data["user_id"].(string)
		configName, _ := data["config_name"].(string)

		result, err := mutations.ActivateAgentConfig(userID, configName)
		if err != nil {
			s.sendAgentConfigResponse(conn, requestID, false, buildMutationErrorData(err), err, nil)
		} else {
			s.sendAgentConfigResponse(conn, requestID, true, map[string]any{"mutation": result}, nil, nil)
		}
		return true

	case MsgTypeAgentQuickSwitch:
		configName, _ := msg.Data["config_name"].(string)
		if configName == "" {
			s.sendAgentConfigResponse(conn, requestID, false, nil, fmt.Errorf("config_name 不能为空"), newWebSocketRequestEnvelope(
				"INVALID_AGENT_CONFIG_REQUEST",
				"config_name 不能为空",
				string(control.DomainConfig),
				"agent_quick_switch",
				"请提供目标 config_name 后重试。",
				map[string]any{"message_type": MsgTypeAgentQuickSwitch},
			))
			return true
		}
		mutation, err := mutations.ActivateAgentConfig("", configName)
		if err != nil {
			s.sendAgentConfigResponse(conn, requestID, false, buildMutationErrorData(err), err, nil)
		} else {
			s.sendSessionState(conn, "")
			s.sendAgentConfigResponse(conn, requestID, true, map[string]any{"active": configName, "mutation": mutation}, nil, nil)
		}
		return true

	case MsgTypeAgentConfigDelete:
		userID, _ := data["user_id"].(string)
		configName, _ := data["config_name"].(string)

		payload, err := mutations.DeleteAgentConfig(userID, configName)
		if err != nil {
			s.sendAgentConfigResponse(conn, requestID, false, buildMutationErrorData(err), err, nil)
		} else {
			s.sendAgentConfigResponse(conn, requestID, true, payload, nil, nil)
		}
		return true

	case MsgTypeToolCatalogGet:
		scope := "global"
		if rawScope, ok := data["scope"].(string); ok && rawScope != "" {
			scope = rawScope
		}
		view, err := assets.GetToolCatalog(context.Background(), scope)
		if err != nil {
			s.sendToolCatalogResponse(conn, requestID, false, nil, err, newWebSocketAvailabilityEnvelope(
				"AGENT_CONFIG_UNAVAILABLE",
				"工具目录依赖的智能体配置不可用",
				string(control.DomainConfig),
				"asset_catalog",
				"请先初始化或修复智能体配置管理器后重试。",
				map[string]any{"message_type": MsgTypeToolCatalogGet, "scope": scope},
			))
		} else {
			projection := view.ResponseProjection()
			switch projection.FailureMode {
			case control.ToolCatalogFailureModeUnsupportedRuntime:
				fallback := newWebSocketAvailabilityEnvelope(
					"TOOL_CATALOG_UNSUPPORTED_RUNTIME",
					projection.SharedError.Message,
					string(control.DomainCatalog),
					"tool_catalog_get",
					projection.SharedError.UserAction,
					map[string]any{"message_type": MsgTypeToolCatalogGet, "scope": scope, "catalog_id": projection.CatalogID},
				)
				fallback.Retryable = projection.SharedError.Retryable
				s.sendToolCatalogResponse(conn, requestID, false, projection.Payload, fallback, fallback)
			case control.ToolCatalogFailureModeAbnormalEmpty:
				fallback := newWebSocketRequestEnvelope(
					"TOOL_CATALOG_ABNORMAL_EMPTY",
					projection.SharedError.Message,
					string(control.DomainCatalog),
					"tool_catalog_get",
					projection.SharedError.UserAction,
					map[string]any{"message_type": MsgTypeToolCatalogGet, "scope": scope, "catalog_id": projection.CatalogID},
				)
				fallback.Retryable = projection.SharedError.Retryable
				s.sendToolCatalogResponse(conn, requestID, false, projection.Payload, fallback, fallback)
			default:
				s.sendToolCatalogResponse(conn, requestID, true, projection.Payload, nil, nil)
			}
		}
		return true
	}

	return false
}

func (s *DeepServer) sendAgentConfigResponse(conn *websocket.Conn, requestID string, success bool, data map[string]any, err error, fallback *control.ErrorEnvelope) {
	respData := map[string]any{
		"request_id": requestID,
		"success":    success,
	}
	for k, v := range data {
		respData[k] = v
	}
	applyWebSocketErrorPayload(respData, err, fallback, map[string]any{
		"message_type": MsgTypeAgentConfigResponse,
		"request_id":   strings.TrimSpace(requestID),
	})
	s.sendMessage(conn, Message{
		Type: MsgTypeAgentConfigResponse,
		Data: respData,
	})
}

func (s *DeepServer) sendToolCatalogResponse(conn *websocket.Conn, requestID string, success bool, data map[string]any, err error, fallback *control.ErrorEnvelope) {
	respData := map[string]any{
		"request_id": requestID,
		"success":    success,
	}
	for k, v := range data {
		respData[k] = v
	}
	applyWebSocketErrorPayload(respData, err, fallback, map[string]any{
		"message_type": MsgTypeToolCatalogResp,
		"request_id":   strings.TrimSpace(requestID),
	})
	s.sendMessage(conn, Message{
		Type: MsgTypeToolCatalogResp,
		Data: respData,
	})
}
func (s *DeepServer) canHandleAgentConfigMessage(msgType string) bool {
	switch msgType {
	case MsgTypeAgentConfigList,
		MsgTypeAgentConfigSave,
		MsgTypeAgentConfigSwitch,
		MsgTypeAgentConfigDelete,
		MsgTypeAgentQuickSwitch,
		MsgTypeToolCatalogGet:
		return s.agentConfigManager != nil
	default:
		return false
	}
}

func toStringSlice(arr []any) []string {
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func (s *DeepServer) handleLLMConfigMessage(conn *websocket.Conn, msg Message) bool {
	requestID, _ := msg.Data["request_id"].(string)

	switch msg.Type {
	case MsgTypeLLMConfigSave:
		settingsService := s.settingsService()
		if settingsService == nil || s.configService == nil {
			return false
		}
		llmConfig, ok := msg.Data["llm_config"].(map[string]any)
		if !ok {
			s.sendLLMConfigResponse(conn, requestID, false, fmt.Errorf("无效的 LLM 配置格式"), newWebSocketRequestEnvelope(
				"INVALID_LLM_CONFIG_REQUEST",
				"无效的 LLM 配置格式",
				string(control.DomainConfig),
				"llm_config_save",
				"请检查 LLM 配置请求体后重试。",
				map[string]any{"message_type": MsgTypeLLMConfigSave},
			))
			return true
		}
		if result, err := settingsService.SaveLLMProviders(llmConfig); err != nil {
			s.sendLLMConfigResponseWithData(conn, requestID, false, err, nil, buildMutationErrorData(err))
		} else {
			s.sendLLMConfigResponseWithData(conn, requestID, true, nil, nil, map[string]any{"mutation": result})
		}
		return true

	case MsgTypeModelQuickSwitch:
		if _, err := resolveMessageProtocolVersion(msg); err != nil {
			s.sendLLMConfigResponseWithData(conn, requestID, false, err, newWebSocketRequestEnvelope(
				ErrorCodeUnsupportedProtocolVersion,
				"WebSocket 协议版本不受支持。",
				"websocket",
				"model_quick_switch",
				"请升级客户端后重试。",
				map[string]any{"message_type": MsgTypeModelQuickSwitch},
			), map[string]any{"protocol_version": websocketProtocolVersion})
			return true
		}
		settingsService := s.settingsService()
		if settingsService == nil || s.configService == nil {
			return false
		}
		providerName, _ := msg.Data["provider_name"].(string)
		if providerName == "" {
			s.sendLLMConfigResponse(conn, requestID, false, fmt.Errorf("provider_name 不能为空"), newWebSocketRequestEnvelope(
				"INVALID_LLM_CONFIG_REQUEST",
				"provider_name 不能为空",
				string(control.DomainConfig),
				"model_quick_switch",
				"请提供 provider_name 后重试。",
				map[string]any{"message_type": MsgTypeModelQuickSwitch},
			))
			return true
		}

		mutation, err := settingsService.SwitchActiveLLMProvider(providerName)
		if err != nil {
			s.sendLLMConfigResponseWithData(conn, requestID, false, err, nil, buildMutationErrorData(err))
			return true
		}

		s.sendSessionState(conn, "")
		s.sendLLMConfigResponseWithData(conn, requestID, true, nil, nil, map[string]any{"mutation": mutation})
		return true

	case MsgTypeModeSwitch:
		settingsService := s.settingsService()
		if settingsService == nil || s.configService == nil {
			return false
		}
		parsedMode, err := parseExecutionModeFromMessageData(msg.Data)
		if err != nil {
			s.sendLLMConfigResponse(conn, requestID, false, err, newWebSocketRequestEnvelope(
				"INVALID_EXECUTION_MODE",
				"执行模式无效",
				string(control.DomainConfig),
				"mode_switch",
				"请提供合法的 execution_mode 后重试。",
				map[string]any{"message_type": MsgTypeModeSwitch},
			))
			return true
		}
		mode := string(parsedMode)
		runIntent := config.ExecutionModeToRunIntent(mode)
		sessionID := strings.TrimSpace(msg.SessionID)
		if sessionID == "" {
			if rawSessionID, ok := msg.Data["session_id"].(string); ok {
				sessionID = strings.TrimSpace(rawSessionID)
			}
		}

		if sessionID != "" {
			projectPath, _ := msg.Data["project_path"].(string)
			if _, err := s.SaveSessionAtomically(sessionID, projectPath, func(sessionData map[string]any) error {
				sessionData["execution_mode"] = mode
				sessionData["run_intent"] = runIntent
				sessionData["updated_at"] = time.Now().Format(time.RFC3339)
				return nil
			}); err != nil {
				logger.Warn("执行模式持久化失败: session_id=%s, err=%v", sessionID, err)
			}
		}

		s.sendMessage(conn, Message{
			Type:      MsgTypeSessionState,
			SessionID: sessionID,
			Data: map[string]any{
				"execution_mode": mode,
				"run_intent":     runIntent,
			},
		})
		s.sendLLMConfigResponse(conn, requestID, true, nil, nil)
		return true
	}

	return false
}

func (s *DeepServer) sendLLMConfigResponse(conn *websocket.Conn, requestID string, success bool, err error, fallback *control.ErrorEnvelope) {
	s.sendLLMConfigResponseWithData(conn, requestID, success, err, fallback, nil)
}

func (s *DeepServer) sendLLMConfigResponseWithData(conn *websocket.Conn, requestID string, success bool, err error, fallback *control.ErrorEnvelope, extra map[string]any) {
	respData := map[string]any{
		"request_id": requestID,
		"success":    success,
	}
	for key, val := range extra {
		respData[key] = val
	}
	applyWebSocketErrorPayload(respData, err, fallback, map[string]any{
		"message_type": MsgTypeLLMConfigResponse,
		"request_id":   strings.TrimSpace(requestID),
	})
	s.sendMessage(conn, Message{
		Type: MsgTypeLLMConfigResponse,
		Data: respData,
	})
}

func (s *DeepServer) sendSessionState(conn *websocket.Conn, executionMode string) {
	if normalized := config.NormalizeSessionExecutionMode(executionMode); normalized != "" {
		executionMode = normalized
	} else {
		executionMode = "build"
	}
	runIntent := config.ExecutionModeToRunIntent(executionMode)
	state := map[string]any{
		"execution_mode": executionMode,
		"run_intent":     runIntent,
	}

	if s.configService != nil {
		if llmCfg, err := s.configService.GetConfig("llm", ""); err == nil {
			if llmMap, ok := llmCfg.(map[string]any); ok {
				state["active_model"] = llmMap["active"]
				state["available_models"] = llmMap["providers"]
			}
		}
	}

	if s.agentConfigManager != nil {
		state["active_agent_config"] = s.agentConfigManager.GetActiveConfigName()
		if cfgs, err := s.agentConfigManager.ListConfigs(""); err == nil {
			state["available_agent_configs"] = cfgs
		}
		if activeConfig := s.agentConfigManager.GetActiveConfig(); activeConfig != nil {
			state["agent_config_meta"] = activeConfig.Meta
		}
	}

	if rebuildConfig := s.agentRebuildConfigSnapshot(); rebuildConfig != nil && rebuildConfig.BaseConfig != nil {
		state["plan_mode_enabled"] = rebuildConfig.BaseConfig.PlanMode.IsEnabled()
	}

	s.sendMessage(conn, Message{Type: MsgTypeSessionState, Data: state})
}
