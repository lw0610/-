import { create } from 'zustand'
import { applyFlowEvent, normalizeFlowRun, record } from './model'
import type { FlowBlocked, FlowEvent, FlowRunState } from './types'

interface State {
  runs: Record<string, FlowRunState[]>
  blocked: Record<string, FlowBlocked | undefined>
  snapshotSeq: Record<string, number>
  eventSeq: Record<string, number>
  applyEvent: (event: FlowEvent, sequence?: number, replayed?: boolean) => void
  restore: (sessionId: string, value: unknown, sequence?: number) => void
  purge: (sessionId: string) => void
}
export const useFlowRunStore = create<State>((set) => ({
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
  purge: sid => set(state => ({ runs: { ...state.runs, [sid]: [] }, blocked: { ...state.blocked, [sid]: undefined }, snapshotSeq: { ...state.snapshotSeq, [sid]: 0 }, eventSeq: { ...state.eventSeq, [sid]: 0 } })),
}))
