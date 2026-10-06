import React from 'react'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import ContextToolbar from '@/components/chat/ContextToolbar'
import { resolvePlanUnavailableReason } from '@/lib/planModeAvailability'

class ResizeObserverMock {
  observe() {}
  unobserve() {}
  disconnect() {}
}

globalThis.ResizeObserver = ResizeObserverMock as unknown as typeof ResizeObserver

function renderToolbar(overrides: Partial<React.ComponentProps<typeof ContextToolbar>> = {}) {
  const onExecutionModeChange = vi.fn()
  const view = render(
    <ContextToolbar
      executionMode="build"
      thinkingEnabled={false}
      activeModelName="chat-main"
      activeAgentConfig="default"
      models={[
        {
          name: 'chat-main',
          model: 'gpt-4.1',
          provider: 'openai',
          supports_thinking: true,
          supports_plan_execute: true,
          supports_forced_tool_choice: true,
          ready_for_chat: true,
          ready_for_plan: true,
          ready_for_auto_title: true,
          readiness_reason: '',
        },
      ]}
      agentConfigs={['default']}
      messageCount={12}
      estimatedTokens={4200}
      totalChars={12600}
      onExecutionModeChange={onExecutionModeChange}
      onThinkingToggle={vi.fn()}
      onModelSwitch={vi.fn()}
      onAgentConfigSwitch={vi.fn()}
      onCompress={vi.fn()}
      onCreateSnapshot={vi.fn()}
      onRestoreSnapshot={vi.fn()}
      recentSnapshots={[]}
      modelLoadError="模型列表加载失败"
      onReloadRuntimeConfig={vi.fn()}
      running
      {...overrides}
    />,
  )

  return { ...view, onExecutionModeChange }
}

describe('ContextToolbar', () => {
  it('运行中仅锁定会影响当前会话运行态的控件', () => {
    renderToolbar()

    expect(screen.getByRole('button', { name: /build/i })).toBeDisabled()
    expect(screen.getByRole('button', { name: /chat-main/i })).toBeDisabled()
    expect(screen.getByRole('button', { name: /default/i })).toBeDisabled()
    expect(screen.getByRole('button', { name: '压缩' })).toBeDisabled()
    expect(screen.getByRole('button', { name: /快照/i })).toBeDisabled()

    expect(screen.getByRole('checkbox')).toBeEnabled()
    expect(screen.getByRole('button', { name: '重试' })).toBeDisabled()
  })

  it('当 runtime 已声明 plan 可用时，不再被当前聊天模型的 plan 能力阻断', async () => {
    const user = userEvent.setup()
    const { onExecutionModeChange } = renderToolbar({
      running: false,
      modelLoadError: '',
      planEnabled: true,
      planIssue: '',
      models: [
        {
          name: 'chat-main',
          model: 'gpt-4.1',
          provider: 'openai',
          supports_thinking: true,
          supports_plan_execute: false,
          supports_forced_tool_choice: true,
          ready_for_chat: true,
          ready_for_plan: false,
          ready_for_auto_title: true,
          readiness_reason: '当前聊天模型自身不支持 plan',
        },
      ],
    })

    await user.click(screen.getByRole('button', { name: /build/i }))
    await user.click(screen.getByText('PLAN'))

    expect(onExecutionModeChange).toHaveBeenCalledWith('plan')
    expect(screen.queryByText('当前不可切换')).not.toBeInTheDocument()
  })

  it('plan 不可用原因只由 runtime 级 plan 开关与 issue 决定', () => {
    expect(resolvePlanUnavailableReason(true, '')).toBe('')
    expect(resolvePlanUnavailableReason(true, '需要先重建运行时')).toBe('需要先重建运行时')
    expect(resolvePlanUnavailableReason(false, '')).toBe('当前激活智能体配置未开启 Plan 模式。')
  })

  it('BUILD 选项会使用执行态主题色', () => {
    renderToolbar({ running: false, modelLoadError: '' })

    const buildButton = screen.getByRole('button', { name: /build/i })
    expect(buildButton.className).toContain('emerald')
    expect(screen.getByText('⚡ BUILD').className).toContain('emerald')
    expect(screen.getByRole('button', { name: '压缩' }).className).toContain('emerald')
  })
})
