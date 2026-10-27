import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import { useFlowComposer } from './useFlowComposer'
import type { FlowDetail, FlowInput, FlowPlan } from '@/features/flows/types'
import { useChatStore } from '@/stores/chatStore'
import { useConnectionStore } from '@/stores/connectionStore'
import { useSessionStore } from '@/stores/sessionStore'
import { useInstanceStore } from '@/stores/instanceStore'

const mocks = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), inputMemory: vi.fn(), checkInputs: vi.fn() }))
vi.mock('@/features/flows/api', () => ({ flowApi: mocks }))

const table: FlowInput = { key: 'table', label: '统计表', type: 'file', required: false, remember: true }
function definition(inputs: FlowInput[], policy?: FlowPlan['auto_acquire']): FlowDetail {
  return {
    meta: { id: 'statistics', name: '统计专题图', version: '3' }, source: 'builtin', flow_md: '', resources: [], diagnostics: [],
    plan: { api_version: 'flow-plan/v1', flow_id: 'statistics', name: '统计专题图', scheme: 'default', inputs, steps: [], resources: [], auto_acquire: policy },
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  window.history.replaceState({}, '', '/chat?flow=statistics')
  mocks.list.mockResolvedValue([{ id: 'statistics', name: '统计专题图', enabled: true }])
  mocks.get.mockResolvedValue(definition([table]))
  mocks.inputMemory.mockResolvedValue({})
  mocks.checkInputs.mockResolvedValue([])
  useConnectionStore.setState({ backendReady: true })
  useChatStore.setState({ activeAgentConfig: 'default', executionMode: 'build', sessionRuntimeStates: {} })
  useSessionStore.setState({ activeSessionId: null })
  useInstanceStore.setState({ activeInstanceId: null })
})

it('does not restore a removed auto-acquisition flag or block the new browser acquisition step', async () => {
  mocks.inputMemory.mockResolvedValue({ table: { $acquire: true, source: 'old-statistics-source' } })
  const send = vi.fn(async () => true)
  const hook = renderHook(() => useFlowComposer(send))
  await waitFor(() => expect(hook.result.current.blocked).toBe(false))
  expect(hook.result.current.values).not.toHaveProperty('table')
  expect(mocks.checkInputs).toHaveBeenLastCalledWith('statistics', 'default', {})
  await act(async () => { await hook.result.current.send('制作统计专题图') })
  expect(send).toHaveBeenCalledWith('制作统计专题图', { id: 'statistics', inputs: {} })
})

it.each([
  { name: 'disabled availability', input: { ...table, acquirable: true, acquire_available: false }, policy: undefined },
  { name: 'denied policy', input: { ...table, acquirable: true, acquire_available: true }, policy: 'deny' as const },
  { name: 'removed auto mode', input: { ...table, acquirable: true, acquire_available: true, modes: ['user'] as FlowInput['modes'], default: '/default.csv' }, policy: undefined },
])('drops stale auto flags for $name while preserving the current default', async ({ input, policy }) => {
  mocks.get.mockResolvedValue(definition([input], policy))
  mocks.inputMemory.mockResolvedValue({ table: { $acquire: true } })
  const hook = renderHook(() => useFlowComposer(vi.fn(async () => true)))
  await waitFor(() => expect(hook.result.current.blocked).toBe(false))
  expect(hook.result.current.values.table).toBe(input.default)
})

it('preserves a local path when remembered fill mode is no longer allowed', async () => {
  mocks.get.mockResolvedValue(definition([{ ...table, type: 'folder', acquirable: true, acquire_available: true, modes: ['user', 'auto'] }]))
  mocks.inputMemory.mockResolvedValue({ table: { $fill: true, path: '/provided-data', sources: { roads: 'old-source' } } })
  const hook = renderHook(() => useFlowComposer(vi.fn(async () => true)))
  await waitFor(() => expect(hook.result.current.values.table).toBe('/provided-data'))
})

it('keeps supported acquisition choices and ordinary remembered values unchanged', async () => {
  const fill = { $fill: true, path: '/base', sources: { roads: 'osm' }, files: { places: '/places.geojson' } }
  const data = { path: '/table.csv', metadata: { year: 2025 } }
  mocks.get.mockResolvedValue(definition([
    { ...table, key: 'auto', acquirable: true, acquire_available: true, modes: ['user', 'auto'] },
    { ...table, key: 'fill', type: 'folder', acquirable: true, acquire_available: true, modes: ['user', 'auto', 'fill'] },
    { ...table, key: 'file' }, { ...table, key: 'data' }, { ...table, key: 'notRemembered', remember: false },
  ]))
  mocks.inputMemory.mockResolvedValue({ auto: { $acquire: true, source: 'official' }, fill, file: '/real.csv', data, obsolete: '/stale.csv', notRemembered: '/other.csv' })
  const hook = renderHook(() => useFlowComposer(vi.fn(async () => true)))
  await waitFor(() => expect(hook.result.current.values.file).toBe('/real.csv'))
  expect(hook.result.current.values).toEqual({ auto: { $acquire: true, source: 'official' }, fill, file: '/real.csv', data })
})

it('does not replace a current user selection when stale input memory arrives late', async () => {
  let resolveMemory!: (value: Record<string, unknown>) => void
  mocks.inputMemory.mockReturnValueOnce(new Promise<Record<string, unknown>>(resolve => { resolveMemory = resolve }))
  const hook = renderHook(() => useFlowComposer(vi.fn(async () => true)))
  await waitFor(() => expect(hook.result.current.detail?.plan?.inputs).toEqual([table]))
  act(() => hook.result.current.setValue('table', '/current.csv'))
  await act(async () => resolveMemory({ table: { $fill: true, path: '/previous.csv' } }))
  expect(hook.result.current.values.table).toBe('/current.csv')
})
