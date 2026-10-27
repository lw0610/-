import React from 'react'
import {
  act,
  fireEvent,
  render,
  renderHook,
  screen,
  waitFor,
} from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import { useFlowComposer } from './useFlowComposer'
import FlowComposer from './FlowComposer'
import AcquisitionProgress from './AcquisitionProgress'
import InterruptCard from '@/components/chat/InterruptCard'
import { useChatStore } from '@/stores/chatStore'
import { useConnectionStore } from '@/stores/connectionStore'
import { useSessionStore } from '@/stores/sessionStore'
import { useInstanceStore } from '@/stores/instanceStore'
import { applyFlowEvent, prepareFlowSubmission } from './model'
import { changeItemChoice } from './acquisition'
import type {
  AcquisitionItem,
  FlowDetail,
  InputCheckPlan,
} from '@/features/flows/types'
const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  get: vi.fn(),
  inputMemory: vi.fn(),
  checkInputPlan: vi.fn(),
  checkInputs: vi.fn(),
  file: vi.fn(),
}))
vi.mock('@/features/flows/api', () => ({ flowApi: mocks }))
vi.mock('@/lib/config-form-helpers', () => ({
  isDesktopRuntime: () => true,
  pickFilePath: mocks.file,
  pickDirectoryPath: vi.fn(),
}))
const item: AcquisitionItem = {
  key: 'base:roads',
  requirement: '底图',
  entry: '道路',
  category: 'transport.road',
  input: 'base',
  input_type: 'folder',
  required: true,
  acquirable: true,
  available: true,
  mode: 'auto',
  params: {},
  fields: {},
  scale_denominator: 100000,
  extent: { pending: false, region: '甲地' },
  chosen: { source_id: 'source.a', name: '来源甲', tier: 1, license: 'CC0' },
  alternatives: [
    { source_id: 'source.b', name: '来源乙', tier: 2, license: 'ODbL' },
  ],
  excluded: [],
  notes: [],
  reasons: [],
}
it('在线底图定义等待确认，不误显示为下载完成或普通等待', () => {
  let run = applyFlowEvent(undefined, { event: 'flow.acquire_item_done', session_id: 's', flow_run_id: 'r', payload: { key: 'tiles', status: 'waiting_user', output: { paths: ['/tmp/tiles.layer.json'], layer_definition: { type: 'tianditu_xyz' } } } })!
  run = applyFlowEvent(run, { event: 'flow.acquire_confirmation', session_id: 's', flow_run_id: 'r', payload: { key: 'tiles', required: true } })!
  expect(run.status).toBe('waiting_user')
  expect(run.acquisition?.items[0].status).toBe('waiting_user')
  render(<AcquisitionProgress state={run.acquisition!}/>)
  expect(screen.getByText('等待加载确认')).toBeInTheDocument()
  expect(screen.queryByText('waiting_user')).not.toBeInTheDocument()
})
it('单文件手动提供改为普通路径，换来源清除同一清单行的手动文件',()=>{
  expect(changeItemChoice({$acquire:true},{...item,input_type:'file',resource:undefined},'file','/tmp/roads.gpkg')).toBe('/tmp/roads.gpkg')
  expect(changeItemChoice({$fill:true,path:'/tmp/base',files:{道路:'/tmp/manual.gpkg',其他:'/tmp/other.gpkg'}},item,'source','source.b')).toEqual({$fill:true,path:'/tmp/base',files:{其他:'/tmp/other.gpkg'},sources:{道路:'source.b'}})
})
it('启动事件保留完整计划，使获取进度能归入系统步骤',()=>{
  const plan={...detail.plan!,steps:[{id:'acquire',index:1,title:'获取数据',kind:'auto',instructions:'获取底图',acquisition_requirement:'底图',on_failure:'ask'}]}
  const run=applyFlowEvent(undefined,{event:'flow.started',session_id:'s',flow_run_id:'r',payload:{flow_id:'demo',steps:plan.steps,plan}})!
  expect(run.plan?.steps[0].acquisition_requirement).toBe('底图')
  const started=applyFlowEvent(run,{event:'flow.acquire_item_started',session_id:'s',flow_run_id:'r',payload:{key:item.key,source_id:'source.a'}})!
  expect(started.acquisition?.items[0]).toMatchObject({key:item.key,status:'running'})
})
const detail: FlowDetail = {
  meta: { id: 'demo', name: '自动获取流程', version: '1' },
  source: 'user',
  flow_md: '',
  resources: [],
  diagnostics: [],
  plan: {
    api_version: 'flow-plan/v1',
    flow_id: 'demo',
    name: '自动获取流程',
    scheme: 'default',
    inputs: [
      {
        key: 'base',
        label: '底图文件夹',
        type: 'folder',
        required: true,
        remember: true,
        acquirable: true,
        acquire_available: true,
        modes: ['user', 'auto', 'fill'],
      },
    ],
    steps: [],
    resources: [],
    acquisition_plan: { items: [item] },
  },
}
beforeEach(() => {
  vi.clearAllMocks()
  window.history.replaceState({}, '', '/chat?flow=demo')
  mocks.list.mockResolvedValue([
    { id: 'demo', name: '自动获取流程', enabled: true },
  ])
  mocks.get.mockResolvedValue(detail)
  mocks.inputMemory.mockResolvedValue({})
  mocks.checkInputPlan.mockResolvedValue({
    problems: [],
    acquisition_plan: { items: [item] },
  })
  mocks.checkInputs.mockResolvedValue([])
  mocks.file.mockResolvedValue('/tmp/roads.geojson')
  useConnectionStore.setState({ backendReady: true })
  useChatStore.setState({
    activeAgentConfig: 'default',
    executionMode: 'build',
    sessionRuntimeStates: {
      sid: { session_id: 'sid', bound_instance_id: 'bound' },
    },
  })
  useSessionStore.setState({ activeSessionId: 'sid' })
  useInstanceStore.setState({ activeInstanceId: 'bound' })
})
it('无默认文件时预选自动，记忆中的补齐选择保持对象结构', async () => {
  mocks.inputMemory.mockResolvedValue({
    base: { $fill: true, path: '/provided', sources: { 道路: 'source.b' } },
  })
  const hook = renderHook(() => useFlowComposer(vi.fn(async () => true)))
  await waitFor(() =>
    expect(hook.result.current.values.base).toEqual({
      $fill: true,
      path: '/provided',
      sources: { 道路: 'source.b' },
    }),
  )
  expect(
    prepareFlowSubmission(
      'demo',
      detail.plan!.inputs,
      hook.result.current.values,
    ).flow.inputs?.base,
  ).toEqual(hook.result.current.values.base)
})
it('默认自动获取、换来源与单项文件写入发送对象', async () => {
  const send = vi.fn(async () => true)
  const hook = renderHook(() => useFlowComposer(send))
  await waitFor(() => expect(hook.result.current.blocked).toBe(false))
  expect(hook.result.current.values.base).toEqual({ $acquire: true })
  act(() =>
    hook.result.current.setValue(
      'base',
      changeItemChoice(
        hook.result.current.values.base,
        item,
        'source',
        'source.b',
      ),
    ),
  )
  act(() =>
    hook.result.current.setValue(
      'base',
      changeItemChoice(
        hook.result.current.values.base,
        item,
        'file',
        '/tmp/roads.geojson',
      ),
    ),
  )
  await waitFor(() => expect(hook.result.current.blocked).toBe(false))
  await act(async () => {
    await hook.result.current.send('制作地图')
  })
  expect(send).toHaveBeenCalledWith('制作地图', {
    id: 'demo',
    inputs: {
      base: {
        $acquire: true,
        sources: { 道路: 'source.b' },
        files: { 道路: '/tmp/roads.geojson' },
      },
    },
  })
})
it.each(['mode', 'source', 'file'])(
  '改变 %s 后丢弃进行中的发送前检查',
  async (kind) => {
    const send = vi.fn(async () => true)
    const hook = renderHook(() => useFlowComposer(send))
    await waitFor(() => expect(hook.result.current.blocked).toBe(false))
    let resolve!: (result: InputCheckPlan) => void
    mocks.checkInputPlan.mockReturnValueOnce(
      new Promise<InputCheckPlan>((done) => {
        resolve = done
      }),
    )
    let pending: Promise<boolean | void>
    act(() => {
      pending = hook.result.current.send('保留原文')
    })
    act(() =>
      hook.result.current.setValue(
        'base',
        kind === 'mode'
          ? { $fill: true, path: '/provided' }
          : changeItemChoice(
              hook.result.current.values.base,
              item,
              kind === 'source' ? 'source' : 'file',
              kind === 'source' ? 'source.b' : '/tmp/file',
            ),
      ),
    )
    await act(async () => {
      resolve({ problems: [], acquisition_plan: { items: [item] } })
      await pending
    })
    expect(send).not.toHaveBeenCalled()
  },
)
it('表单切换来源时同步文件夹与计划，不把对象显示成文本', async () => {
  function Form() {
    const composer = useFlowComposer(vi.fn(async () => true))
    return <FlowComposer composer={composer} mode="build" />
  }
  render(<Form />)
  const select = await screen.findByRole('combobox', { name: '底图文件夹来源' })
  expect(select).toHaveValue('auto')
  expect(screen.getByRole('region', { name: '数据获取计划' })).toBeVisible()
  fireEvent.change(select, { target: { value: 'fill' } })
  const path = screen.getByRole('textbox', { name: '底图文件夹（必填）' })
  fireEvent.change(path, { target: { value: '/provided' } })
  expect(path).toHaveValue('/provided')
  expect(screen.queryByDisplayValue('[object Object]')).not.toBeInTheDocument()
})
it('获取进度事件无 step_id 也能保存状态、来源、字节和文件', () => {
  let state = applyFlowEvent(undefined, {
    event: 'flow.acquire_planned',
    session_id: 'sid',
    flow_run_id: 'r',
    payload: { plan: { items: [item] } },
  })!
  state = applyFlowEvent(state, {
    event: 'flow.acquire_progress',
    session_id: 'sid',
    flow_run_id: 'r',
    payload: { key: item.key, stage: 'downloading', bytes: 50, total: 100 },
  })!
  expect(state.acquisition?.items[0]).toMatchObject({
    status: 'downloading',
    bytes: 50,
    total: 100,
  })
  state = applyFlowEvent(state, {
    event: 'flow.acquire_item_done',
    session_id: 'sid',
    flow_run_id: 'r',
    payload: {
      key: item.key,
      status: 'reused',
      source_id: 'source.a',
      output: { paths: ['/tmp/roads.gpkg'] },
      notes: ['必须署名'],
    },
  })!
  expect(state.acquisition?.items[0]).toMatchObject({
    status: 'reused',
    source_id: 'source.a',
    output: { paths: ['/tmp/roads.gpkg'] },
    notes: ['必须署名'],
  })
})
it('获取失败选择我来提供时要求路径，并通过既有确认通道发送 guidance', async () => {
  const approve = vi.fn()
  render(
    <InterruptCard
      interrupt={{
        checkpoint_id: 'acq',
        interrupt_contexts: [
          {
            is_root_cause: true,
            info: {
              type: 'decision',
              category: 'flow_acquisition',
              select_mode: 'single',
              required: true,
              options: [
                { label: '重试', value: 'retry' },
                { label: '我来提供', value: 'provide' },
                { label: '停止', value: 'stop' },
              ],
            },
          },
        ],
      }}
      onApprove={approve}
      onReject={vi.fn()}
    />,
  )
  fireEvent.click(screen.getByRole('radio', { name: '我来提供' }))
  expect(screen.getByRole('button', { name: '确认并继续' })).toBeDisabled()
  fireEvent.click(screen.getByRole('button', { name: '选择数据文件' }))
  await waitFor(() =>
    expect(screen.getByLabelText('提供数据文件路径')).toHaveValue(
      '/tmp/roads.geojson',
    ),
  )
  fireEvent.click(screen.getByRole('button', { name: '确认并继续' }))
  expect(approve).toHaveBeenCalledWith(
    expect.objectContaining({
      option: 'provide',
      guidance: '/tmp/roads.geojson',
    }),
  )
  expect(screen.queryByText('自定义回答')).not.toBeInTheDocument()
  expect(screen.queryByRole('radio', { name: '跳过' })).not.toBeInTheDocument()
})
