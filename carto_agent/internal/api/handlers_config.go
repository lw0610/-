package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/gorilla/mux"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/llm"
)

var image2HTTPClient = &http.Client{Timeout: 20 * time.Second}

// handleGetAllConfig GET /api/config
func (a *API) handleGetAllConfig(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "config_get_all") {
		return
	}
	if a.settingsMutationService == nil {
		writeControlPlaneError(w, r, http.StatusServiceUnavailable, nil, newAvailabilityEnvelope(
			"CONFIG_SERVICE_UNAVAILABLE",
			"设置服务未初始化",
			"config",
			"config_api",
			map[string]any{"dependency": "settings_mutation_service", "operation": "get_all_config"},
		), nil)
		return
	}
	allConfig, err := a.settingsMutationService.GetAllConfig()
	if err != nil {
		writeControlPlaneError(w, r, http.StatusInternalServerError, err, newRequestEnvelope(
			"CONFIG_READ_FAILED",
			"读取全量配置失败",
			"config",
			"config_api",
			"请检查配置服务状态后重试。",
			map[string]any{"operation": "get_all_config"},
		), nil)
		return
	}
	Success(w, allConfig)
}

// handleGetConfig GET /api/config/{path}
func (a *API) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "config_get") {
		return
	}
	if a.settingsMutationService == nil {
		writeControlPlaneError(w, r, http.StatusServiceUnavailable, nil, newAvailabilityEnvelope(
			"CONFIG_SERVICE_UNAVAILABLE",
			"设置服务未初始化",
			"config",
			"config_api",
			map[string]any{"dependency": "settings_mutation_service", "operation": "get_config"},
		), nil)
		return
	}
	path := mux.Vars(r)["path"]
	userID := r.URL.Query().Get("user_id")
	value, err := a.settingsMutationService.GetConfig(path, userID)
	if err != nil {
		writeControlPlaneError(w, r, http.StatusNotFound, err, newRequestEnvelope(
			"CONFIG_NOT_FOUND",
			err.Error(),
			"config",
			"config_api",
			"请确认配置路径是否存在后重试。",
			map[string]any{"path": path, "operation": "get_config"},
		), nil)
		return
	}
	Success(w, value)
}

// handleSetConfig PUT /api/config/{path}
func (a *API) handleSetConfig(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "config_set") {
		return
	}
	if a.settingsMutationService == nil {
		writeControlPlaneError(w, r, http.StatusServiceUnavailable, nil, newAvailabilityEnvelope(
			"CONFIG_SERVICE_UNAVAILABLE",
			"设置服务未初始化",
			"config",
			"config_api",
			map[string]any{"dependency": "settings_mutation_service", "operation": "set_config"},
		), nil)
		return
	}
	path := mux.Vars(r)["path"]
	userID := r.URL.Query().Get("user_id")
	var body struct {
		Value any `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeControlPlaneError(w, r, http.StatusBadRequest, err, newRequestEnvelope(
			"CONFIG_REQUEST_INVALID",
			"无效的请求体",
			"config",
			"config_api",
			"请提供合法的 JSON 请求体。",
			map[string]any{"operation": "set_config", "path": path},
		), nil)
		return
	}
	result, err := a.settingsMutationService.SaveConfig(path, body.Value, userID)
	if err != nil {
		if writeSettingsMutationError(w, r, err) {
			return
		}
		if writeConfigMigrationError(w, r, err, "set_config", map[string]any{"path": path}) {
			return
		}
		writeControlPlaneError(w, r, http.StatusInternalServerError, err, newRequestEnvelope(
			"CONFIG_SAVE_FAILED",
			"配置保存失败",
			"config",
			"config_api",
			"请检查配置服务状态后重试。",
			map[string]any{"operation": "set_config", "path": path},
		), nil)
		return
	}
	Success(w, result)
}

// handleBatchSetConfig PUT /api/config/batch
func (a *API) handleBatchSetConfig(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "config_batch_set") {
		return
	}
	if a.settingsMutationService == nil {
		writeControlPlaneError(w, r, http.StatusServiceUnavailable, nil, newAvailabilityEnvelope(
			"CONFIG_SERVICE_UNAVAILABLE",
			"设置服务未初始化",
			"config",
			"config_api",
			map[string]any{"dependency": "settings_mutation_service", "operation": "batch_set_config"},
		), nil)
		return
	}
	var body struct {
		Items []config.ConfigItem `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeControlPlaneError(w, r, http.StatusBadRequest, err, newRequestEnvelope(
			"CONFIG_REQUEST_INVALID",
			"无效的请求体",
			"config",
			"config_api",
			"请提供合法的 JSON 请求体。",
			map[string]any{"operation": "batch_set_config"},
		), nil)
		return
	}
	result, err := a.settingsMutationService.BatchSetConfig(body.Items)
	if err != nil {
		if writeSettingsMutationError(w, r, err) {
			return
		}
		if writeConfigMigrationError(w, r, err, "batch_set_config", nil) {
			return
		}
		writeControlPlaneError(w, r, http.StatusInternalServerError, err, newRequestEnvelope(
			"CONFIG_BATCH_SAVE_FAILED",
			"批量配置保存失败",
			"config",
			"config_api",
			"请检查配置服务状态后重试。",
			map[string]any{"operation": "batch_set_config"},
		), nil)
		return
	}
	Success(w, result)
}

// handleGetLLMProviders GET /api/llm/providers
func (a *API) handleGetLLMProviders(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "llm_providers_get") {
		return
	}
	if a.settingsMutationService == nil {
		writeControlPlaneError(w, r, http.StatusServiceUnavailable, nil, newAvailabilityEnvelope(
			"CONFIG_SERVICE_UNAVAILABLE",
			"设置服务未初始化",
			"config",
			"config_api",
			map[string]any{"dependency": "settings_mutation_service", "operation": "get_llm_config"},
		), nil)
		return
	}
	value, err := a.settingsMutationService.GetLLMConfig()
	if err != nil {
		writeControlPlaneError(w, r, http.StatusInternalServerError, err, newRequestEnvelope(
			"LLM_CONFIG_READ_FAILED",
			"读取 LLM 配置失败",
			"config",
			"config_api",
			"请检查配置服务状态后重试。",
			map[string]any{"operation": "get_llm_config"},
		), nil)
		return
	}
	Success(w, value)
}

// handleSaveLLMProviders PUT /api/llm/providers
func (a *API) handleSaveLLMProviders(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "llm_providers_save") {
		return
	}
	if a.settingsMutationService == nil {
		writeControlPlaneError(w, r, http.StatusServiceUnavailable, nil, newAvailabilityEnvelope(
			"CONFIG_SERVICE_UNAVAILABLE",
			"设置服务未初始化",
			"config",
			"config_api",
			map[string]any{"dependency": "settings_mutation_service", "operation": "save_llm_providers"},
		), nil)
		return
	}
	var llmConfig map[string]any
	if err := json.NewDecoder(r.Body).Decode(&llmConfig); err != nil {
		writeControlPlaneError(w, r, http.StatusBadRequest, err, newRequestEnvelope(
			"LLM_CONFIG_REQUEST_INVALID",
			"无效的请求体",
			"config",
			"config_api",
			"请提供合法的 JSON 请求体。",
			map[string]any{"operation": "save_llm_providers"},
		), nil)
		return
	}
	result, err := a.settingsMutationService.SaveLLMProviders(llmConfig)
	if err != nil {
		if writeSettingsMutationError(w, r, err) {
			return
		}
		writeControlPlaneError(w, r, http.StatusInternalServerError, err, newRequestEnvelope(
			"LLM_CONFIG_SAVE_FAILED",
			"保存 LLM 配置失败",
			"config",
			"config_api",
			"请检查配置服务状态后重试。",
			map[string]any{"operation": "save_llm_providers"},
		), nil)
		return
	}
	Success(w, result)
}

// handleGetLLMRegistry GET /api/llm/registry
func (a *API) handleGetLLMRegistry(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "llm_registry_get") {
		return
	}
	Success(w, map[string]any{
		"providers": llm.ListProviderRegistry(),
	})
}

// handleTestLLMConnection POST /api/llm/test
func (a *API) handleTestLLMConnection(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "llm_test") {
		return
	}
	var body struct {
		Provider        string  `json:"provider"`
		Model           string  `json:"model"`
		APIKey          string  `json:"api_key"`
		BaseURL         string  `json:"base_url"`
		MaxTokens       int     `json:"max_tokens"`
		Temperature     float64 `json:"temperature"`
		ThinkingEnabled bool    `json:"thinking_enabled"`
		ThinkingBudget  int     `json:"thinking_budget"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "无效的请求体")
		return
	}

	modelConfig := &llm.Config{
		Provider:        strings.TrimSpace(body.Provider),
		Model:           strings.TrimSpace(body.Model),
		APIKey:          strings.TrimSpace(body.APIKey),
		BaseURL:         strings.TrimSpace(body.BaseURL),
		MaxTokens:       body.MaxTokens,
		Temperature:     body.Temperature,
		ThinkingEnabled: body.ThinkingEnabled,
		ThinkingBudget:  body.ThinkingBudget,
	}
	if modelConfig.MaxTokens <= 0 || modelConfig.MaxTokens > 64 {
		modelConfig.MaxTokens = 16
	}
	if modelConfig.ThinkingBudget <= 0 {
		modelConfig.ThinkingBudget = 1024
	}

	chatModel, err := llm.NewChatModel(modelConfig)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	resp, err := chatModel.Generate(ctx, []*schema.Message{
		schema.SystemMessage("你是连接测试助手。请只回复 OK。"),
		schema.UserMessage("请返回 OK，用于验证模型连接。"),
	})
	if err != nil {
		Error(w, http.StatusBadGateway, err.Error())
		return
	}
	Success(w, map[string]any{
		"provider": modelConfig.Provider,
		"model":    modelConfig.Model,
		"message":  strings.TrimSpace(resp.Content),
	})
}

// handleGetImage2Config GET /api/image2/config
func (a *API) handleGetImage2Config(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "image2_config_get") {
		return
	}
	if a.configService == nil {
		Error(w, http.StatusServiceUnavailable, "设置服务未初始化")
		return
	}
	view, err := a.configService.GetImage2Config()
	if err != nil {
		Error(w, http.StatusInternalServerError, "读取 Image 2 配置失败")
		return
	}
	Success(w, view)
}

// handleSaveImage2Config PUT /api/image2/config
func (a *API) handleSaveImage2Config(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "image2_config_save") {
		return
	}
	if a.configService == nil {
		Error(w, http.StatusServiceUnavailable, "设置服务未初始化")
		return
	}
	var body struct {
		Enabled bool   `json:"enabled"`
		APIKey  string `json:"api_key"`
		BaseURL string `json:"base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "无效的请求体")
		return
	}
	result, err := a.configService.SaveImage2Config(body.Enabled, body.APIKey, body.BaseURL)
	if err != nil {
		if errors.Is(err, config.ErrInvalidImage2BaseURL) {
			Error(w, http.StatusBadRequest, "Image 2 服务地址无效，请输入完整的 http:// 或 https:// 地址")
			return
		}
		Error(w, http.StatusInternalServerError, "保存 Image 2 配置失败")
		return
	}
	Success(w, map[string]any{
		"scope":           "image2",
		"persisted":       result.Changed,
		"config_loaded":   true,
		"runtime_applied": true,
		"required_action": "none",
		"affected_paths":  []string{"image2.enabled", "image2.base_url", "secrets.image2"},
	})
}

// handleTestImage2Connection POST /api/image2/test
// 只读取模型列表验证鉴权和服务可达性，不生成图片。
func (a *API) handleTestImage2Connection(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "image2_connection_test") {
		return
	}
	if a.configService == nil {
		Error(w, http.StatusServiceUnavailable, "设置服务未初始化")
		return
	}
	var body struct {
		APIKey  string `json:"api_key"`
		BaseURL string `json:"base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "无效的请求体")
		return
	}
	apiKey := strings.TrimSpace(body.APIKey)
	view, err := a.configService.GetImage2Config()
	if err != nil {
		Error(w, http.StatusInternalServerError, "读取 Image 2 配置失败")
		return
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(view.APIKey)
	}
	baseURL := strings.TrimSpace(body.BaseURL)
	if baseURL == "" {
		baseURL = view.BaseURL
	}
	baseURL, err = config.NormalizeImage2BaseURL(baseURL)
	if err != nil {
		Error(w, http.StatusBadRequest, "Image 2 服务地址无效，请输入完整的 http:// 或 https:// 地址")
		return
	}
	if apiKey == "" {
		Error(w, http.StatusBadRequest, "请先填写 OpenAI API Key")
		return
	}

	endpoint := baseURL + "/models"
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		Error(w, http.StatusInternalServerError, "无法创建 Image 2 连接测试")
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := image2HTTPClient.Do(req)
	if err != nil {
		Error(w, http.StatusBadGateway, "无法连接 OpenAI Image 2 服务")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		message := "Image 2 连接测试失败"
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			message = "OpenAI API Key 无效"
		case http.StatusForbidden:
			message = "当前 API Key 无权访问 Image 2 服务"
		case http.StatusNotFound:
			message = "Image 2 服务地址不支持模型列表接口"
		}
		Error(w, http.StatusBadGateway, message)
		return
	}
	Success(w, map[string]any{
		"status": "ok",
		"model":  config.Image2ModelName,
	})
}
