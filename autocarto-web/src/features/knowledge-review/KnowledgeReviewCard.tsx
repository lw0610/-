'use client'

import { buttonClass, editable, record, statusLabel, type ReviewReference, type ReviewSummary } from './model'

export default function KnowledgeReviewCard({ reference, summary, onOpen, onConfirm }: {
  reference: ReviewReference; summary?: ReviewSummary
  onOpen?: (ref: ReviewReference) => void; onConfirm?: (ref: ReviewReference) => void
}) {
  const status = summary?.status || reference.status
  const sourceCounts = record(reference.summary)
  const counts = summary?.counts || { ...sourceCounts, ...(typeof sourceCounts.real_places === 'number' ? { spatial_entities: sourceCounts.real_places } : {}) }
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

    <p className="mt-2 text-zinc-600 dark:text-zinc-300">查看或修改路线、事件、地点、部队与图文注记。保存修改不会启动制图。</p>

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
        disabled={!onConfirm || Boolean(status && !editable(status))}
      >开始制图</button>

      <span className="text-[10px] text-zinc-500">{summary?.revision || reference.revision ? `版本 ${summary?.revision || reference.revision} · ` : ''}打开时读取最新状态</span>

    </div>

  </section>
}
