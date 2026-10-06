import React from 'react'
import { render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import DiagnosticsWorkspace from './DiagnosticsWorkspace'
import { useConnectionStore } from '@/stores/connectionStore'

type DiagnosticsSettingsState = {
  config: {
    data_dir: string
    log: { dir: string }
    backend: { ws_url: string }
    ports: { agent_system: { default: number } }
  }
  llmProviders: { providers: unknown[] }
  agentConfigs: unknown[]
  skills: unknown[]
  refreshConfig: ReturnType<typeof vi.fn>
  refreshLLMProviders: ReturnType<typeof vi.fn>
  refreshAgentConfigs: ReturnType<typeof vi.fn>
  refreshTools: ReturnType<typeof vi.fn>
  refreshSkills: ReturnType<typeof vi.fn>
}

type DiagnosticsRuntimeState = {
  snapshot: {
    runtime_revision: string
    tool_schema_revision: string
    tool_availability_revision: string
    services: {
      agent: { status: string; port: number; host?: string }
      qgis: { status: string; port: number }
    }
    instances: { running_count: number; active_id: string | null }
    mode_issues: Record<string, unknown>
  }
  fetchSnapshot: ReturnType<typeof vi.fn>
  loading: boolean
}

const apiMocks = vi.hoisted(() => ({
  getCapabilities: vi.fn(),
  getToolCatalog: vi.fn(),
}))

const settingsState: DiagnosticsSettingsState = vi.hoisted(() => ({
  config: {
    data_dir: 'data',
    log: { dir: 'data/logs' },
    backend: { ws_url: 'ws://127.0.0.1:3000/ws' },
    ports: { agent_system: { default: 3000 } },
  },
  llmProviders: { providers: [] },
  agentConfigs: [],
  skills: [],
  refreshConfig: vi.fn(),
  refreshLLMProviders: vi.fn(),
  refreshAgentConfigs: vi.fn(),
  refreshTools: vi.fn(),
  refreshSkills: vi.fn(),
}))

const runtimeState: DiagnosticsRuntimeState = vi.hoisted(() => ({
  snapshot: {
    runtime_revision: 'rev-1',
    tool_schema_revision: 'schema-1',
    tool_availability_revision: 'availability-1',
    services: {
      agent: { status: 'running', host: '127.0.0.1', port: 3000 },
      qgis: { status: 'running', port: 50101 },
    },
    instances: { running_count: 1, active_id: 'instance-a' },
    mode_issues: {},
  },
  fetchSnapshot: vi.fn(),
  loading: false,
}))

vi.mock('@/lib/api', () => ({
  __esModule: true,
  default: {
    getCapabilities: apiMocks.getCapabilities,
    getToolCatalog: apiMocks.getToolCatalog,
  },
}))

vi.mock('@/stores/settingsStore', () => ({
  useSettingsStore: (selector: (state: typeof settingsState) => unknown) =>
    selector(settingsState),
}))

vi.mock('@/stores/runtimeSnapshotStore', () => ({
  useRuntimeSnapshotStore: (selector: (state: typeof runtimeState) => unknown) =>
    selector(runtimeState),
}))

vi.mock('@/stores/chatStore', () => ({
  useChatStore: (selector: (state: { sessionRuntimeStates: Record<string, unknown> }) => unknown) =>
    selector({ sessionRuntimeStates: {} }),
}))

vi.mock('@/components/settings/ActionBar', () => ({
  __esModule: true,
  default: ({ left, right }: { left?: React.ReactNode; right?: React.ReactNode }) => (
    <div>
      <div>{left}</div>
      <div>{right}</div>
    </div>
  ),
  ActionButton: ({ children, onClick, disabled }: { children: React.ReactNode; onClick?: () => void; disabled?: boolean }) => (
    <button type="button" onClick={onClick} disabled={disabled}>
      {children}
    </button>
  ),
}))

vi.mock('@/components/settings/AlertPanel', () => ({
  __esModule: true,
  default: ({ title, message, evidence_id }: { title?: string; message?: React.ReactNode; evidence_id?: string }) => (
    <div>
      {title ? <div>{title}</div> : null}
      {message ? <div>{message}</div> : null}
      {evidence_id ? <div>{evidence_id}</div> : null}
    </div>
  ),
}))

vi.mock('@/components/settings/DetailList', () => ({
  __esModule: true,
  default: ({ items }: { items?: Array<{ label?: string; value?: unknown }> }) => (
    <div>
      {(items || []).map((item, index) => (
        <div key={`${item.label || 'item'}-${index}`}>
          <span>{item.label}</span>
          <span>{String(item.value ?? '')}</span>
        </div>
      ))}
    </div>
  ),
}))

vi.mock('@/components/settings/EmptyStatePanel', () => ({
  __esModule: true,
  default: ({ title }: { title?: string }) => <div>{title || 'EmptyStatePanel'}</div>,
}))

vi.mock('@/components/settings/FormSection', () => ({
  __esModule: true,
  default: ({ title, children }: { title?: string; children?: React.ReactNode }) => (
    <section>
      <h2>{title}</h2>
      {children}
    </section>
  ),
}))

vi.mock('@/components/settings/PageHeader', () => ({
  __esModule: true,
  default: ({ title }: { title?: string }) => <div>{title}</div>,
}))

vi.mock('@/components/settings/SectionNav', () => ({
  __esModule: true,
  default: () => <div>SectionNav</div>,
}))

vi.mock('@/components/settings/SummaryStrip', () => ({
  __esModule: true,
  default: ({ items }: { items?: Array<{ label?: string; value?: React.ReactNode; hint?: React.ReactNode }> }) => (
    <div>
      {(items || []).map((item, index) => (
        <div key={`${item.label || 'item'}-${index}`}>
          <span>{item.label}</span>
          <span>{item.value}</span>
          {item.hint ? <span>{item.hint}</span> : null}
        </div>
      ))}
    </div>
  ),
}))

vi.mock('@/components/runtime/SessionRuntimeSummary', () => ({
  __esModule: true,
  default: () => <div>SessionRuntimeSummary</div>,
}))

vi.mock('@/components/layout/WorkspaceShell', () => ({
  __esModule: true,
  default: ({ actionBar, children }: { actionBar?: React.ReactNode; children?: React.ReactNode }) => (
    <div>
      {actionBar}
      {children}
    </div>
  ),
}))

vi.mock('@/lib/serviceStatus', () => ({
  buildRuntimeServiceMeta: () => [],
  getRuntimeServiceStatusConfig: () => ({ label: '运行中', badgeClassName: 'ok' }),
}))

vi.mock('@/lib/sessionRuntimeDisplay', () => ({
  compareSessionRuntimeRecords: () => 0,
}))

describe('DiagnosticsWorkspace', () => {
  beforeEach(() => {
    useConnectionStore.setState({
      connected: false,
      backendReady: true,
      backendUrl: 'http://127.0.0.1:3000',
      wsUrl: 'ws://127.0.0.1:3000/ws',
      startupError: '',
      servicePorts: {},
      initializeRuntime: vi.fn(async () => {}),
      waitForBackend: vi.fn(async () => true),
      checkHealth: vi.fn(async () => false),
      refreshServicePorts: vi.fn(async () => {}),
      setConnected: vi.fn(),
      setBackendUrl: vi.fn(),
      setWsUrl: vi.fn(),
    })

    settingsState.config = {
      data_dir: 'data',
      log: { dir: 'data/logs' },
      backend: { ws_url: 'ws://127.0.0.1:3000/ws' },
      ports: { agent_system: { default: 3000 } },
    }
    runtimeState.snapshot = {
      runtime_revision: 'rev-1',
      tool_schema_revision: 'schema-1',
      tool_availability_revision: 'availability-1',
      services: {
        agent: { status: 'running', host: '127.0.0.1', port: 3000 },
        qgis: { status: 'running', port: 50101 },
      },
      instances: { running_count: 1, active_id: 'instance-a' },
      mode_issues: {},
    }
    apiMocks.getCapabilities.mockReset()
    apiMocks.getToolCatalog.mockReset()
    settingsState.refreshConfig.mockReset()
    settingsState.refreshLLMProviders.mockReset()
    settingsState.refreshAgentConfigs.mockReset()
    settingsState.refreshTools.mockReset()
    settingsState.refreshSkills.mockReset()
    runtimeState.fetchSnapshot.mockReset()

    apiMocks.getCapabilities.mockResolvedValue({
      qgis_available: true,
      instance_count: 1,
    })
    apiMocks.getToolCatalog.mockResolvedValue({
      tool_infos: [
        {
          name: 'registry_only_tool',
          description: '仅运行态注册的工具',
          available: false,
          missing_reason: 'runtime_catalog_missing',
        },
      ],
    })
    settingsState.refreshConfig.mockResolvedValue(undefined)
    settingsState.refreshLLMProviders.mockResolvedValue(undefined)
    settingsState.refreshAgentConfigs.mockResolvedValue(undefined)
    settingsState.refreshTools.mockResolvedValue(undefined)
    settingsState.refreshSkills.mockResolvedValue(undefined)
    runtimeState.fetchSnapshot.mockResolvedValue(undefined)
  })

  it('刷新时直接请求 global 工具目录', async () => {
    render(<DiagnosticsWorkspace />)

    expect(await screen.findByText('registry_only_tool')).toBeInTheDocument()
    await waitFor(() => {
      expect(apiMocks.getToolCatalog).toHaveBeenCalledTimes(1)
    })
    expect(apiMocks.getToolCatalog).toHaveBeenCalledWith('global')
  })

  it('Agent WS 仅展示 authoritative runtime 地址，不回退到本地配置默认值', async () => {
    settingsState.config = {
      data_dir: 'data',
      log: { dir: 'data/logs' },
      backend: { ws_url: '' },
      ports: { agent_system: { default: 3000 } },
    }
    runtimeState.snapshot = {
      ...runtimeState.snapshot,
      services: {
        ...runtimeState.snapshot.services,
        agent: { status: 'running', host: '127.0.0.1', port: 50321 },
      },
    }

    render(<DiagnosticsWorkspace />)

    expect(await screen.findByText('ws://127.0.0.1:50321/ws')).toBeInTheDocument()
    expect(screen.queryByText('ws://127.0.0.1:3000/ws')).not.toBeInTheDocument()
  })

  it('QGIS 环境只根据 runtime snapshot 判定，不回退 capabilities 可用性', async () => {
    runtimeState.snapshot = {
      ...runtimeState.snapshot,
      services: {
        ...runtimeState.snapshot.services,
        qgis: { status: 'unknown', port: 0 },
      },
      instances: { running_count: 0, active_id: null },
    }
    apiMocks.getCapabilities.mockResolvedValue({
      qgis_available: true,
      instance_count: 3,
    })

    render(<DiagnosticsWorkspace />)

    expect(await screen.findByText('QGIS 环境')).toBeInTheDocument()
    expect(screen.getByText('未检测到')).toBeInTheDocument()
    expect(screen.getByText('实例数量 0')).toBeInTheDocument()
    expect(screen.queryByText('可用')).not.toBeInTheDocument()
  })

  it('智能体系统端口缺失时显示缺失态，不回退到配置默认端口', async () => {
    settingsState.config = {
      data_dir: 'data',
      log: { dir: 'data/logs' },
      backend: { ws_url: '' },
      ports: { agent_system: { default: 3000 } },
    }
    runtimeState.snapshot = {
      ...runtimeState.snapshot,
      services: {
        ...runtimeState.snapshot.services,
        agent: { status: 'unknown', port: 0 },
      },
    }

    render(<DiagnosticsWorkspace />)

    await screen.findByText('registry_only_tool')
    const agentCard = screen
      .getByText('智能体系统')
      .parentElement?.parentElement?.parentElement
    expect(agentCard).not.toBeNull()
    expect(within(agentCard as HTMLElement).getByText('Port')).toBeInTheDocument()
    expect(within(agentCard as HTMLElement).getByText('-')).toBeInTheDocument()
    expect(within(agentCard as HTMLElement).queryByText('3000')).not.toBeInTheDocument()
  })

  it('在 backendReady 为 true 时仍展示 sticky 协议版本错误', async () => {
    useConnectionStore.setState({
      ...useConnectionStore.getState(),
      backendReady: true,
      startupError: '协议版本不兼容，请刷新页面。',
    })

    render(<DiagnosticsWorkspace />)

    expect(await screen.findByText('协议版本不兼容')).toBeInTheDocument()
    expect(screen.getByText('协议版本不兼容，请刷新页面。')).toBeInTheDocument()
  })

  it('工具目录降级时展示统一 degraded 字典消息与建议动作', async () => {
    apiMocks.getToolCatalog.mockResolvedValue({
      catalog: {
        id: 'tool_catalog/global',
        version: '1',
        availability: 'degraded',
        degradation_status: 'provider_missing',
        missing_dependency_reason: 'provider_missing',
        items: [],
      },
      tool_infos: [
        {
          name: 'buffer',
          description: '创建缓冲区。',
          available: false,
          missing_reason: 'provider_missing',
          evidence_id: 'EVIDENCE-provider-missing',
        },
      ],
      tools: ['buffer'],
    })

    render(<DiagnosticsWorkspace />)

    expect(await screen.findByText('buffer')).toBeInTheDocument()
    expect(screen.getAllByText('provider_missing').length).toBeGreaterThan(0)
    expect(screen.getByText('Provider 未配置，当前能力已降级。')).toBeInTheDocument()
    expect(screen.getByText('请先配置至少一个 Provider 后重试。')).toBeInTheDocument()
    expect(
      screen.getByText((content) => content.includes('EVIDENCE-') && content.includes('provider-missing')),
    ).toBeInTheDocument()
  })
})
