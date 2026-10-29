'use client'
import { useEffect, useState, type ReactNode } from 'react'
import type { Message } from '@/types/message'
import type { FlowRunState, ResourceResumeChoice } from './types'
import FlowOutputs from './FlowOutputs'
import MessageBubble from '@/components/chat/MessageBubble'
import { terminalFlow } from './model'

const labels: Record<string, string> = { needs_replan: '等待处理', replanning: '正在调整计划', pending: '等待执行', running: '正在执行', waiting_user: '等待确认', interrupted: '任务已中断', completed: '任务已完成。', completed_with_issues: '任务已完成，另有待处理事项，可在右侧查看。', failed: '任务未完成。', cancelled: '任务已停止。' }
export default function FlowProgressCard({ run, messages, renderMessage, onResume, onOpenDetails }: {
  run: FlowRunState; messages: Message[]; renderMessage: (message: Message) => ReactNode
  onOpenDetails?: () => void
  onResume?: (run: FlowRunState, choice?: ResourceResumeChoice) => Promise<boolean>
}) {
  const [resumeRequested, setResumeRequested] = useState(false)
  const [resumeError, setResumeError] = useState('')
  useEffect(() => { setResumeRequested(false) }, [run.status, run.resource_change?.fingerprint])
  const currentStep = run.steps.find(step => ['running', 'waiting_user', 'failed', 'interrupted'].includes(step.status))
  const actions = messages.filter(message => message.type === 'interrupt' || message.type === 'decision')
  const finished = terminalFlow(run.status)
  const lastCompleted = run.steps.slice().reverse().find(step => step.status === 'completed')
  const completedSummary = lastCompleted?.attempts.at(-1)?.receipt?.summary
  const detail = run.status === 'failed' ? currentStep?.last_failure || run.result.issues.join('\n')
    : run.status.startsWith('completed') ? completedSummary : ''
  const content = [labels[run.status], detail].filter(Boolean).join('\n\n')
  const declared = run.carto_plan?.deliverables
  const outputs = declared?.length
    ? Object.fromEntries(declared.map(output => typeof output === 'string' ? output : output.label).filter(label => label in run.result.deliverables).map(label => [label, run.result.deliverables[label]]))
    : run.result.deliverables
  const requestResume = async (choice?: ResourceResumeChoice) => {
    if (!onResume) return
    setResumeRequested(true); setResumeError('')
    try { if (!await (choice ? onResume(run, choice) : onResume(run))) setResumeRequested(false) } catch (error) { setResumeRequested(false); setResumeError(error instanceof Error ? error.message : '继续请求发送失败') }
  }
  return <section id={`flow-run-${run.flow_run_id}`} aria-label={`流程进度 ${run.carto_plan?.goal || run.plan?.name || run.name || run.flow_id}`} className="mb-3">
    {finished && <MessageBubble message={{ id: `flow-summary-${run.flow_run_id}`, type: 'assistant', content, timestamp: Date.parse(run.finished_at || run.started_at) || 0 }}>
      {!!Object.keys(outputs).length && <div className="mt-3"><FlowOutputs outputs={outputs}/></div>}
      {onOpenDetails && <button type="button" className="mt-2 rounded border border-zinc-200 px-1.5 py-0.5 text-[10px] text-zinc-500 dark:border-zinc-700" onClick={onOpenDetails}>查看详情</button>}
    </MessageBubble>}
    {!run.carto_plan && <p className="mb-2 text-xs text-text-muted">旧版运行记录（只读）</p>}
    {run.resource_change && onResume && <div role="alert" className="mb-3 rounded border border-amber-300 bg-amber-50 p-2 text-xs text-amber-950 dark:bg-amber-950 dark:text-amber-100">
      <p>{run.resource_change.message}</p>
      <div className="mt-2 flex flex-wrap gap-2">
        <button type="button" disabled={resumeRequested} className="rounded border border-amber-500 px-2 py-1 disabled:opacity-50" onClick={() => void requestResume('continue_snapshot')}>继续使用运行快照</button>
        <p>如需使用当前资源，请停止此任务并生成新计划。</p>
      </div>
      {resumeRequested && <p className="mt-1">选择已发送，正在核对资源</p>}
    </div>}
    {run.status === 'interrupted' && !run.resource_change && onResume && <div className="mb-2 text-xs"><button type="button" disabled={resumeRequested} className="rounded border border-blue-300 px-2 py-1 text-blue-700 disabled:opacity-50" onClick={() => void requestResume()}>{resumeRequested ? '继续请求已发送，等待确认' : '继续此流程'}</button></div>}
    {resumeError && <p role="alert" className="mb-2 text-xs text-red-700">{resumeError}</p>}
    {!run.resource_change && run.carto_plan && actions.map(message => <div key={message.id} className="mt-2">{renderMessage(message)}</div>)}
  </section>
}
