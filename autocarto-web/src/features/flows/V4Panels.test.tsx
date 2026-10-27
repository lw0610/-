import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import RoleMappingPanel from './RoleMappingPanel'
import DataRequirementsPanel from './DataRequirementsPanel'
import { StepPreview } from './PreviewPanels'
import FlowPolicySettings from '@/components/settings/FlowPolicySettings'
import { flowReadiness } from '@/components/settings/AgentFlowSelection'
import {
  normalizeAgentProfilePayload,
  serializeAgentProfilePayload,
} from '@/lib/api'
import type { FlowPlan, FlowSummary, RoleMappingResponse } from './types'
const mocks = vi.hoisted(() => ({
  roles: vi.fn(),
  setRoles: vi.fn(),
  list: vi.fn(),
}))
vi.mock('./api', () => ({ flowApi: mocks }))
const response: RoleMappingResponse = {
  flow_id: 'demo',
  scheme: 'selected',
  role_fill: 'auto',
  role_mapping: {},
  role_slots: [
    {
      name: '制图',
      packages: ['layout'],
      resolution: {
        source: 'flow_role',
        role_id: 'local',
        role_label: '流程制图',
      },
    },
  ],
  scheme_roles: [
    {
      id: 'map-custom',
      display_name: '方案制图',
      kind: 'agent',
      enabled: true,
      is_main: false,
      tool_packages: ['layout'],
    },
  ],
  flow_roles: [
    {
      id: 'local',
      slot: '制图',
      display_name: '流程制图',
      kind: 'agent',
      tool_packages: ['layout'],
    },
  ],
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.roles.mockResolvedValue(structuredClone(response))
  mocks.setRoles.mockImplementation(async (_id, _scheme, mapping) => ({
    ...response,
    role_mapping: mapping,
  }))
})
it('saves role mapping to the selected scheme and restores automatic resolution', async () => {
  const saved = vi.fn().mockResolvedValue(undefined)
  render(<RoleMappingPanel id="demo" scheme="selected" onSaved={saved} />)
  fireEvent.change(await screen.findByRole('combobox'), {
    target: { value: 'map-custom' },
  })
  fireEvent.click(screen.getByRole('button', { name: '保存角色对应' }))
  await waitFor(() =>
    expect(mocks.setRoles).toHaveBeenCalledWith('demo', 'selected', {
      制图: 'map-custom',
    }),
  )
  await waitFor(() => expect(saved).toHaveBeenCalledTimes(1))
  fireEvent.click(screen.getByRole('button', { name: '恢复全部自动' }))
  await waitFor(() =>
    expect(mocks.setRoles).toHaveBeenLastCalledWith('demo', 'selected', {}),
  )
})
it('does not replace the selected scheme with a late mapping response', async () => {
  let resolve!: (value: RoleMappingResponse) => void
  mocks.roles.mockImplementation((_id, scheme) =>
    scheme === 'old'
      ? new Promise((r) => {
          resolve = r
        })
      : Promise.resolve({ ...response, role_mapping: { 制图: 'map-custom' } }),
  )
  const { rerender } = render(
    <RoleMappingPanel id="demo" scheme="old" onSaved={async () => {}} />,
  )
  rerender(
    <RoleMappingPanel id="demo" scheme="selected" onSaved={async () => {}} />,
  )
  expect(await screen.findByRole('combobox')).toHaveValue('map-custom')
  resolve(response)
  await waitFor(() =>
    expect(screen.getByRole('combobox')).toHaveValue('map-custom'),
  )
})
it('disables mapping writes while a flow draft has unsaved changes', async () => {
  render(
    <RoleMappingPanel
      id="demo"
      scheme="selected"
      disabled
      onSaved={async () => {}}
    />,
  )
  expect(await screen.findByRole('combobox')).toBeDisabled()
  expect(screen.getByRole('button', { name: '保存角色对应' })).toBeDisabled()
})
const plan: FlowPlan = {
  api_version: 'flow-plan/v1',
  flow_id: 'demo',
  name: 'demo',
  scheme: 'selected',
  inputs: [],
  resources: [],
  steps: [],
  requirements: [
    {
      name: '底图',
      list: '数据清单',
      extent: '全国',
      scale: '自动',
      params: {},
      fields: { NAME: 'name' },
      recommended_sources: [],
      excluded_sources: [],
      acquisition: '系统',
    },
  ],
  acquisition_plan: {
    items: [
      {
        key: 'a',
        requirement: '底图',
        entry: '未分类数据',
        category: '',
        params: {},
        fields: {},
        input: 'folder',
        required: true,
        acquirable: false,
        available: false,
        mode: 'user',
        alternatives: [],
        excluded: [],
        reasons: [],
        notes: [],
        scale_denominator: 0,
      },
    ],
  },
}
it('shows user-only rows and field guidance without editable acquisition choices', () => {
  render(<DataRequirementsPanel plan={plan} />)
  expect(screen.getByText(/该行只能由用户提供/)).toBeInTheDocument()
  expect(
    screen.getByRole('link', { name: '查看清单编辑说明' }),
  ).toHaveAttribute('href', '#data-list-guide')
  expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
  expect(screen.getByText('字段要求：NAME：name')).toBeInTheDocument()
})
it('previews role origins, temporary packages and conditional system acquisition', () => {
  render(
    <StepPreview
      plan={{
        ...plan,
        steps: [
          {
            id: 'a',
            index: 1,
            title: '制图',
            kind: 'agent',
            role_label: '制图',
            role_source: 'mapping',
            granted_packages: ['layout'],
            instructions: '制图',
            on_failure: 'stop',
          },
          {
            id: 'b',
            index: 2,
            title: '审查',
            kind: 'agent',
            role_source: 'unresolved',
            unresolved: 'FLOW_ROLE_VISION_UNAVAILABLE',
            condition: { text: '需要审查' },
            instructions: '审查',
            on_failure: 'ask',
          },
          {
            id: 'c',
            index: 3,
            title: '获取数据',
            kind: 'auto',
            acquisition_requirement: '底图',
            instructions: '获取底图',
            on_failure: 'ask',
          },
        ],
      }}
      capabilities={[
        {
          name: 'export',
          package_id: 'layout',
          package_display_name: '版面输出',
        },
      ]}
    />,
  )
  expect(screen.getByText('临时补齐：版面输出')).toBeInTheDocument()
  expect(screen.getByText(/仅在条件满足时需要/)).toBeInTheDocument()
  expect(screen.getByText(/仅在自动获取或补齐时执行/)).toBeInTheDocument()
})
it('changes policy without losing role mappings or unknown persisted fields', () => {
  const value = {
      role_fill: 'auto' as const,
      auto_acquire: 'ask' as const,
      role_mapping: { demo: { 制图: 'map-custom' } },
      future: 42,
    },
    change = vi.fn()
  render(<FlowPolicySettings value={value} onChange={change} />)
  fireEvent.change(
    screen.getByRole('combobox', { name: '对话中流程缺少数据时' }),
    { target: { value: 'deny' } },
  )
  expect(change).toHaveBeenCalledWith({ ...value, auto_acquire: 'deny' })
  const raw = {
    version: '2.0',
    runtime_agents: [],
    runtime_shared: {},
    has_plan: false,
    flow_policy: { ...value, auto_acquire: 'deny' as const },
  }
  expect(
    serializeAgentProfilePayload(normalizeAgentProfilePayload(raw)).flow_policy,
  ).toEqual(raw.flow_policy)
})
it('distinguishes augmentation, conditional restrictions and errors without warning counts', () => {
  const base = {
    diagnostics_summary: { errors: 0, warnings: 99, infos: 0 },
    roles: {
      scheme: 1,
      augmented: 1,
      flow_role: 1,
      executor_default: 0,
      unresolved: 0,
      conditional_unresolved: 0,
    },
  } as FlowSummary
  expect(flowReadiness(base)).toMatchObject({
    label: '自动补齐 2 项',
    tab: 'roles',
  })
  expect(
    flowReadiness({
      ...base,
      roles: { ...base.roles!, conditional_unresolved: 1 },
    }),
  ).toMatchObject({ label: '部分步骤有条件限制' })
  expect(
    flowReadiness({ ...base, roles: { ...base.roles!, unresolved: 1 } }),
  ).toMatchObject({ label: '有错误' })
})
