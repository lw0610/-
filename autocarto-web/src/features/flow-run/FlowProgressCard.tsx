'use client'
import { useEffect, useState, type ReactNode } from 'react'
import type { Message } from '@/types/message'
import type { FlowRunState, FlowStepState, ResourceResumeChoice } from './types'
import { terminalFlow } from './model'
import FlowOutputs from './FlowOutputs'
import { flowApi } from '@/features/flows/api'

const labels: Record<string, string> = { pending: '等待', running: '进行中', waiting_user: '等待确认', interrupted: '已中断', completed: '已完成', completed_with_issues: '完成，存在问题', skipped: '已跳过', failed: '失败', cancelled: '已终止' }
function duration(step: FlowStepState, now: number) {
  const start = Date.parse(step.attempts[0]?.started_at || '')
  const end = Date.parse(step.attempts.at(-1)?.finished_at || '') || now
  if (!Number.isFinite(start)) return ''
  const seconds = Math.max(0, Math.round((end - start) / 1000))
  return seconds < 60 ? `${seconds} 秒` : `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`
}
export default function FlowProgressCard({ run, messages, renderMessage, onResume }: {
  run: FlowRunState; messages: Message[]; renderMessage: (message: Message) => ReactNode
  onResume?: (run: FlowRunState, choice?: ResourceResumeChoice) => Promise<boolean>
}) {
  const [now, setNow] = useState(Date.now())
  const [resumeRequested, setResumeRequested] = useState(false)
  const [resumeError, setResumeError] = useState('')
  const [toolLabels, setToolLabels] = useState<Record<string, string>>({})
  useEffect(() => {
    let current = true
    void flowApi.capabilities().then(items => { if (current) setToolLabels(Object.fromEntries(items.map(item => [item.name, item.display_name || item.name]))) }).catch(() => undefined)
    return () => { current = false }
  }, [])
  useEffect(() => { setResumeRequested(false) }, [run.status, run.resource_change?.fingerprint])
  useEffect(() => {
    if (terminalFlow(run.status)) return
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [run.status])
  const assigned = new Set(run.steps.map(step => step.id))
  const unassigned = messages.filter(message => !assigned.has(String(message.data?.step_id || '')))
  const requestResume = async (choice?: ResourceResumeChoice) => {
    if (!onResume) return
    setResumeRequested(true); setResumeError('')
    try { if (!await (choice ? onResume(run, choice) : onResume(run))) setResumeRequested(false) } catch (error) { setResumeRequested(false); setResumeError(error instanceof Error ? error.message : '继续请求发送失败') }
  }
  return <section aria-label={`流程进度 ${run.plan?.name || run.name || run.flow_id}`} className="mb-3 w-full rounded border border-blue-200 bg-surface-raised p-3 dark:border-blue-900">
    <header className="mb-3 flex flex-wrap justify-between gap-2"><h2 className="text-sm font-semibold">{run.plan?.name || run.name || run.flow_id}</h2><span className="text-xs text-blue-700">{labels[run.status] || run.status} · {run.steps.filter(step => ['completed', 'skipped'].includes(step.status)).length}/{run.steps.length}</span></header>
    {run.resource_change && onResume && <div role="alert" className="mb-3 rounded border border-amber-300 bg-amber-50 p-2 text-xs text-amber-950 dark:bg-amber-950 dark:text-amber-100">
      <p>{run.resource_change.message}</p>
      <div className="mt-2 flex flex-wrap gap-2">
        <button type="button" disabled={resumeRequested} className="rounded border border-amber-500 px-2 py-1 disabled:opacity-50" onClick={() => void requestResume('continue_snapshot')}>继续使用运行快照</button>
        <button type="button" disabled={resumeRequested} className="rounded border border-amber-500 px-2 py-1 disabled:opacity-50" onClick={() => void requestResume('restart')}>使用当前资源重新开始</button>
      </div>
      {resumeRequested && <p className="mt-1">选择已发送，正在核对资源</p>}
    </div>}
    {run.status === 'interrupted' && !run.resource_change && onResume && <div className="mb-2 text-xs"><button type="button" disabled={resumeRequested} className="rounded border border-blue-300 px-2 py-1 text-blue-700 disabled:opacity-50" onClick={() => void requestResume()}>{resumeRequested ? '继续请求已发送，等待确认' : '继续此流程'}</button></div>}
    {resumeError && <p role="alert" className="mb-2 text-xs text-red-700">{resumeError}</p>}
    <ol className="space-y-2">{run.steps.map(step => {
      const process = messages.filter(message => message.data?.step_id === step.id)
      const actions = process.filter(message => message.type === 'interrupt')
      const tools = process.filter(message => message.type !== 'interrupt')
      const receipt = step.attempts.at(-1)?.receipt
      return <li key={step.id} className="rounded border border-zinc-200 p-2 dark:border-zinc-700" aria-label={`第 ${step.index} 步 ${step.title}`}>
        <div className="flex flex-wrap items-center justify-between gap-2 text-xs"><h3 className="font-medium">{step.index}. {step.title}</h3><span>{labels[step.status] || step.status}{duration(step, now) && ` · ${duration(step, now)}`}</span></div>
        <p className="mt-1 text-xs text-text-muted">负责：{step.role || run.plan?.steps.find(item => item.id === step.id)?.role_label || '系统' }{step.attempts.length > 1 && ` · 第 ${step.attempts.length} 次尝试`}</p>
        {step.status === 'skipped' && step.condition?.reason && <p className="mt-1 text-xs">跳过原因：{step.condition.reason}</p>}
        {step.last_failure && <p className="mt-1 whitespace-pre-wrap text-xs text-amber-700">{step.last_failure}</p>}
        {(receipt || Object.keys(step.outputs).length > 0) && <details className="mt-2 text-xs"><summary className="cursor-pointer">回执与产出{receipt?.summary && `：${receipt.summary}`}</summary><div className="mt-2 space-y-2"><FlowOutputs outputs={step.outputs}/>{receipt?.issues?.map((issue, index) => <p key={index} className="text-amber-700">{issue}</p>)}{step.attempts.at(-1)?.verification?.errors?.map((error, index) => <p key={index} className="text-red-700">{error}</p>)}</div></details>}
        {!!tools.length && <details className="mt-2 text-xs"><summary className="cursor-pointer">步骤过程（{tools.length}）</summary><div className="mt-2 space-y-2">{tools.map(message => <div key={message.id} className="rounded bg-zinc-50 p-2 dark:bg-zinc-900"><p className="text-[10px] text-text-muted">{message.phase === 'tool_call' ? '调用能力' : message.phase === 'tool_result' ? '能力回执' : message.type === 'thinking' ? '思考' : '过程' }{message.data?.tool ? ` · ${toolLabels[String(message.data.tool)] || String(message.data.tool)}` : ''}</p><pre className="mt-1 max-h-64 overflow-auto whitespace-pre-wrap break-words text-[11px]">{String(message.data?.result || message.content || '')}</pre></div>)}</div></details>}
        {!run.resource_change && actions.map(message => <div key={message.id} className="mt-2">{renderMessage(message)}</div>)}
      </li>
    })}</ol>
    {!!unassigned.length && <details className="mt-2 text-xs"><summary>其他流程消息（{unassigned.length}）</summary>{unassigned.map(renderMessage)}</details>}
    {run.result.summary && <p className="mt-3 text-xs">{run.result.summary}</p>}
    {!!Object.keys(run.result.deliverables).length && <div className="mt-3"><h3 className="mb-1 text-xs font-medium">交付成果</h3><FlowOutputs outputs={run.result.deliverables}/></div>}
    {run.result.issues.map((issue, index) => <p key={index} className="mt-1 text-xs text-amber-700">{issue}</p>)}
    {run.run_json_path && <div className="mt-2"><FlowOutputs outputs={{ '运行记录': run.run_json_path }}/></div>}
  </section>
}
