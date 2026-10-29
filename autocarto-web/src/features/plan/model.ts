import type { FlowEvent, FlowRunState, FlowStepState } from './types'

export const record = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
export const terminalFlow = (status: string) => ['completed', 'completed_with_issues', 'failed', 'cancelled'].includes(status)
export function normalizeFlowRun(value: unknown, sessionId?: string): FlowRunState | null {
  const raw = record(value)
  const id = String(raw.flow_run_id || '').trim(), sid = String(raw.session_id || sessionId || '').trim()
  if (!id || !sid) return null
  const result = record(raw.result)
  return { ...raw, api_version: String(raw.api_version || 'flow-run/v1'), flow_run_id: id, session_id: sid,
    plan_id: String(raw.plan_id || record(raw.carto_plan).plan_id || ''), plan_version: Number(raw.plan_version || record(raw.carto_plan).version || 0),
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
  if (current && payload.plan_version && Number(payload.plan_version) < Number(current.plan_version || current.carto_plan?.version || 0)) return current
  if (payload.plan_id) run.plan_id = String(payload.plan_id)
  if (payload.plan_version) run.plan_version = Number(payload.plan_version)
  if (payload.carto_plan) run.carto_plan = payload.carto_plan as import('./types').CartoPlan
  if (event.event === 'flow.started') {
    if (payload.plan && Array.isArray(record(payload.plan).steps)) run.plan = payload.plan as import('@/features/guides/types').FlowPlan
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
    if (Array.isArray(payload.steps)) run.steps = normalizeFlowRun({ ...run, steps: payload.steps })!.steps
    const result = { ...payload }
    delete result.steps
    run.result = { ...result, deliverables: record(payload.deliverables), issues: Array.isArray(payload.issues) ? payload.issues.map(String) : [], summary: String(payload.summary || '') }
    run.run_json_path = String(payload.run_json_path || '')
    return run
  }
  if (terminalFlow(run.status)) return current || run
  if (event.event === 'flow.data_preparation') {
    const item = payload as unknown as import('./types').DataPreparation
    run.data_preparation = [...(run.data_preparation || []).filter(previous => previous.data_id !== item.data_id), item]
    return run
  }
  if (event.event === 'flow.acquisition_progress') {
    const key = String(payload.row || '')
    if (!key) return run
    const acquisition = run.acquisition || {items: []}
    const previous = acquisition.items.find(item => item.key === key)
    const stage = String(payload.stage || 'running')
    const terminal = (status: string) => ['completed', 'done', 'failed', 'cancelled', 'partial'].includes(status)
    if (previous && terminal(previous.status) && !terminal(stage)) return run
    acquisition.items = [...acquisition.items.filter(item => item.key !== key), {...previous, ...payload, key, status: stage}]
    run.acquisition = acquisition
    return run
  }
  if (event.event.startsWith('flow.acquire_')) {
    const acquisition=run.acquisition || {items:[]}
    if(event.event==='flow.acquire_planned') {
      const plan=record(payload.plan || payload.acquisition_plan || payload)
      if(Array.isArray(plan.items))acquisition.plan={items:plan.items as import('@/features/guides/types').AcquisitionItem[]}
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
    step.status = payload.status === 'interrupted' ? 'interrupted' : 'waiting_user'
    step.awaiting_choice = payload.kind === 'failure_choice'
    if (payload.reason) step.last_failure = String(payload.reason)
    run.status = step.status
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
