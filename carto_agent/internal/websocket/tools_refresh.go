package websocket

import (
	"fmt"

	"github.com/cloudwego/eino/components/tool"
)

// UpdateAgentTools 更新 Agent 工具并触发重建。
func (s *DeepServer) UpdateAgentTools(tools []tool.BaseTool) error {
	if s == nil {
		return fmt.Errorf("server is nil")
	}
	if s.agentRebuildConfigSnapshot() == nil {
		return fmt.Errorf("agentRebuildConfig 未设置")
	}

	cp := make([]tool.BaseTool, len(tools))
	copy(cp, tools)
	pluginSkillsDir := s.currentPluginSkillsDir()
	updated := s.mutateAgentRebuildConfig(func(cfg *AgentRebuildConfig) {
		cfg.Tools = cp
		cfg.PluginSkillsDir = pluginSkillsDir
	})
	if updated == nil {
		return fmt.Errorf("agentRebuildConfig 未设置")
	}
	s.SetRuntimeFactory(nil)
	return s.rebuildAgent()
}
