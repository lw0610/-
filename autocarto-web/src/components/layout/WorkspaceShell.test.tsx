import React from 'react'
import { render, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import WorkspaceShell from './WorkspaceShell'
import { useConnectionStore } from '@/stores/connectionStore'
import { useSettingsWorkspaceStore } from '@/stores/settingsWorkspaceStore'
import { emptySettingsWorkspaceMetadata } from '@/lib/settingsRegistry'

const apiMocks = vi.hoisted(() => ({
  getSettingsWorkspace: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  default: {
    getSettingsWorkspace: apiMocks.getSettingsWorkspace,
  },
}))

vi.mock('next/navigation', () => ({
  usePathname: () => '/settings/tools',
}))

vi.mock('@/components/settings/SettingsViewportContext', () => ({
  SettingsViewportProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  useSettingsViewport: () => ({
    registerScrollContainer: vi.fn(),
    scrollToSection: vi.fn(),
  }),
}))

vi.mock('@/components/settings/InfoTooltip', () => ({
  default: () => null,
}))

vi.mock('@/stores/connectionStore', async () => {
  const actual = await vi.importActual<typeof import('@/stores/connectionStore')>('@/stores/connectionStore')
  return {
    ...actual,
    useConnectionStore: actual.useConnectionStore,
  }
})

vi.mock('@/stores/settingsWorkspaceStore', async () => {
  const actual = await vi.importActual<typeof import('@/stores/settingsWorkspaceStore')>('@/stores/settingsWorkspaceStore')
  return {
    ...actual,
    useSettingsWorkspaceStore: actual.useSettingsWorkspaceStore,
  }
})

describe('WorkspaceShell', () => {
  beforeEach(() => {
    apiMocks.getSettingsWorkspace.mockReset()
    useSettingsWorkspaceStore.setState({
      metadata: emptySettingsWorkspaceMetadata,
      loading: false,
      loaded: false,
      error: undefined,
    })
    useConnectionStore.setState({
      connected: false,
      backendReady: true,
      backendUrl: '',
      wsUrl: '',
      startupError: '',
      authoritativeStartupError: '',
      servicePorts: {},
    })
  })

  it('loads workspace metadata once backend is ready even when backendUrl is empty', async () => {
    apiMocks.getSettingsWorkspace.mockResolvedValueOnce({
      tasks: [
        {
          key: 'tools',
          href: '/settings/tools',
          label: '工具',
          routes: ['/settings/tools'],
        },
      ],
      descriptors: [
        {
          id: 'tools-skill-packages',
          task: 'tools',
          section: '工具与技能',
          path: '/settings/tools',
          label: '技能包目录',
        },
      ],
    })

    render(
      <WorkspaceShell>
        <div>content</div>
      </WorkspaceShell>,
    )

    await waitFor(() => {
      expect(apiMocks.getSettingsWorkspace).toHaveBeenCalledTimes(1)
    })
    expect(useSettingsWorkspaceStore.getState().metadata.tasks).toHaveLength(1)
    expect(useSettingsWorkspaceStore.getState().metadata.descriptors).toHaveLength(1)
  })
})
