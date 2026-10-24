import { beforeEach, describe, expect, it } from 'vitest'
import { applyFlowEvent, choiceValues, inputDefaults, normalizeFlowRun, prepareFlowSubmission } from './model'
import { useFlowRunStore } from './store'
import { parseFlowPreview } from './FlowPreviewCard'
import type { FlowEvent } from './types'
import type { FlowInput } from '@/features/flows/types'

const event = (name: string, payload: Record<string, unknown> = {}, session = 's'): FlowEvent => ({ event: `flow.${name}`, session_id: session, flow_run_id: 'r', payload })
const started = event('started', { flow_id: 'demo', name: '示例', steps: [{ id: 's1', index: 1, title: '检查', role_label: '数据角色' }] })
describe('flow inputs and authoritative projection', () => {
  beforeEach(() => useFlowRunStore.setState({ runs: {}, blocked: {}, snapshotSeq: {}, eventSeq: {} }))
  it('uses the original-message reference and preserves typed false and zero inputs', () => {
    const inputs: FlowInput[] = [
      { key: 'source', label: '原文', type: 'conversation_text', required: true },
      { key: 'paper', label: '纸张', type: 'choice', required: true, default: 'A1', options: ['A0', 'A2'] },
      { key: 'review', label: '审查', type: 'boolean', required: true },
      { key: 'count', label: '数量', type: 'number', required: true },
      { key: 'file', label: '文件', type: 'file', required: false },
    ]
    expect(choiceValues(inputs[1])).toEqual(['A1', 'A0', 'A2'])
    const result = prepareFlowSubmission('demo', inputs, { ...inputDefaults(inputs), count: '0' })
    expect(result.errors).toEqual([])
    expect(result.flow).toEqual({ id: 'demo', inputs: { source: { $ref: 'current_user_message' }, paper: 'A1', review: false, count: 0 } })
    expect(prepareFlowSubmission('demo', inputs, { count: 'NaN', paper: 'wrong' }).errors).toEqual(['请为纸张选择有效选项', '请填写审查', '数量需要有效数字'])
  })
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
  it('keeps production null defaults empty and omits optional values from the task', () => {
    const inputs: FlowInput[] = [
      { key: 'source', label: '原文', type: 'conversation_text', required: true, default: null },
      { key: 'dataset', label: '数据集', type: 'folder', required: false, default: null },
      { key: 'title', label: '标题', type: 'text', required: false, default: null },
      { key: 'count', label: '数量', type: 'number', required: false, default: null },
      { key: 'review', label: '审查', type: 'boolean', required: false, default: null },
    ]
    const defaults = inputDefaults(inputs)
    expect(defaults).toEqual({ review: false })
    expect(prepareFlowSubmission('demo', inputs, defaults)).toEqual({ errors: [], flow: { id: 'demo', inputs: { source: { $ref: 'current_user_message' }, review: false } } })
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
  it('does not duplicate a started run when its live mirror arrives', () => {
    const store = useFlowRunStore.getState()
    store.applyEvent(started)
    store.applyEvent(started, 3)
    expect(useFlowRunStore.getState().runs.s).toHaveLength(1)
    expect(useFlowRunStore.getState().eventSeq.s).toBe(3)
    store.applyEvent(event('blocked', { flow_id: 'bad', reasons: [{ code: 'MISSING', message: '缺少数据' }] }))
    expect(useFlowRunStore.getState().blocked.s?.reasons[0].message).toBe('缺少数据')
  })
  it('rejects unrelated preview output and accepts the agreed tool wrapper', () => {
    expect(parseFlowPreview('not json')).toBeNull()
    const plan = { flow_id: 'demo', name: '示例', inputs: [], steps: [], resources: [] }
    expect(parseFlowPreview(JSON.stringify({ status: 'ok', data: { flow_id: 'demo', plan, input_check: { valid: false, missing: ['数据'] } } }))).toMatchObject({ flow_id: 'demo', input_check: { missing: ['数据'] } })
    expect(normalizeFlowRun({ flow_id: 'demo' })).toBeNull()
  })
})
