'use client'

import { useCallback, useEffect, useMemo, useState } from 'react'

import api from '@/lib/api'
import { resolvePlanUnavailableReason } from '@/lib/planModeAvailability'
import { wsClient } from '@/lib/websocket'
import { useChatStore } from '@/stores/chatStore'
import { useRuntimeSnapshotStore } from '@/stores/runtimeSnapshotStore'

interface UseChatRuntimeConfigParams {
  loadSessionSettings: () => Promise<void>
}

export function useChatRuntimeConfig({
  loadSessionSettings,
}: UseChatRuntimeConfigParams) {
  const {
    activeModelName,
    activeAgentConfig,
    availableModels,
    availableAgentConfigs,
    addMessage,
    setActiveModelName,
    setActiveAgentConfig,
    setAvailableModels,
    setAvailableAgentConfigs,
  } = useChatStore()

  const runtimeSnapshot = useRuntimeSnapshotStore((s) => s.snapshot)
  const [modelLoadError, setModelLoadError] = useState('')
  const [agentConfigLoadError, setAgentConfigLoadError] = useState('')

  const planReadiness = runtimeSnapshot.mode_readiness?.plan
  const snapshotReady = Boolean(runtimeSnapshot.mode_readiness)
    || runtimeSnapshot.plan_enabled !== undefined
  const planIssueMessage = snapshotReady
    ? String(planReadiness?.message || '').trim()
    : ''
  const planEnabled = snapshotReady
    ? (typeof planReadiness?.enabled === 'boolean'
        ? planReadiness.enabled
        : runtimeSnapshot.plan_enabled !== false)
    : true

  const planUnavailableReason = useMemo(
    () => resolvePlanUnavailableReason(planEnabled, planIssueMessage),
    [planEnabled, planIssueMessage],
  )

  const handleModelSwitch = useCallback((modelName: string) => {
    if (!modelName) {
      return
    }
    if (!wsClient.sendModelSwitch(modelName)) {
      addMessage({
        type: 'error',
        content: '模型切换请求发送失败，请检查连接后重试。',
      })
      return
    }
    setActiveModelName(modelName)
  }, [addMessage, setActiveModelName])

  const handleAgentConfigSwitch = useCallback(async (configName: string) => {
    if (!configName) {
      return
    }
    if (!wsClient.sendAgentQuickSwitch(configName)) {
      addMessage({
        type: 'error',
        content: '智能体配置切换请求发送失败，请检查连接后重试。',
      })
      return
    }
    setActiveAgentConfig(configName)
  }, [
    addMessage,
    setActiveAgentConfig,
  ])

  const loadRuntimeConfig = useCallback(async () => {
    setModelLoadError('')
    setAgentConfigLoadError('')

    try { const payload = await api.getLLMProviders()
    const models = payload.providers.map((item) => ({
      name: item.name,
      model: item.model,
      provider: item.provider,
      max_context_tokens: item.max_context_tokens,
      auto_compress_threshold: item.auto_compress_threshold,
      has_api_key: Boolean(item.has_api_key),
      supports_thinking: Boolean(item.supports_thinking),
      supports_plan_execute: Boolean(item.supports_plan_execute),
      supports_forced_tool_choice: Boolean(item.supports_forced_tool_choice),
      ready_for_chat: Boolean(item.ready_for_chat),
      ready_for_plan: Boolean(item.ready_for_plan),
      ready_for_auto_title: Boolean(item.ready_for_auto_title),
      readiness_reason: String(item.readiness_reason || ''),
    }))
    setAvailableModels(models)
    if (payload.active) {
      setActiveModelName(payload.active)
    } } catch (error) { void error; setAvailableModels([])
    setModelLoadError('模型列表加载失败')
    addMessage({
      type: 'error',
      content: '模型列表加载失败，请检查服务连接后重试。',
    }) }

    try { const configs = await api.getAgentConfigs()
    const names = configs.map((item) => item.config_name)
    setAvailableAgentConfigs(names)
    const active = configs.find((item) => item.is_active)?.config_name || ''
    setActiveAgentConfig(active) } catch (error) { void error; setAvailableAgentConfigs([])
    setAgentConfigLoadError('智能体配置加载失败')
    addMessage({
      type: 'error',
      content: '智能体配置加载失败，请检查服务连接后重试。',
    }) }

    await loadSessionSettings()
  }, [
    addMessage,
    loadSessionSettings,
    setActiveAgentConfig,
    setActiveModelName,
    setAvailableAgentConfigs,
    setAvailableModels,
  ])

  useEffect(() => {
    void loadRuntimeConfig()
  }, [loadRuntimeConfig])

  return {
    activeAgentConfig,
    activeModelName,
    agentConfigLoadError,
    availableAgentConfigs,
    availableModels,
    handleAgentConfigSwitch,
    handleModelSwitch,
    loadRuntimeConfig,
    modelLoadError,
    planEnabled,
    planIssueMessage,
    planUnavailableReason,
  }
}
