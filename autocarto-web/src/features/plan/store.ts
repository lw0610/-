import { create } from 'zustand'
import { applyFlowEvent, normalizeFlowRun, record, terminalFlow } from './model'
import type { FlowBlocked, FlowEvent, FlowRunState, PlanSnapshot } from './types'

interface State {
  plans: Record<string, PlanSnapshot[]>
  applyPlan: (value: unknown, sessionId?: string) => void
  restorePlans: (sessionId: string, value: unknown) => void
  runs: Record<string, FlowRunState[]>
  blocked: Record<string, FlowBlocked | undefined>
  snapshotSeq: Record<string, number>
  eventSeq: Record<string, number>
  applyEvent: (event: FlowEvent, sequence?: number, replayed?: boolean) => void
  restore: (sessionId: string, value: unknown, sequence?: number) => void
  purge: (sessionId: string) => void
}
export const useFlowRunStore = create<State>((set) => ({
  plans: {},
  applyPlan: (value, sessionId) => set(state => {
    const next = normalizePlanSnapshot(value, sessionId)
    if (!next) return state
    const plans = state.plans[next.session_id] || [], previous = plans.find(item => item.plan_id === next.plan_id)
    if (previous && (previous.plan_version > next.plan_version || previous.plan_version === next.plan_version && terminalPlan(previous.status) && !terminalPlan(next.status))) return state
    if (previous?.plan_version === next.plan_version && previous.updated_at && next.updated_at && Date.parse(previous.updated_at) > Date.parse(next.updated_at)) return state
    if (next.run) next.run = normalizeFlowRun(next.run, next.session_id) || undefined
    next.created_at ||= previous?.created_at || new Date().toISOString()
    const runs = state.runs[next.session_id] || [], current = runs.find(item => item.flow_run_id === next.run?.flow_run_id)
    const acceptRun = next.run && (!current || !(terminalFlow(current.status) && !terminalFlow(next.run.status)) && Number(current.event_seq || 0) <= Number(next.run.event_seq || 0))
    return { plans: { ...state.plans, [next.session_id]: previous ? plans.map(item => item.plan_id === next.plan_id ? next : item) : [...plans, next] },
      ...(acceptRun ? { runs: {...state.runs, [next.session_id]: current ? runs.map(item => item.flow_run_id === next.run!.flow_run_id ? next.run! : item) : [...runs, next.run!] } } : {}) }
  }),
  restorePlans: (sid, value) => {
    if (!Array.isArray(value)) return
    for (const item of value) useFlowRunStore.getState().applyPlan(item, sid)
  },
  runs: {}, blocked: {}, snapshotSeq: {}, eventSeq: {},
  applyEvent: (event, sequence, replayed) => set(state => {
    const sid = String(event.session_id || '').trim()
    if (!event.payload || typeof event.event !== 'string' || !event.event.startsWith('flow.')) return state
    if (!sid || replayed && sequence && sequence <= (state.snapshotSeq[sid] || 0)) return state
    if (event.event === 'flow.blocked') return { blocked: { ...state.blocked, [sid]: {
      flow_id: String(event.payload.flow_id || ''), reasons: (Array.isArray(event.payload.reasons) ? event.payload.reasons : []).map(value => {
        const item = record(value); return { code: String(item.code || ''), message: String(item.message || value) }
      }),
    } } }
    const id = event.flow_run_id || String(event.payload.flow_run_id || '')
    const runs = state.runs[sid] || [], current = runs.find(run => run.flow_run_id === id)
    if (replayed && current && event.event === 'flow.step_started' && current.steps.some(step => step.id === event.payload.step_id && step.attempts.some(attempt => attempt.run_id === event.payload.run_id))) {
      return sequence ? { eventSeq: { ...state.eventSeq, [sid]: Math.max(sequence, state.eventSeq[sid] || 0) } } : state
    }
    const next = applyFlowEvent(current, event)
    if (!next) return state
    if (next === current) return sequence ? { eventSeq: { ...state.eventSeq, [sid]: Math.max(sequence, state.eventSeq[sid] || 0) } } : state
    if (sequence) next.event_seq = Math.max(sequence, Number(next.event_seq || 0))
    return { runs: { ...state.runs, [sid]: current ? runs.map(run => run.flow_run_id === id ? next : run) : [...runs, next] },
      blocked: { ...state.blocked, [sid]: undefined }, eventSeq: { ...state.eventSeq, [sid]: Math.max(sequence || 0, state.eventSeq[sid] || 0) } }
  }),
  restore: (sid, value, sequence) => set(state => {
    if (value === undefined) return state
    if (sequence && sequence < (state.eventSeq[sid] || 0)) return state
    const next = normalizeFlowRun(value, sid)
    return { runs: { ...state.runs, [sid]: next ? [...(state.runs[sid] || []).filter(run => run.flow_run_id !== next.flow_run_id), next] : [] },
      blocked: { ...state.blocked, [sid]: undefined },
      snapshotSeq: { ...state.snapshotSeq, [sid]: sequence || 0 }, eventSeq: { ...state.eventSeq, [sid]: sequence || 0 } }
  }),
  purge: sid => set(state => ({ plans: { ...state.plans, [sid]: [] }, runs: { ...state.runs, [sid]: [] }, blocked: { ...state.blocked, [sid]: undefined }, snapshotSeq: { ...state.snapshotSeq, [sid]: 0 }, eventSeq: { ...state.eventSeq, [sid]: 0 } })),
}))

export const terminalPlan = (status: string) => ['completed', 'completed_with_issues', 'failed', 'cancelled'].includes(status)
export function normalizePlanSnapshot(value: unknown, sessionId?: string): PlanSnapshot | null {
  const raw = record(value), plan = record(raw.plan)
  const sid = String(raw.session_id || sessionId || ''), id = String(raw.plan_id || plan.plan_id || '')
  if (!sid || !id || plan.api_version !== 'carto-plan/v1' || !Array.isArray(plan.steps)) return null
  return { ...raw, session_id: sid, plan_id: id, plan_version: Number(raw.plan_version || plan.version || 1),
    plan: { ...plan, facts: plan.facts || [], assumptions: plan.assumptions || [], data: plan.data || [], deliverables: plan.deliverables || [], checks: plan.checks || [] },
    status: String(raw.status || 'draft'), needs_confirmation: Boolean(raw.needs_confirmation), execution_mode: raw.execution_mode === 'plan' ? 'plan' : 'build',
  } as PlanSnapshot
}
