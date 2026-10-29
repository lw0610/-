'use client'

import { buttonClass, editable, record, statusLabel, tabs, type ReviewReference, type ReviewSummary } from './model'
import { useChatStore } from '@/stores/chatStore'

export default function KnowledgeReviewCard({ reference, summary, onOpen, onConfirm }: {
  reference: ReviewReference; summary?: ReviewSummary
  onOpen?: (ref: ReviewReference) => void; onConfirm?: (ref: ReviewReference) => void
}) {
  const taskRunning = useChatStore(state => state.sessionRuntimeStates[reference.session_id]?.status === 'running')
  const referenceIsNewer = Boolean(summary && ((reference.revision || 0) > summary.revision ||
    (reference.revision || 0) === summary.revision && (Date.parse(reference.updated_at || '') || 0) > (Date.parse(summary.updated_at || '') || 0)))
  const currentSummary = referenceIsNewer ? undefined : summary
  const status = currentSummary?.status || reference.status
  const sourceCounts = record(reference.summary)
  const counts = currentSummary?.counts || { ...sourceCounts, ...(typeof sourceCounts.real_places === 'number' ? { spatial_entities: sourceCounts.real_places } : {}) }
  const empty = tabs.every(({ key }) => counts[key] === 0) || (!currentSummary?.counts && reference.can_start === false)
  const emptyReason = currentSummary?.empty_reason || (reference.can_start === false ? reference.description : '') || '未提取到可制图的内容，请检查原文是否正确读取，或补充材料后重新抽取。'
  const failure = (currentSummary?.errors || reference.errors)?.[0]?.message || reference.description || '请打开结果查看失败详情。'
  return <section
    className="mb-3 max-w-2xl rounded-lg border border-blue-200 bg-blue-50/60 p-4 text-xs dark:border-blue-900 dark:bg-blue-950/30"
    aria-label="知识抽取结果卡片"
  >

    <div className="flex items-center justify-between gap-3">
      <h3 className="font-semibold text-zinc-900 dark:text-zinc-100">知识抽取结果</h3>
      <span>
        {statusLabel(status)}
      </span>
    </div>

    <p className="mt-2 text-zinc-600 dark:text-zinc-300">{empty ? emptyReason : '查看或修改路线、事件、地点、部队与图文注记。保存修改不会启动制图。'}</p>
    {status === 'compile_failed' && <p role="alert" className="mt-2 whitespace-pre-wrap break-words text-red-700 dark:text-red-300">制图数据生成失败：{failure}</p>}

    <div className="mt-2 flex flex-wrap gap-3">
      {Object.entries(counts).filter(([key, value]) => ['routes', 'events', 'spatial_entities', 'actors', 'callouts'].includes(key) && typeof value === 'number').map(([key, value]) => <span key={key}>
        {({ routes: '路线', events: '事件', spatial_entities: '地点', actors: '部队', callouts: '图文注记' } as Record<string, string>)[key] || key}
        {String(value)}
      </span>)}
    </div>

    <div className="mt-3 flex flex-wrap items-center gap-2">

      <button
        className={buttonClass}
        onClick={() => onOpen?.(reference)}
        disabled={!onOpen}
      >查看{!status || editable(status) ? '并修改' : '结果'}</button>

      <button
        className={`${buttonClass} border-blue-400 text-blue-700 dark:text-blue-300`}
        onClick={() => onConfirm?.(reference)}
        disabled={!onConfirm || empty || taskRunning || Boolean(status && !editable(status))}
      >开始制图</button>
      {taskRunning && <span className="text-[10px] text-zinc-500">当前任务仍在执行</span>}

      <span className="text-[10px] text-zinc-500">{currentSummary?.revision || reference.revision ? `版本 ${currentSummary?.revision || reference.revision} · ` : ''}打开时读取最新状态</span>

    </div>

  </section>
}
