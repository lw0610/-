'use client'
import { useState, type ReactNode } from 'react'
import { SourceEditor } from './V2PlanCard'
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
  const [editingData, setEditingData] = useState(false), [selectedData, setSelectedData] = useState('')
  const [resourceChange, setResourceChange] = useState<FlowRunState['resource_change']>()
  const projected = { ...run, status: snapshot.replanning ? 'replanning' : snapshot.status === 'needs_replan' ? 'needs_replan' : run.status, carto_plan: run.carto_plan || snapshot.plan, resource_change: resourceChange || run.resource_change }
  const step = run.steps.find(item => item.status === 'waiting_user' || item.status === 'failed' || item.status === 'interrupted')
  const optional = snapshot.plan.steps.find(item => item.id === step?.id)?.optional
  const activePlanStep = snapshot.plan.steps.find(candidate => candidate.id === step?.id)
  const inputs = snapshot.plan.data.filter(item => (activePlanStep?.inputs || activePlanStep?.data)?.includes(item.id))
  const blockedData = run.data_preparation?.find(item => ['failed', 'unavailable', 'waiting_user', 'interrupted'].includes(item.status))?.data_id
  const data = inputs.find(item => item.id === selectedData) || inputs.find(item => item.id === blockedData) || inputs[0]
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
  const paused = run.status === 'interrupted'
  const failed = paused || snapshot.status === 'needs_replan' || snapshot.status === 'waiting_user' && step?.awaiting_choice
  return <div id={`plan-${snapshot.plan_id}`} aria-label={`计划执行 ${snapshot.plan.goal}`}>
    <FlowProgressCard onOpenDetails={onOpenDetails} run={projected} messages={messages} renderMessage={renderMessage} onResume={executionMode === 'build' ? resume : undefined} />
    {snapshot.replanning && <p role="status" className="mb-3 text-xs">正在根据失败原因调整计划，已完成步骤保持不变。</p>}
    {failed && <section aria-label="执行失败处理" className="mb-3 space-y-2 rounded border border-amber-300 p-3 text-xs"><p>{step?.last_failure || '当前步骤仍未完成，请选择下一步。'}</p>{inputs.length > 1 && <label className="block">要调整的数据<select className="ml-2 rounded border p-1" value={data?.id || ''} onChange={event => { setSelectedData(event.target.value); setEditingData(false); setPath('') }}>{inputs.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>}<div className="flex flex-wrap gap-2"><button disabled={busy || snapshot.replanning || executionMode !== 'build'} onClick={() => void update(() => planApi.failure(snapshot, 'replan'))}>换来源或调整方案</button>{data && snapshot.plan.api_version === 'carto-plan/v2' && <button disabled={busy || executionMode !== 'build'} onClick={() => setEditingData(!editingData)}>修改数据来源</button>}{optional && <button disabled={busy || snapshot.replanning || executionMode !== 'build'} onClick={() => void update(() => planApi.failure(snapshot, 'skip'))}>跳过可选步骤</button>}<button disabled={busy} onClick={() => void update(() => planApi.failure(snapshot, 'stop'))}>停止任务</button></div>{data && <div className="flex gap-2"><input aria-label="补充数据路径" value={path} onChange={event => setPath(event.target.value)} placeholder="提供本地文件或文件夹路径" className="min-w-0 flex-1 rounded border p-1"/><button disabled={busy || snapshot.replanning || executionMode !== 'build' || !path.trim()} onClick={() => void update(() => planApi.failure(snapshot, 'provide_data', { path, data_id: data.id }))}>提供数据并继续</button></div>}{editingData && data && <SourceEditor key={data.id} snapshot={snapshot} data={data} check={snapshot.precheck?.find(item => item.data_id === data.id)} onDone={() => setEditingData(false)}/>}</section>}
    {error && <p role="alert" className="mb-3 text-xs text-red-700">{error}</p>}
  </div>
}
