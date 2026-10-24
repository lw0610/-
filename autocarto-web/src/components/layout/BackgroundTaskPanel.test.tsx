import React from 'react'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import BackgroundTaskPanel from './BackgroundTaskPanel'
import { useChatStore } from '@/stores/chatStore'
import { useSessionStore } from '@/stores/sessionStore'

const sendCancelMock = vi.hoisted(() => vi.fn())
const pushMock = vi.hoisted(() => vi.fn())

vi.mock('@/lib/websocket', () => ({
  wsClient: {
    sendCancel: sendCancelMock,
  },
}))

vi.mock('next/navigation', () => ({
  useRouter: () => ({
    push: pushMock,
  }),
}))

vi.mock('@/components/runtime/SessionRuntimeSummary', () => ({
  default: () => <div data-testid="runtime-summary" />,
}))

describe('BackgroundTaskPanel', () => {
  beforeEach(() => {
    sendCancelMock.mockReset()
    pushMock.mockReset()
    sendCancelMock.mockReturnValue(true)
    useChatStore.setState({
      runningSessionIds: ['sid-bg'],
      sessionPendingInterrupts: {},
      sessionRuntimeStates: {
        'sid-bg': {
          session_id: 'sid-bg',
          status: 'running',
          updated_at: '2026-05-28T00:00:00Z',
        },
      },
    })
    useSessionStore.setState({
      sessions: [
        {
          session_id: 'sid-bg',
          name: '后台绘图任务',
          last_message_preview: 'running',
          created_at: '2026-05-28T00:00:00Z',
          updated_at: '2026-05-28T00:00:00Z',
          messages: [],
        },
      ],
      sessionAttachState: {},
      setActiveSession: vi.fn(),
    })
  })

  it('sends cancel for the selected background task session', async () => {
    const anchor = document.createElement('button')
    document.body.appendChild(anchor)
    const anchorRef = { current: anchor }
    const panelRef = { current: null }

    render(
      <BackgroundTaskPanel
        anchorRef={anchorRef}
        panelRef={panelRef}
        onClose={vi.fn()}
      />,
    )

    const cancelButton = await screen.findByRole('button', { name: '取消' })
    fireEvent.click(cancelButton)

    await waitFor(() => {
      expect(sendCancelMock).toHaveBeenCalledWith('sid-bg')
    })
  })
})
