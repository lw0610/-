import React from 'react'
import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ChatPage from '../page'

const connectionState = {
  connected: true,
}

const instanceState = {
  projectConnected: false,
}

const chatState = {
  messages: [] as Array<{ id: string; type: string; content: string }>,
}

vi.mock('@/components/layout/AppShell', () => ({
  __esModule: true,
  default: ({ children }: { children?: React.ReactNode }) => <div data-testid="app-shell">{children}</div>,
}))

vi.mock('@/components/chat/ChatPanel', () => ({
  __esModule: true,
  default: () => <div data-testid="chat-panel">ChatPanel</div>,
}))

vi.mock('@/components/chat/ProjectNotConnected', () => ({
  __esModule: true,
  default: () => <div data-testid="project-not-connected">ProjectNotConnected</div>,
}))

vi.mock('@/stores/connectionStore', () => ({
  useConnectionStore: (selector: (state: typeof connectionState) => unknown) => selector(connectionState),
}))

vi.mock('@/stores/instanceStore', () => ({
  useInstanceStore: (selector: (state: typeof instanceState) => unknown) => selector(instanceState),
}))

vi.mock('@/stores/chatStore', () => ({
  useChatStore: (selector: (state: typeof chatState) => unknown) => selector(chatState),
}))

describe('ChatPage degraded gating', () => {
  beforeEach(() => {
    connectionState.connected = true
    instanceState.projectConnected = false
    chatState.messages = []
  })

  it('后端在线但项目未连接时仍保留聊天工作区', () => {
    render(<ChatPage />)

    expect(screen.getByTestId('chat-panel')).toBeInTheDocument()
    expect(screen.queryByTestId('project-not-connected')).not.toBeInTheDocument()
  })

  it('后端断开且没有历史内容时回退到连接占位', () => {
    connectionState.connected = false

    render(<ChatPage />)

    expect(screen.getByTestId('project-not-connected')).toBeInTheDocument()
    expect(screen.queryByTestId('chat-panel')).not.toBeInTheDocument()
  })

  it('即使项目未连接，只要已有历史消息也继续显示聊天工作区', () => {
    connectionState.connected = false
    chatState.messages = [{ id: 'm-1', type: 'assistant', content: '历史恢复成功' }]

    render(<ChatPage />)

    expect(screen.getByTestId('chat-panel')).toBeInTheDocument()
  })
})
