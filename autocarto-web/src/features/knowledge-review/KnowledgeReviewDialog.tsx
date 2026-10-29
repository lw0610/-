'use client'

import { useEffect, useState } from 'react'
import { Dialog, DialogPanel, DialogTitle } from '@headlessui/react'
import EntityEditors from './EntityEditors'
import RouteEditor from './RouteEditor'
import CalloutEditor from './CalloutEditor'
import { isDesktopRuntime } from '@/lib/config-form-helpers'
import { buttonClass, createItem, editable, hasExtractedContent, itemId, itemOptions, items, issueLabel, label, referencesTo, reviewGroups, statusLabel, tabs, tabsByGroup, unlocatedPlaces, unlocatedReferences, updateItem, visibleItems, type GraphItem, type ReviewGroup, type ReviewTab } from './model'
import type { KnowledgeReviewController } from './useKnowledgeReview'

export default function KnowledgeReviewDialog({ controller: c }: { controller: KnowledgeReviewController }) {
  const [group, setGroup] = useState<ReviewGroup>('content')
  const [tab, setTab] = useState<ReviewTab>('routes')
  const [selected, setSelected] = useState<Record<string, string>>({})
  const [deleteError, setDeleteError] = useState('')
  const [assetBusy, setAssetBusy] = useState(false)
  useEffect(() => { setSelected({}); setGroup('content'); setTab('routes'); setDeleteError('') }, [c.target?.review_id, c.target?.session_id])
  const busy = c.loading || c.saving || c.confirming || c.geocoding || c.importing || assetBusy
  const canEdit = Boolean(c.document && editable(c.document.status))
  const list = c.draft ? visibleItems(c.draft, tab) : []
  const item = list.find((candidate) => itemId(candidate, tab) === selected[tab]) || list[0]
  const id = item ? itemId(item, tab) : ''
  const change = (patch: Record<string, unknown>) => c.setDraft((graph) => graph ? updateItem(graph, tab, id, patch) : graph)
  const add = () => {
    const created = createItem(tab)
    c.setDraft((graph) => graph ? { ...graph, [tab]: [...items(graph, tab), created] } : graph)
    setSelected((prev) => ({ ...prev, [tab]: itemId(created, tab) })); setDeleteError('')
  }
  const remove = () => {
    if (!c.draft || !item) return
    const refs = referencesTo(c.draft, tab, id)
    if (refs.length) { setDeleteError(`不能删除：仍被${refs.join('、')}引用。请先修改引用。`); return }
    if (!window.confirm(`删除「${label(item, c.draft)}」？保存后才会写入草稿。`)) return
    c.setDraft((graph) => graph ? { ...graph, [tab]: items(graph, tab).filter((candidate) => itemId(candidate, tab) !== id) } : graph)
    setDeleteError('')
  }
  const evidenceIds = new Set(Array.isArray(item?.evidence_refs) ? item.evidence_refs : [])
  const evidence = (Array.isArray(c.draft?.evidence) ? c.draft.evidence : []).filter((entry: GraphItem) => evidenceIds.has(String(entry.evidence_id)))
  return <Dialog
    open={c.open || c.confirmOpen}
    onClose={() => { if (!busy) c.close() }}
    className="relative z-[70]"
  >

    <div
      className="fixed inset-0 bg-zinc-950/60 backdrop-blur-sm"
      aria-hidden="true"
    />

    <div className="fixed inset-0 flex items-center justify-center p-3 sm:p-6">

      <DialogPanel className={`flex max-h-[94vh] w-full flex-col overflow-hidden rounded-xl border border-zinc-200 bg-white shadow-2xl dark:border-zinc-700 dark:bg-zinc-950 ${c.confirmOpen ? 'max-w-xl' : 'max-w-6xl'}`}>

        <header className="flex shrink-0 items-start justify-between gap-3 border-b border-zinc-200 p-4 dark:border-zinc-800">

          <div>
            <DialogTitle className="text-base font-semibold">
              知识抽取结果
            </DialogTitle>
            <p className="mt-1 text-xs text-zinc-500">
              {statusLabel(c.document?.status)}
              {c.document ? ` · 版本 ${c.document.revision}` : ''}
              {c.dirty ? ' · 有未保存修改' : ''}
            </p>
          </div>

          <button
            className={buttonClass}
            disabled={busy}
            onClick={c.close}
            aria-label="关闭知识抽取结果"
          >关闭</button>

        </header>

        <div className="min-h-0 overflow-y-auto">

          {(c.error || c.notice || c.issues.length > 0) && <div className="space-y-2 border-b border-zinc-200 p-3 text-xs dark:border-zinc-800">

            {c.error && <p
              role="alert"
              className="text-red-700 dark:text-red-300"
            >
              {c.error}
            </p>}

            {c.notice && <p
              role="status"
              className="text-blue-700 dark:text-blue-300"
            >
              {c.notice}
            </p>}

            {c.issues.length > 0 && <ul className="max-h-32 list-inside list-disc overflow-y-auto text-amber-700 dark:text-amber-300">
              {c.issues.map((issue, index) => <li key={index} title={issue.path}>
                {issueLabel(issue, c.draft) ? `${issueLabel(issue, c.draft)}：` : ''}
                {issue.message}
              </li>)}
            </ul>}

          </div>}

          {c.loading && <p
            role="status"
            className="p-8 text-sm"
          >正在读取最新草稿…</p>}

          {c.importing && <p role="status" className="px-4 py-3 text-xs text-blue-700 dark:text-blue-300">正在读取 Excel、导入图片并自动定位注记地点，请稍候…</p>}
          {c.importWarnings.length > 0 && <details className="border-b border-zinc-200 px-4 py-3 text-xs dark:border-zinc-800">
            <summary className="cursor-pointer">导入说明（{c.importWarnings.length}）</summary>
            <ul className="mt-2 max-h-40 list-inside list-disc overflow-y-auto text-zinc-500">
              {c.importWarnings.map((warning, index) => <li key={index}>{warning}</li>)}
            </ul>
          </details>}

          {!c.confirmOpen && c.document && <section aria-label="地点坐标补全" className="flex flex-wrap items-center justify-between gap-3 border-b border-zinc-200 p-3 dark:border-zinc-800">
            <div className="text-xs text-zinc-500">
              <p>自动定位地点与路线参照并保存坐标；路线参照不会新增地点标记。</p>
              {c.draft && <p className="mt-1">未定位地点 {unlocatedPlaces(c.draft).length} 个 · 路线参照 {unlocatedReferences(c.draft).length} 处</p>}
              {c.dirty && <p className="mt-1 text-amber-700 dark:text-amber-300">请先保存当前修改，再补全坐标。</p>}
            </div>
            <button type="button" className={buttonClass} disabled={busy || !canEdit || c.dirty} onClick={() => void c.geocode()}>
              {c.geocoding ? '正在查询并回填坐标…' : '自动补全坐标'}
            </button>
            {c.geocoding && <p role="status" className="w-full text-xs text-blue-700 dark:text-blue-300">正在自动定位并保存结果，可能需要几分钟。</p>}
          </section>}

          {c.confirmOpen && c.document && <div className="space-y-3 p-5 text-sm">
            <p>使用已保存的版本 {c.document.revision} 开始制图。</p>
            <p className="text-xs text-zinc-500">后台将按当前结果自动适配并生成地图。</p>
          </div>}

          {!c.confirmOpen && c.draft && c.document && c.target && <>

            <nav
              className="space-y-2 border-b border-zinc-200 px-4 py-2 dark:border-zinc-800"
              aria-label="知识结果分类"
            >
              <div className="flex gap-1 overflow-x-auto" aria-label="知识结果一级分类">
                {reviewGroups.map((entry) => <button
                  key={entry.key}
                  type="button"
                  className={`${buttonClass} shrink-0 font-medium ${group === entry.key ? 'border-blue-400 bg-blue-50 text-blue-700 dark:bg-blue-950 dark:text-blue-300' : ''}`}
                  disabled={busy}
                  aria-pressed={group === entry.key}
                  onClick={() => {
                    setGroup(entry.key)
                    if (!tabsByGroup[entry.key].includes(tab)) setTab(tabsByGroup[entry.key][0])
                    setDeleteError('')
                  }}
                >{entry.label}</button>)}
              </div>
              <div className="flex gap-1 overflow-x-auto pl-3" aria-label="知识结果二级分类">
                {tabs.filter((entry) => tabsByGroup[group].includes(entry.key)).map((entry) => <button
                  key={entry.key}
                  type="button"
                  className={`${buttonClass} shrink-0 ${tab === entry.key ? 'border-blue-400 bg-blue-50 text-blue-700 dark:bg-blue-950 dark:text-blue-300' : ''}`}
                  disabled={busy}
                  aria-pressed={tab === entry.key}
                  onClick={() => { setTab(entry.key); setDeleteError('') }}
                >{entry.label} ({visibleItems(c.draft!, entry.key).length})</button>)}
              </div>
            </nav>

            <div className="grid min-h-[380px] grid-cols-[180px_minmax(0,1fr)] sm:grid-cols-[220px_minmax(0,1fr)]">

              <aside className="border-r border-zinc-200 p-3 dark:border-zinc-800">
                <button
                  type="button"
                  className={`${buttonClass} mb-3 w-full`}
                  disabled={!canEdit || busy}
                  onClick={add}
                >新增{tabs.find((entry) => entry.key === tab)?.label}</button>
                {tab === 'callouts' && <div className="mb-3 space-y-2">
                  <button type="button" className={`${buttonClass} w-full`} disabled={!canEdit || busy || !isDesktopRuntime()} onClick={() => void c.importCallouts()}>
                    {c.importing ? '正在导入…' : '从 Excel 导入'}
                  </button>
                  <p className="text-[11px] text-zinc-500">支持 .xlsx 的地点、文本和内嵌图片。导入会连同当前修改一起保存；无图记录使用纯文字。{!isDesktopRuntime() && '请在桌面端选择本地文件。'}</p>
                </div>}
                <div className="space-y-1">
                  {list.map((entry) => <button
                    type="button"
                    key={itemId(entry, tab)}
                    disabled={busy}
                    title={itemId(entry, tab)}
                    className={`w-full rounded p-2 text-left text-sm ${itemId(entry, tab) === id ? 'bg-blue-50 text-blue-700 dark:bg-blue-950 dark:text-blue-300' : 'hover:bg-zinc-50 dark:hover:bg-zinc-900'}`}
                    onClick={() => { setSelected((prev) => ({ ...prev, [tab]: itemId(entry, tab) })); setDeleteError('') }}
                  >
                    <span className="block break-words">
                      {itemOptions(c.draft!, tab).find(([key]) => key === itemId(entry, tab))?.[1] || label(entry)}
                    </span>
                  </button>)}
                </div>
              </aside>

              <main className="min-w-0 p-4">

                {item ? <>

                  <div className="mb-4 flex items-center justify-between gap-3">
                    <h3 className="min-w-0 truncate text-sm font-medium" title={id}>{label(item, c.draft)}</h3>
                    <button
                      type="button"
                      className={`${buttonClass} shrink-0 text-red-600`}
                      disabled={!canEdit || busy}
                      onClick={remove}
                    >删除条目</button>
                  </div>

                  {deleteError && <p
                    role="alert"
                    className="mb-3 text-xs text-red-600"
                  >
                    {deleteError}
                  </p>}

                  {tab === 'routes' ? <RouteEditor
                    key={id}
                    item={item}
                    graph={c.draft}
                    onChange={change}
                    disabled={!canEdit || busy}
                  /> : tab === 'callouts' ? <CalloutEditor
                    key={id}
                    item={item}
                    graph={c.draft}
                    onChange={change}
                    disabled={!canEdit || c.loading || c.saving || c.confirming || c.geocoding || c.importing}
                    reference={c.target}
                    document={c.document}
                    onBusyChange={setAssetBusy}
                    onAssetImported={c.registerAsset}
                  /> : <EntityEditors
                    item={item}
                    graph={c.draft}
                    onChange={change}
                    tab={tab}
                    disabled={!canEdit || busy}
                  />}

                  <details className="mt-6 rounded bg-zinc-50 p-3 text-xs dark:bg-zinc-900">
                    <summary className="cursor-pointer font-medium">原文证据 ({evidence.length})</summary>
                    <div className="mt-2 space-y-2">
                      {evidence.length ? evidence.map((entry) => <blockquote
                        key={String(entry.evidence_id)}
                        className="whitespace-pre-wrap border-l-2 border-blue-300 pl-2"
                      >
                        <p>
                          {String(entry.quote || '')}
                        </p>
                      </blockquote>) : <p className="text-zinc-500">暂无原文引用；人工补充不会冒充原文内容。</p>}
                    </div>
                  </details>

                </> : <p className="py-16 text-center text-sm text-zinc-500">暂无条目。可新增后填写真实信息。</p>}

              </main>

            </div>

            <details className="border-t border-zinc-200 p-4 text-xs dark:border-zinc-800">
              <summary className="cursor-pointer font-medium">查看完整输入原文（只读）</summary>
              <pre className="mt-3 max-h-56 overflow-y-auto whitespace-pre-wrap break-words font-sans leading-relaxed">
                {c.document.source_text}
              </pre>
            </details>

          </>}

        </div>

        <footer className="flex shrink-0 items-center justify-between gap-3 border-t border-zinc-200 p-4 dark:border-zinc-800">

          <span className="text-[11px] text-zinc-500">
            {c.confirmOpen ? '使用当前已保存版本' : '保存后，返回结果卡片开始制图'}
          </span>

          <div className="flex shrink-0 gap-2">
            <button
              className={buttonClass}
              disabled={busy}
              onClick={c.close}
            >取消</button>
            {c.confirmOpen ? <button
              className={`${buttonClass} border-blue-400 text-blue-700`}
              disabled={busy || !canEdit || c.dirty || !c.document || !hasExtractedContent(c.document.graph)}
              onClick={() => void c.confirm()}
            >
              {c.confirming ? '提交中…' : '开始制图'}
            </button> : <button
              className={`${buttonClass} border-blue-400 text-blue-700`}
              disabled={busy || !canEdit || !c.dirty}
              onClick={() => void c.save()}
            >
              {c.saving ? '保存中…' : '保存修改'}
            </button>}
          </div>

        </footer>

      </DialogPanel>

    </div>

  </Dialog>
}
