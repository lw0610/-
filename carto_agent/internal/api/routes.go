package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

// SetupRoutes 在给定的 router 上注册所有 REST API 路由
func (a *API) SetupRoutes(router *mux.Router) {
	a.router = router

	// 所有 /api 路由使用 CORS 中间件
	sub := router.PathPrefix("/api").Subrouter()
	sub.Use(corsMiddleware)

	// 健康检查
	sub.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}).Methods("GET", "OPTIONS")
	sub.HandleFunc("/capabilities", a.handleGetCapabilities).Methods("GET", "OPTIONS")
	a.setupGuideRoutes(sub)
	a.setupPlanRoutes(sub)
	a.setupDataServiceRoutes(sub)
	a.setupResourceRoutes(sub)
	sub.HandleFunc("/runtime/snapshot", a.handleRuntimeSnapshot).Methods("GET", "OPTIONS")
	sub.HandleFunc("/runtime/services", a.handleRuntimeServices).Methods("GET", "OPTIONS")
	sub.HandleFunc("/runtime/descriptor", a.handleRuntimeDescriptor).Methods("GET", "OPTIONS")

	// 配置
	sub.HandleFunc("/integrations/check", a.handleCheckIntegration).Methods("POST", "OPTIONS")
	sub.HandleFunc("/settings/workspace", a.handleSettingsWorkspace).Methods("GET", "OPTIONS")
	sub.HandleFunc("/data-sources", a.handleDataSources).Methods("GET", "POST", "OPTIONS")
	sub.HandleFunc("/data-sources/scan", a.handleScanDataSource).Methods("POST", "OPTIONS")
	sub.HandleFunc("/data-sources/{id}", a.handleDataSources).Methods("PUT", "DELETE", "OPTIONS")
	sub.HandleFunc("/data-store", a.handleDataStore).Methods("GET", "OPTIONS")
	sub.HandleFunc("/data-store/clean-expired", a.handleDataStore).Methods("POST", "OPTIONS")
	sub.HandleFunc("/data-store/{key}", a.handleDataStore).Methods("DELETE", "OPTIONS")
	sub.HandleFunc("/acquisition/config", a.handleAcquisitionConfig).Methods("GET", "PUT", "OPTIONS")
	sub.HandleFunc("/config", a.handleGetAllConfig).Methods("GET", "OPTIONS")
	sub.HandleFunc("/config/batch", a.handleBatchSetConfig).Methods("PUT", "OPTIONS")
	sub.HandleFunc("/config/{path:.*}", a.handleGetConfig).Methods("GET", "OPTIONS")
	sub.HandleFunc("/config/{path:.*}", a.handleSetConfig).Methods("PUT", "OPTIONS")

	// LLM
	sub.HandleFunc("/llm/registry", a.handleGetLLMRegistry).Methods("GET", "OPTIONS")
	sub.HandleFunc("/llm/providers", a.handleGetLLMProviders).Methods("GET", "OPTIONS")
	sub.HandleFunc("/llm/providers", a.handleSaveLLMProviders).Methods("PUT", "OPTIONS")
	sub.HandleFunc("/llm/test", a.handleTestLLMConnection).Methods("POST", "OPTIONS")

	// Image 2 工具服务（独立于聊天模型 Provider）
	sub.HandleFunc("/image2/config", a.handleGetImage2Config).Methods("GET", "OPTIONS")
	sub.HandleFunc("/image2/config", a.handleSaveImage2Config).Methods("PUT", "OPTIONS")
	sub.HandleFunc("/image2/test", a.handleTestImage2Connection).Methods("POST", "OPTIONS")

	// 智能体配置
	sub.HandleFunc("/agent-configs", a.handleListAgentConfigs).Methods("GET", "OPTIONS")
	sub.HandleFunc("/agent-configs", a.handleCreateAgentConfig).Methods("POST", "OPTIONS")
	sub.HandleFunc("/agent-configs/{name}", a.handleGetAgentConfig).Methods("GET", "OPTIONS")
	sub.HandleFunc("/agent-configs/{name}/data", a.handleGetAgentConfigData).Methods("GET", "OPTIONS")
	sub.HandleFunc("/agent-configs/{name}/migrate", a.handleMigrateAgentConfig).Methods("POST", "OPTIONS")
	sub.HandleFunc("/agent-configs/{name}", a.handleSaveAgentConfig).Methods("PUT", "OPTIONS")
	sub.HandleFunc("/agent-configs/{name}", a.handleDeleteAgentConfig).Methods("DELETE", "OPTIONS")
	sub.HandleFunc("/agent-configs/{name}/activate", a.handleActivateAgentConfig).Methods("POST", "OPTIONS")
	sub.HandleFunc("/agent-configs/{name}/clone", a.handleCloneAgentConfig).Methods("POST", "OPTIONS")

	// 工具
	sub.HandleFunc("/tools/catalog", a.handleGetToolCatalog).Methods("GET", "OPTIONS")

	// 技能
	sub.HandleFunc("/skills", a.handleListSkills).Methods("GET", "OPTIONS")
	sub.HandleFunc("/skills/import", a.handleImportSkill).Methods("POST", "OPTIONS")
	sub.HandleFunc("/skills/{name}/references", a.handleGetSkillReferences).Methods("GET", "OPTIONS")
	sub.HandleFunc("/skills/{name}", a.handleGetSkill).Methods("GET", "OPTIONS")
	sub.HandleFunc("/skills/{name}", a.handleSaveSkill).Methods("PUT", "OPTIONS")
	sub.HandleFunc("/skills/{name}", a.handleDeleteSkill).Methods("DELETE", "OPTIONS")
	sub.HandleFunc("/skill-packages", a.handleListSkillPackages).Methods("GET", "OPTIONS")
	sub.HandleFunc("/skill-packages/migrate", a.handleMigrateSkillPackages).Methods("POST", "OPTIONS")
	sub.HandleFunc("/skill-packages/{name}/references", a.handleGetSkillPackageReferences).Methods("GET", "OPTIONS")
	sub.HandleFunc("/skill-packages/{name}/toggle", a.handleToggleSkillPackage).Methods("POST", "OPTIONS")
	sub.HandleFunc("/skill-packages/{name}", a.handleGetSkillPackage).Methods("GET", "OPTIONS")

	// 会话
	a.setupSessionFileRoutes(sub)
	sub.HandleFunc("/sessions/{id}/knowledge-reviews", a.handleListKnowledgeReviews).Methods("GET", "OPTIONS")
	sub.HandleFunc("/sessions/{id}/knowledge-reviews/{review_id}", a.handleGetKnowledgeReview).Methods("GET", "OPTIONS")
	sub.HandleFunc("/sessions/{id}/knowledge-reviews/{review_id}", a.handleSaveKnowledgeReview).Methods("PUT", "OPTIONS")
	sub.HandleFunc("/sessions/{id}/knowledge-reviews/{review_id}/confirm", a.handleConfirmKnowledgeReview).Methods("POST", "OPTIONS")
	sub.HandleFunc("/sessions/{id}/knowledge-reviews/{review_id}/geocode", a.handleGeocodeKnowledgeReview).Methods("POST", "OPTIONS")
	sub.HandleFunc("/sessions/{id}/knowledge-reviews/{review_id}/callouts/import", a.handleImportKnowledgeReviewCallouts).Methods("POST", "OPTIONS")
	sub.HandleFunc("/sessions/{id}/knowledge-reviews/{review_id}/assets", a.handleImportKnowledgeReviewAsset).Methods("POST", "OPTIONS")
	sub.HandleFunc("/sessions/{id}/knowledge-reviews/{review_id}/assets/{asset_id}", a.handlePreviewKnowledgeReviewAsset).Methods("GET", "OPTIONS")
	sub.HandleFunc("/projects", a.handleListProjects).Methods("GET", "OPTIONS")
	sub.HandleFunc("/sessions", a.handleListSessions).Methods("GET", "OPTIONS")
	sub.HandleFunc("/sessions", a.handleCreateSession).Methods("POST", "OPTIONS")
	sub.HandleFunc("/sessions/import", a.handleImportSession).Methods("POST", "OPTIONS")
	sub.HandleFunc("/sessions/{id}", a.handleGetSession).Methods("GET", "OPTIONS")
	sub.HandleFunc("/sessions/{id}/export", a.handleExportSession).Methods("GET", "OPTIONS")
	sub.HandleFunc("/sessions/{id}", a.handleSaveSession).Methods("PUT", "OPTIONS")
	sub.HandleFunc("/sessions/{id}", a.handlePatchSession).Methods("PATCH", "OPTIONS")
	sub.HandleFunc("/sessions/{id}/operations", a.handleSessionOperation).Methods("POST", "OPTIONS")
	sub.HandleFunc("/sessions/{id}", a.handleDeleteSession).Methods("DELETE", "OPTIONS")
	sub.HandleFunc("/sessions/{id}/dir", a.handleInitSessionDir).Methods("POST", "OPTIONS")

	// 实例管理（Python 端注册 + 前端查询）
	sub.HandleFunc("/instances", a.handleListInstances).Methods("GET", "OPTIONS")
	sub.HandleFunc("/instances/register", a.handleInstanceRegister).Methods("POST", "OPTIONS")
	sub.HandleFunc("/instances/{id}", a.handleGetInstance).Methods("GET", "OPTIONS")
	sub.HandleFunc("/instances/{id}/layouts", a.handleGetInstanceLayouts).Methods("GET", "OPTIONS")
	sub.HandleFunc("/instances/{id}/layers", a.handleGetInstanceLayers).Methods("GET", "OPTIONS")
	sub.HandleFunc("/instances/{id}/close", a.handleCloseInstance).Methods("POST", "OPTIONS")
	sub.HandleFunc("/instances/{id}/open-project", a.handleOpenInstanceProject).Methods("POST", "OPTIONS")
	sub.HandleFunc("/instances/{id}/create-project", a.handleCreateInstanceProject).Methods("POST", "OPTIONS")
	sub.HandleFunc("/instances/{id}/heartbeat", a.handleInstanceHeartbeat).Methods("POST", "OPTIONS")
	sub.HandleFunc("/instances/{id}/event", a.handleInstanceEvent).Methods("POST", "OPTIONS")
	sub.HandleFunc("/instances/{id}", a.handleInstanceUnregister).Methods("DELETE", "OPTIONS")

	sub.HandleFunc("/services/ports", a.handleGetServicePorts).Methods("GET", "OPTIONS")
}
