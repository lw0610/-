import React from 'react'
import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import InstancesPage from './page'

type InstanceState = {
  instances: Array<{
    id: string
    status: string
    project_path: string
    source: string
    services: Record<string, unknown>
    created_at: string
    updated_at: string
    last_heartbeat: string
    qgis_version: string
    pid: number
  }>
  activeInstanceId: string | null
  qgisAvailable: boolean
  projectConnected: boolean
  instanceCount: number
  loading: boolean
  runningCount: number
  stale: boolean
  fetchInstances: ReturnType<typeof vi.fn>
  setActiveInstance: ReturnType<typeof vi.fn>
  syncFromRuntimeSnapshot: ReturnType<typeof vi.fn>
  handleInstanceEvent: ReturnType<typeof vi.fn>
}

const instanceState: InstanceState = {
  instances: [],
  activeInstanceId: null,
  qgisAvailable: true,
  projectConnected: false,
  instanceCount: 0,
  loading: false,
  runningCount: 0,
  stale: false,
  fetchInstances: vi.fn(async () => {}),
  setActiveInstance: vi.fn(),
  syncFromRuntimeSnapshot: vi.fn(),
  handleInstanceEvent: vi.fn(),
}

const connectionState = {
  backendReady: true,
  startupError: '',
}

const launcherState = {
  launching: false,
  loadingRecent: false,
  recentProjects: [],
  loadRecentProjects: vi.fn(async () => {}),
  openProjectFromPicker: vi.fn(async () => null),
  openRecentProject: vi.fn(async () => null),
  createProject: vi.fn(async () => {}),
}

vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
    info: vi.fn(),
  },
}))

vi.mock('@/components/layout/AppShell', () => ({
  __esModule: true,
  default: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
}))

vi.mock('@/components/common/AppIcon', () => ({
  CloseIcon: () => <span>CloseIcon</span>,
  PlusIcon: () => <span>PlusIcon</span>,
}))

vi.mock('@/components/common/ConfirmDialog', () => ({
  __esModule: true,
  default: () => null,
}))

vi.mock('@/components/instances/NewProjectDialog', () => ({
  __esModule: true,
  default: () => null,
}))

vi.mock('@/components/instances/ProjectLaunchPopover', () => ({
  __esModule: true,
  default: () => null,
}))

vi.mock('@/hooks/useQGISProjectLauncher', () => ({
  useQGISProjectLauncher: () => launcherState,
}))

vi.mock('@/lib/api', () => ({
  __esModule: true,
  default: {
    closeInstance: vi.fn(async () => {}),
  },
}))

vi.mock('@/lib/degradedDictionary', () => ({
  getInstanceDegradedDictionary: () => null,
}))

vi.mock('@/lib/instanceSort', () => ({
  sortInstancesByCreatedAt: (instances: unknown[]) => instances,
}))

vi.mock('@/lib/tauri', () => ({
  pickQGISProjectDirectory: vi.fn(async () => null),
}))

vi.mock('@/lib/websocket', () => ({
  __esModule: true,
  default: {
    selectInstance: vi.fn(async () => {}),
  },
  getInstanceSelectRollbackTarget: vi.fn(() => null),
}))

vi.mock('@/stores/instanceStore', () => {
  type UseInstanceStoreMock = {
    (): InstanceState
    <T>(selector: (state: InstanceState) => T): T
    getState: () => InstanceState
  }

  const useInstanceStore = ((selector?: (state: InstanceState) => unknown) => (
    typeof selector === 'function' ? selector(instanceState) : instanceState
  )) as UseInstanceStoreMock
  useInstanceStore.getState = () => instanceState
  return { useInstanceStore }
})

vi.mock('@/stores/connectionStore', () => ({
  useConnectionStore: (selector: (state: typeof connectionState) => unknown) => selector(connectionState),
}))

vi.mock('@/components/settings/StatusBadge', () => ({
  __esModule: true,
  default: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
}))

vi.mock('@/components/common/Chip', () => ({
  __esModule: true,
  default: ({ children }: { children?: React.ReactNode }) => <span>{children}</span>,
  StatusDot: () => <span>StatusDot</span>,
}))

vi.mock('@/components/settings/ActionBar', () => {
  function MockActionButtonImpl(
    { children, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement>,
    ref: React.ForwardedRef<HTMLButtonElement>,
  ) {
    return (
      <button ref={ref} type="button" {...props}>
        {children}
      </button>
    )
  }

  const MockActionButton = React.forwardRef(MockActionButtonImpl)
  MockActionButton.displayName = 'MockActionButton'

  return {
    ActionButton: MockActionButton,
  }
})

vi.mock('@/components/settings/AlertPanel', () => ({
  __esModule: true,
  default: ({ title, message }: { title?: React.ReactNode; message?: React.ReactNode }) => (
    <div>
      <div data-testid="alert-title">{title}</div>
      <div data-testid="alert-message">{message}</div>
    </div>
  ),
}))

vi.mock('@/components/common/StateBlock', () => ({
  __esModule: true,
  default: ({ title, description }: { title?: React.ReactNode; description?: React.ReactNode }) => (
    <div>
      {title}
      {description}
    </div>
  ),
}))

describe('InstancesPage thin consumer', () => {
  beforeEach(() => {
    instanceState.instances = [
      {
        id: 'instance-a',
        status: 'running',
        project_path: '/tmp/demo.qgz',
        source: 'reverse_ws',
        services: {},
        created_at: '2026-04-07T10:00:00.000Z',
        updated_at: '2026-04-07T10:00:00.000Z',
        last_heartbeat: '2026-04-07T10:00:00.000Z',
        qgis_version: '3.34',
        pid: 1001,
      },
    ]
    instanceState.activeInstanceId = null
    instanceState.qgisAvailable = true
    instanceState.projectConnected = false
    instanceState.instanceCount = 1
    instanceState.loading = false
    instanceState.runningCount = 7
    instanceState.stale = false
    instanceState.fetchInstances.mockClear()
    instanceState.setActiveInstance.mockClear()
    instanceState.syncFromRuntimeSnapshot.mockClear()
    instanceState.handleInstanceEvent.mockClear()
    connectionState.backendReady = true
    connectionState.startupError = ''
  })

  it('uses store runningCount and does not invent an active instance fallback', () => {
    render(<InstancesPage />)

    expect(screen.getByText('共 1 个实例')).toBeInTheDocument()
    expect(screen.getByText((_, node) => node?.textContent === '运行中 7')).toBeInTheDocument()
    expect(screen.queryByText((_, node) => node?.textContent === '当前 demo · 运行中')).not.toBeInTheDocument()
  })

  it('shows plugin root resolution failures with an accurate startup title', () => {
    connectionState.backendReady = false
    connectionState.startupError = '启动后端失败: 未发现 AutoCarto 插件根目录：已检查 AUTOCARTO_HOME，并已从当前目录和可执行目录向上最多 8 层查找，要求根目录包含 skills/system'

    render(<InstancesPage />)

    expect(screen.getByTestId('alert-title')).toHaveTextContent('插件根目录解析失败')
    expect(screen.getByTestId('alert-message')).toHaveTextContent(/AUTOCARTO_HOME/)
    expect(screen.queryByText('协议版本不兼容')).not.toBeInTheDocument()
  })

  it('keeps protocol mismatch startup errors under the protocol title', () => {
    connectionState.backendReady = true
    connectionState.startupError = '协议版本不兼容，请升级客户端后重试。'

    render(<InstancesPage />)

    expect(screen.getByTestId('alert-title')).toHaveTextContent('协议版本不兼容')
    expect(screen.getByTestId('alert-message')).toHaveTextContent('协议版本不兼容，请升级客户端后重试。')
  })
})
