'use client'
import { useState, type ReactNode } from 'react'
import { ApiError } from '@/lib/api'
import type { Message } from '@/types/message'
import { planApi } from './api'
import { record } from './model'
import { useFlowRunStore } from './store'
import FlowProgressCard from './FlowProgressCard'
import type { FlowRunState, PlanSnapshot, ResourceResumeChoice } from './types'

export default function PlanProgress({ snapshot, run, messages, renderMessage, executionMode, onOpenDetails }: {
  snapshot: PlanSnapshot; run: FlowRunState; messages: Message[]; renderMessage: (message: Message) => ReactNode; executionMode: 'plan' | 'build'; onOpenDetails?: () => void
}) {
  const [busy, setBusy] = useState(false), [error, setError] = useState(''), [path, setPath] = useState('')
  const [resourceChange, setResourceChange] = useState<FlowRunState['resource_change']>()
  const projected = { ...run, status: snapshot.replanning ? 'replanning' : snapshot.status === 'needs_replan' ? 'needs_replan' : run.status, carto_plan: run.carto_plan || snapshot.plan, resource_change: resourceChange || run.resource_change }
  const step = run.steps.find(item => item.status === 'waiting_user' || item.status === 'failed')
  const optional = snapshot.plan.steps.find(item => item.id === step?.id)?.optional
  const data = snapshot.plan.data.find(item => snapshot.plan.steps.find(candidate => candidate.id === step?.id)?.data?.includes(item.id))
  const update = async (request: () => Promise<PlanSnapshot>) => {
    setBusy(true); setError('')
    try { const result = await request(); useFlowRunStore.getState().applyPlan(result, snapshot.session_id); setResourceChange(undefined); return true }
    catch (reason) {
      if (reason instanceof ApiError && reason.code === 'PLAN_RESOURCE_CHANGED') {
        const details = record(reason.data), change = record(details.resource_change || record(details.details).resource_change)
        if (change.fingerprint) setResourceChange({ fingerprint: String(change.fingerprint), message: String(change.message || reason.message) })
      }
      setError(reason instanceof Error ? reason.message : '操作未完成'); return false
    } finally { setBusy(false) }
  }
  const resume = async (_run: FlowRunState, choice?: ResourceResumeChoice) => update(() => planApi.start(snapshot, { resume_run_id: run.flow_run_id, ...(choice === 'continue_snapshot' ? { resume_resource_choice: choice, resource_fingerprint: projected.resource_change?.fingerprint } : {}) }))
  const failed = snapshot.status === 'needs_replan' || snapshot.status === 'waiting_user' && step?.awaiting_choice
  return <div id={`plan-${snapshot.plan_id}`} aria-label={`计划执行 ${snapshot.plan.goal}`}>
    <FlowProgressCard onOpenDetails={onOpenDetails} run={projected} messages={messages} renderMessage={renderMessage} onResume={executionMode === 'build' ? resume : undefined} />
    {snapshot.replanning && <p role="status" className="mb-3 text-xs">正在根据失败原因调整计划，已完成步骤保持不变。</p>}
    {failed && <section aria-label="执行失败处理" className="mb-3 space-y-2 rounded border border-amber-300 p-3 text-xs"><p>{step?.last_failure || '当前步骤仍未完成，请选择下一步。'}</p><div className="flex flex-wrap gap-2"><button disabled={busy || snapshot.replanning || executionMode !== 'build'} onClick={() => void update(() => planApi.failure(snapshot, 'replan'))}>换来源或调整方案</button>{optional && <button disabled={busy || snapshot.replanning || executionMode !== 'build'} onClick={() => void update(() => planApi.failure(snapshot, 'skip'))}>跳过可选步骤</button>}<button disabled={busy} onClick={() => void update(() => planApi.failure(snapshot, 'stop'))}>停止任务</button></div>{data && <div className="flex gap-2"><input aria-label="补充数据路径" value={path} onChange={event => setPath(event.target.value)} placeholder="提供本地文件或文件夹路径" className="min-w-0 flex-1 rounded border p-1"/><button disabled={busy || snapshot.replanning || executionMode !== 'build' || !path.trim()} onClick={() => void update(() => planApi.failure(snapshot, 'provide_data', { path, data_id: data.id }))}>提供数据并继续</button></div>}</section>}
    {error && <p role="alert" className="mb-3 text-xs text-red-700">{error}</p>}
  </div>
}
