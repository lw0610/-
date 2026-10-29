import { beforeEach, describe, expect, it } from 'vitest'
import { applyFlowEvent } from './model'
import { useFlowRunStore } from './store'
import type { FlowEvent } from './types'

const event = (name: string, payload: Record<string, unknown> = {}, session = 's'): FlowEvent => ({ event: `flow.${name}`, session_id: session, flow_run_id: 'r', payload })
const started = event('started', { flow_id: 'demo', name: '示例', steps: [{ id: 's1', index: 1, title: '检查', role_label: '数据角色' }] })
describe('flow inputs and authoritative projection', () => {
  beforeEach(() => useFlowRunStore.setState({ runs: {}, blocked: {}, snapshotSeq: {}, eventSeq: {} }))
  it('tracks attempts, waiting, completion and delivery without tool-message inference', () => {
    let run = applyFlowEvent(undefined, started)!
    run = applyFlowEvent(run, event('step_started', { step_id: 's1', attempt: 1, run_id: 'step-a1' }))!
    run = applyFlowEvent(run, event('step_failed', { step_id: 's1', attempt: 1, errors: ['文件不存在'], next: 'retry' }))!
    run = applyFlowEvent(run, event('step_started', { step_id: 's1', attempt: 2, run_id: 'step-a2' }))!
    run = applyFlowEvent(run, event('step_waiting_user', { step_id: 's1', kind: 'failure_choice' }))!
    expect(run.steps[0]).toMatchObject({ status: 'waiting_user', awaiting_choice: true, last_failure: '文件不存在' })
    run = applyFlowEvent(run, event('step_completed', { step_id: 's1', summary: '已检查', outputs: { 文件: 'outputs/map.pdf' }, verification: { ok: true, errors: [] } }))!
    expect(applyFlowEvent(run, event('step_started', { step_id: 's1', attempt: 1, run_id: 'step-a1' }))).toBe(run)
    run = applyFlowEvent(run, event('finished', { status: 'completed', deliverables: { 图件: 'outputs/map.pdf' }, issues: [], run_json_path: 'flows/r/run.json' }))!
    expect(run).toMatchObject({ status: 'completed', result: { deliverables: { 图件: 'outputs/map.pdf' } }, steps: [{ status: 'completed', attempts: [{ run_id: 'step-a1' }, { run_id: 'step-a2' }] }] })
  })
  it('restores exact run-state fields, ignores covered replay and isolates sessions', () => {
    const store = useFlowRunStore.getState()
    const state = applyFlowEvent(undefined, started)!
    state.steps[0].status = 'waiting_user'
    store.restore('s', { ...state, event_seq: 12, inputs: { source: 'exact' }, future: 'keep' }, 12)
    store.applyEvent(event('step_started', { step_id: 's1', run_id: 'old', attempt: 1 }), 11, true)
    expect(useFlowRunStore.getState().runs.s[0].steps[0].status).toBe('waiting_user')
    store.applyEvent(event('step_completed', { step_id: 's1', summary: '完成', outputs: {} }), 13, true)
    store.restore('s', state, 12)
    expect(useFlowRunStore.getState().runs.s[0].steps[0].status).toBe('completed')
    store.applyEvent({ ...started, session_id: 'other', flow_run_id: 'r-other' })
    expect(useFlowRunStore.getState().runs.other[0].session_id).toBe('other')
    expect(useFlowRunStore.getState().runs.s[0].future).toBe('keep')
    store.restore('s', undefined)
    expect(useFlowRunStore.getState().runs.s).toHaveLength(1)
    store.restore('s', null)
    expect(useFlowRunStore.getState().runs.s).toEqual([])
    expect(useFlowRunStore.getState().runs.other).toHaveLength(1)
  })
  it('takes final step states from a direct terminal event without waiting for a session reload', () => {
    const run = applyFlowEvent(applyFlowEvent(undefined, started)!, event('step_started', { step_id: 's1', run_id: 'step-a1' }))!
    const finished = applyFlowEvent(run, event('finished', { status: 'failed', steps: [{ ...run.steps[0], status: 'failed', last_failure: '模型服务额度不足' }] }))!
    expect(finished.status).toBe('failed')
    expect(finished.steps[0]).toMatchObject({ status: 'failed', last_failure: '模型服务额度不足' })
    expect(finished.result).not.toHaveProperty('steps')
  })
  it('does not duplicate a started run when its live mirror arrives', () => {
    const store = useFlowRunStore.getState()
    store.applyEvent(started)
    store.applyEvent(started, 3)
    expect(useFlowRunStore.getState().runs.s).toHaveLength(1)
    expect(useFlowRunStore.getState().eventSeq.s).toBe(3)
    store.applyEvent(event('blocked', { flow_id: 'bad', reasons: [{ code: 'MISSING', message: '缺少数据' }] }))
    expect(useFlowRunStore.getState().blocked.s?.reasons[0].message).toBe('缺少数据')
  })
})

it('projects new data-service progress by row without step ids and keeps terminal state',()=>{
 let run=applyFlowEvent(undefined,started)!
 run=applyFlowEvent(run,event('acquisition_progress',{row:'河流',stage:'downloading',bytes:100,total:1000,bytes_per_second:20}))!
 run=applyFlowEvent(run,event('acquisition_progress',{row:'铁路',stage:'downloading',bytes:200,total:0,bytes_per_second:30}))!
 expect(run.acquisition?.items).toHaveLength(2)
 run=applyFlowEvent(run,event('acquisition_progress',{row:'河流',stage:'completed',bytes:1000,total:1000}))!
 run=applyFlowEvent(run,event('acquisition_progress',{row:'河流',stage:'downloading',bytes:300,total:1000}))!
 expect(run.acquisition?.items.find(item=>item.key==='河流')?.status).toBe('completed')
 expect(run.acquisition?.items.find(item=>item.key==='铁路')?.bytes).toBe(200)
})
