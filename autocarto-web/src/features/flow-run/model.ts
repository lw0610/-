import type { FlowInput } from '@/features/flows/types'
import type { FlowEvent, FlowRunState, FlowStepState, FlowSubmission } from './types'
import { acquisitionChoice, canAcquire, sourceMode } from './acquisition'

export const record = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
export const terminalFlow = (status: string) => ['completed', 'completed_with_issues', 'failed', 'cancelled'].includes(status)
export const choiceValues = (input: FlowInput) => [...new Set([
  ...(input.default !== undefined && input.default !== null ? [String(input.default)] : []),
  ...(input.options || []),
])]
export function inputDefaults(inputs: FlowInput[], policy?: string) {
  return Object.fromEntries(inputs.filter(input => input.type !== 'conversation_text' && (input.default != null || input.type === 'boolean' || canAcquire(input, policy)))
    .map(input => [input.key, input.default ?? (canAcquire(input, policy) ? {$acquire: true} : false)]))
}
export function prepareFlowSubmission(id: string, inputs: FlowInput[], values: Record<string, unknown>): { flow: FlowSubmission; errors: string[] } {
  const result: Record<string, unknown> = {}, errors: string[] = []
  for (const input of inputs) {
    if (input.type === 'conversation_text') {
      result[input.key] = { $ref: 'current_user_message' }
      continue
    }
    const value = values[input.key] === null ? null : values[input.key] ?? input.default
    if (value === undefined || value === null || typeof value === 'string' && !value.trim()) {
      if (input.required) errors.push(`请填写${input.label}`)
      continue
    }
    const mode = sourceMode(value)
    if (mode === 'auto' || mode === 'fill') {
      const choice = acquisitionChoice(value)
      if (!input.acquirable || input.modes && !input.modes.includes(mode)) errors.push(`${input.label}当前不允许自动获取`)
      else if (mode === 'fill' && !choice.path?.trim()) errors.push(`请选择${input.label}需要补齐的文件夹`)
      else result[input.key] = {...choice}
    } else if (input.type === 'number') {
      const number = Number(value)
      if (!Number.isFinite(number)) errors.push(`${input.label}需要有效数字`)
      else result[input.key] = number
    } else if (input.type === 'boolean') {
      result[input.key] = value === true || value === 'true'
    } else if (input.type === 'choice' && !choiceValues(input).includes(String(value))) {
      errors.push(`请为${input.label}选择有效选项`)
    } else result[input.key] = value
  }
  return { flow: { id, inputs: result }, errors }
}
export function normalizeFlowRun(value: unknown, sessionId?: string): FlowRunState | null {
  const raw = record(value)
  const id = String(raw.flow_run_id || '').trim(), sid = String(raw.session_id || sessionId || '').trim()
  if (!id || !sid) return null
  const result = record(raw.result)
  return { ...raw, api_version: String(raw.api_version || 'flow-run/v1'), flow_run_id: id, session_id: sid,
    flow_id: String(raw.flow_id || ''), status: String(raw.status || 'running'), started_at: String(raw.started_at || ''),
    steps: Array.isArray(raw.steps) ? raw.steps.map((value, index) => {
      const step = record(value)
      return { ...step, id: String(step.id || ''), index: Number(step.index || index + 1), title: String(step.title || step.id || ''),
        role: String(step.role || step.role_label || ''), status: String(step.status || 'pending'),
        attempts: Array.isArray(step.attempts) ? step.attempts : [], outputs: record(step.outputs) } as FlowStepState
    }) : [],
    result: { ...result, deliverables: record(result.deliverables), issues: Array.isArray(result.issues) ? result.issues.map(String) : [], summary: String(result.summary || '') },
  } as FlowRunState
}
export function applyFlowEvent(current: FlowRunState | undefined, event: FlowEvent, now = new Date().toISOString()): FlowRunState | null {
  const payload = event.payload, id = event.flow_run_id || String(payload.flow_run_id || '')
  if (!id) return null
  if (event.event === 'flow.started' && current) return current
  const run: FlowRunState = current ? structuredClone(current) : {
    api_version: 'flow-run/v1', flow_run_id: id, session_id: event.session_id, flow_id: String(payload.flow_id || ''),
    status: 'running', started_at: now, name: String(payload.name || ''), steps: [], result: { deliverables: {}, issues: [], summary: '' },
  }
  if (event.event === 'flow.started') {
    if (payload.plan && Array.isArray(record(payload.plan).steps)) run.plan = payload.plan as import('@/features/flows/types').FlowPlan
    run.steps = (Array.isArray(payload.steps) ? payload.steps : []).map((value, index) => {
      const step = record(value)
      return { id: String(step.id), index: Number(step.index ?? index + 1), title: String(step.title || ''), role: String(step.role_label || step.role || (step.kind === 'confirm' ? '用户' : '')), status: 'pending', attempts: [], outputs: {} }
    })
    return run
  }
  if (event.event === 'flow.resources_changed') {
    const change = record(payload.resource_change)
    run.resource_change = { fingerprint: String(change.fingerprint || ''), message: String(change.message || '资源在暂停期间发生变化') }
    run.status = String(payload.status || run.status)
    return run
  }
  if (event.event === 'flow.resources_confirmed') {
    run.resource_change = null
    return run
  }
  if (event.event === 'flow.finished') {
    run.resource_change = null
    run.status = String(payload.status || 'completed')
    run.finished_at = run.finished_at || now
    run.result = { ...payload, deliverables: record(payload.deliverables), issues: Array.isArray(payload.issues) ? payload.issues.map(String) : [], summary: String(payload.summary || '') }
    run.run_json_path = String(payload.run_json_path || '')
    return run
  }
  if (terminalFlow(run.status)) return current || run
  if (event.event.startsWith('flow.acquire_')) {
    const acquisition=run.acquisition || {items:[]}
    if(event.event==='flow.acquire_planned') {
      const plan=record(payload.plan || payload.acquisition_plan || payload)
      if(Array.isArray(plan.items))acquisition.plan={items:plan.items as import('@/features/flows/types').AcquisitionItem[]}
    } else {
      const key=String(payload.key||'')
      if(!key)return current || run
      const previous=acquisition.items.find(item=>item.key===key)
      const next={...previous,...payload,key,status:String(payload.status||payload.stage||(event.event==='flow.acquire_failed'?'failed':event.event==='flow.acquire_confirmation'?'waiting_user':event.event==='flow.acquire_item_done'?'completed':event.event==='flow.acquire_item_started'?'running':'pending'))}
      acquisition.items=[...acquisition.items.filter(item=>item.key!==key),next]
      if(event.event==='flow.acquire_failed'||event.event==='flow.acquire_confirmation')run.status='waiting_user'
    }
    run.acquisition=acquisition
    return run
  }
  const stepId = String(payload.step_id || '')
  if (!stepId) return current || run
  let step = run.steps.find(step => step.id === stepId)
  if (!step) {
    step = { id: stepId, index: run.steps.length + 1, title: stepId, role: '', status: 'pending', attempts: [], outputs: {} }
    run.steps.push(step)
  }
  const number = Number(payload.attempt || 0)
  if (number && number < step.attempts.length) return current || run
  if (['completed', 'skipped'].includes(step.status)) return current || run
  if (event.event === 'flow.step_started') {
    const runId = String(payload.run_id || '')
    const known = step.attempts.find(attempt => attempt.run_id === runId)
    if (known?.finished_at) return current || run
    if (!known) step.attempts.push({ run_id: runId, started_at: now })
    step.status = 'running'
    step.awaiting_choice = false
    run.status = 'running'
  } else if (event.event === 'flow.step_skipped') {
    step.status = 'skipped'
    run.status = 'running'
    step.condition = { ...step.condition, reason: String(payload.reason || '') }
  } else if (event.event === 'flow.step_waiting_user') {
    step.status = 'waiting_user'
    step.awaiting_choice = payload.kind === 'failure_choice'
    run.status = 'waiting_user'
  } else if (event.event === 'flow.step_failed') {
    const errors = Array.isArray(payload.errors) ? payload.errors.map(String) : []
    step.last_failure = errors.join('；')
    step.status = payload.next === 'skip' ? 'skipped' : payload.next === 'ask' ? 'waiting_user' : 'failed'
    step.awaiting_choice = payload.next === 'ask'
    const attempt = step.attempts.at(-1)
    if (attempt) { attempt.verification = { ok: false, errors }; attempt.finished_at ||= now }
  } else if (event.event === 'flow.step_completed') {
    step.status = 'completed'
    run.status = 'running'
    step.awaiting_choice = false
    step.outputs = record(payload.outputs)
    const attempt = step.attempts.at(-1)
    if (attempt) {
      attempt.receipt = { status: 'completed', summary: String(payload.summary || ''), outputs: step.outputs, issues: [] }
      attempt.verification = payload.verification as { ok: boolean; errors: string[] } | undefined
      attempt.finished_at ||= now
    } else step.attempts.push({ run_id: '', started_at: now, finished_at: now,
      receipt: { status: 'completed', summary: String(payload.summary || ''), outputs: step.outputs, issues: [] } })
  }
  return run
}
