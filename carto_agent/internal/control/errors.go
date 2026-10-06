package control

import "strings"

func newConfigServiceUnavailableError() error {
	envelope := NewErrorEnvelope("CONFIG_SERVICE_UNAVAILABLE", "配置服务未初始化", string(DomainConfig), "settings_mutation")
	envelope.Retryable = true
	envelope.Degraded = true
	envelope.UserAction = "请先初始化配置服务后重试。"
	envelope.DebugContext["dependency"] = "config_service"
	return envelope
}

func newAgentConfigUnavailableError(stage string) error {
	envelope := NewErrorEnvelope("AGENT_CONFIG_UNAVAILABLE", "智能体配置管理器未初始化", string(DomainConfig), stage)
	envelope.Retryable = true
	envelope.Degraded = true
	envelope.UserAction = "请先初始化智能体配置管理器后重试。"
	envelope.DebugContext["dependency"] = "agent_config_manager"
	return envelope
}

func newSkillCatalogUnavailableError(stage string) error {
	envelope := NewErrorEnvelope("SKILL_CATALOG_UNAVAILABLE", "技能服务未初始化", string(DomainCatalog), stage)
	envelope.Retryable = true
	envelope.Degraded = true
	envelope.UserAction = "请先初始化技能目录服务后重试。"
	envelope.DebugContext["dependency"] = "skill_catalog"
	return envelope
}

func newConfigNotFoundError(name string) error {
	trimmedName := strings.TrimSpace(name)
	envelope := NewErrorEnvelope("CONFIG_NOT_FOUND", "配置不存在: "+trimmedName, string(DomainConfig), "asset_catalog")
	envelope.UserAction = "请确认配置名称是否存在后重试。"
	envelope.DebugContext["config_name"] = trimmedName
	return envelope
}

func newAgentNotFoundError() error {
	envelope := NewErrorEnvelope("AGENT_NOT_FOUND", "智能体不存在", string(DomainConfig), "asset_catalog")
	envelope.UserAction = "请确认目标智能体已存在后重试。"
	envelope.DebugContext["entity"] = "agent"
	return envelope
}
