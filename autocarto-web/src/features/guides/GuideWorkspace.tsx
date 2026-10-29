'use client'
import { useCallback, useEffect, useRef, useState } from 'react'
import Link from 'next/link'
import MarkdownContent from '@/components/chat/MarkdownContent'
import { useConnectionStore } from '@/stores/connectionStore'
import { confirmDiscardChanges, useUnsavedChangesGuard } from '@/hooks/useUnsavedChangesGuard'
import { guideApi } from './api'
import { flowBody, guideTemplate, insertResourceReference } from './editor'
import { buttonClass, fieldClass } from './PreviewPanels'
import PathDialog, { type DialogField } from './PathDialog'
import ResourceLibrary from './ResourceLibrary'
import LayerLibrary from './LayerLibrary'
import type { GuideDetail, GuideSummary } from './types'

type Dialog = 'new' | 'import' | 'copy' | 'export' | 'delete'
export default function GuideWorkspace() {
  const ready = useConnectionStore(state => state.backendReady)
  const [page, setPage] = useState<'flows' | 'library' | 'layers'>('flows')
  const [items, setItems] = useState<GuideSummary[]>([])
  const [detail, setDetail] = useState<GuideDetail | null>(null)
  const [markdown, setMarkdown] = useState(''), [editing, setEditing] = useState(false)
  const [dialog, setDialog] = useState<Dialog | null>(null)
  const [busy, setBusy] = useState(false), [error, setError] = useState(''), [notice, setNotice] = useState(''), [search, setSearch] = useState('')
  const request = useRef(0), editor = useRef<HTMLTextAreaElement>(null)
  const dirty = Boolean(detail && markdown !== detail.guide_md), readOnly = detail?.source === 'builtin'
  useUnsavedChangesGuard(dirty)
  const select = (value: GuideDetail) => { setDetail(value); setMarkdown(value.guide_md); setError('') }
  const refresh = useCallback(async () => setItems(await guideApi.list()), [])
  const perform = async (operation: () => Promise<void>) => {
    setBusy(true); setError(''); setNotice('')
    try { await operation() } catch (reason) { setError(reason instanceof Error ? reason.message : '操作失败') } finally { setBusy(false) }
  }
  useEffect(() => {
    if (!ready) return
    let active = true
    void guideApi.list().then(value => { if (active) setItems(value) }).catch(reason => { if (active) setError(String(reason)) })
    const params = new URLSearchParams(window.location.search)
    if (params.get('tab') === 'layers') setPage('layers')
    const id = params.get('guide')
    if (id) void guideApi.get(id).then(value => { if (active) select(value) }).catch(reason => { if (active) setError(String(reason)) })
    return () => { active = false }
  }, [ready])
  const open = async (id: string) => {
    if (dirty && !confirmDiscardChanges()) return
    const ticket = ++request.current
    await perform(async () => { const value = await guideApi.get(id); if (ticket === request.current) { select(value); setEditing(false) } })
  }
  const afterWrite = async (value: GuideDetail, message: string) => {
    select(value); setNotice(message)
    try { await refresh() } catch { setError('操作已完成，但列表刷新失败，请重新打开流程页。') }
  }
  const fields: DialogField[] = dialog === 'new' ? [{key: 'name', label: '流程名称', initial: '新流程'}]
    : dialog === 'import' ? [{key: 'path', label: '流程 ZIP、FLOW.md 或文件夹路径', picker: 'both'}, {key: 'id', label: '新编号（可选）', optional: true}]
    : dialog === 'copy' ? [{key: 'id', label: '新流程编号', initial: `${detail?.meta.id || 'flow'}-copy`}, {key: 'name', label: '新流程名称', initial: `${detail?.meta.name || '流程'}（副本）`}]
    : dialog === 'export' ? [{key: 'path', label: '导出 ZIP 文件完整路径'}] : []
  return <div className="h-full overflow-y-auto p-4 text-sm text-text-primary">
    <header className="mb-4"><h1 className="text-xl font-semibold">流程</h1><p className="mt-1 text-xs text-text-muted">用一份文档说明制图要求。运行时结合实际材料确定数据来源和执行计划。</p></header>
    <nav className="mb-4 flex gap-2" aria-label="流程与资源">{([{id: 'flows', label: '流程'}, {id: 'library', label: '样式与版面'}, {id: 'layers', label: '内置图层'}] as const).map(item => <button key={item.id} className={`${buttonClass} aria-pressed:border-blue-500 aria-pressed:bg-blue-50 aria-pressed:text-blue-700 dark:aria-pressed:bg-blue-950 dark:aria-pressed:text-blue-300`} aria-pressed={page === item.id} onClick={() => setPage(item.id)}>{item.label}</button>)}</nav>
    {error && <p role="alert" className="mb-3 text-red-700">{error}</p>}{notice && <p role="status" className="mb-3 text-emerald-700">{notice}</p>}
    {page === 'layers' ? <LayerLibrary onSelect={!readOnly && detail ? layer => { setMarkdown(insertResourceReference(markdown, {label: layer.name, type: 'layer'})); setPage('flows'); setEditing(true); setNotice('已插入图层引用，请补充用途后保存流程。') } : undefined}/> : page === 'library' ? <ResourceLibrary onSelect={!readOnly && detail ? resource => { setMarkdown(insertResourceReference(markdown, {label: resource.label || resource.name || '', type: resource.type})); setPage('flows'); setEditing(true); setNotice('已插入资源引用，请补充用途后保存流程。') } : undefined}/> : <>
      <div className="mb-4 flex flex-wrap gap-2"><button disabled={busy} className={buttonClass} onClick={() => { if (!dirty || confirmDiscardChanges()) setDialog('new') }}>新建流程</button><button disabled={busy} className={buttonClass} onClick={() => { if (!dirty || confirmDiscardChanges()) setDialog('import') }}>导入流程</button><input aria-label="搜索流程" className={`${fieldClass} max-w-xs`} placeholder="搜索名称、说明或标签" value={search} onChange={event => setSearch(event.target.value)}/></div>
      <div className="grid gap-5 xl:grid-cols-[240px_minmax(0,1fr)]"><ul aria-label="流程列表" className="space-y-2">{items.filter(item => `${item.name} ${item.description} ${(item.tags || []).join(' ')}`.toLowerCase().includes(search.toLowerCase())).map(item => <li key={item.id}><button disabled={busy} aria-pressed={detail?.meta.id === item.id} onClick={() => void open(item.id)} className="w-full rounded border p-3 text-left aria-pressed:border-blue-500 aria-pressed:bg-blue-50 dark:aria-pressed:bg-blue-950"><strong>{item.name}</strong><p className="mt-1 text-xs text-text-muted">{item.description}</p><p className="mt-1 text-xs">{item.source === 'builtin' ? '内置' : '用户'} · {item.enabled ? '已启用' : '未启用'}</p></button></li>)}</ul>
        <main className="min-w-0">{detail ? <>
          <div className="mb-3 flex flex-wrap items-center gap-2"><h2 className="mr-auto font-semibold">{detail.meta.name}{readOnly ? ' · 内置只读' : ''}</h2><button disabled={busy || dirty} className={buttonClass} onClick={() => void perform(async () => afterWrite(await guideApi.setEnabled(detail.meta.id, !detail.enabled), detail.enabled ? '已停用流程。' : '已启用流程。'))}>{detail.enabled ? '停用' : '启用'}</button><button disabled={busy} className={buttonClass} onClick={() => { if (!dirty || confirmDiscardChanges()) setDialog('copy') }}>复制为我的流程</button><button disabled={busy || dirty} className={buttonClass} onClick={() => setDialog('export')}>导出流程</button>{!readOnly && <button disabled={busy} className={buttonClass} onClick={() => setDialog('delete')}>删除</button>}</div>
          <div className="mb-4 flex flex-wrap items-center gap-2">{!readOnly && <><button className={buttonClass} onClick={() => setEditing(!editing)}>{editing ? '收起编辑器' : '编辑流程'}</button><button className={buttonClass} disabled={busy || !dirty} onClick={() => void perform(async () => afterWrite(await guideApi.save(detail.meta.id, markdown), '已保存，流程预览已更新。'))}>保存</button></>}<Link aria-disabled={dirty || !detail.enabled} onClick={event => { if (dirty || !detail.enabled) event.preventDefault() }} className={`${buttonClass} ${dirty || !detail.enabled ? 'opacity-40' : ''}`} href={`/chat?guide=${encodeURIComponent(detail.meta.id)}`}>在对话中使用</Link>{dirty && <span className="text-xs text-amber-700">有未保存的内容</span>}</div>
          {detail.conversion_report && <details className="mb-3 rounded border p-2"><summary>{detail.conversion_report.message || '已自动转换，请检查'}</summary><ul>{detail.conversion_report.items?.map((item, index) => <li key={index}>{item}</li>)}</ul></details>}
          <div className="min-w-0">
            {editing && !readOnly && <div className="mb-4"><label className="mb-2 block text-xs text-text-muted" htmlFor="flow-markdown">直接编写制图要求，下方实时预览正文。</label><textarea id="flow-markdown" ref={editor} aria-label="FLOW.md 内容" className={`${fieldClass} min-h-[360px] font-mono text-xs`} value={markdown} onChange={event => setMarkdown(event.target.value)}/><button className={`${buttonClass} mt-2`} onClick={() => setPage('library')}>插入样式或版面引用</button><button className={`${buttonClass} ml-2 mt-2`} onClick={() => setPage('layers')}>插入内置图层引用</button></div>}
            <article aria-label="流程正文" className="rounded-lg border p-5"><MarkdownContent className="[&_h1]:text-xl [&_h1]:font-semibold [&_h2]:pt-3 [&_h2]:text-base [&_h2]:font-semibold [&_h3]:font-medium" content={flowBody(markdown)}/></article>
            {!!detail.diagnostics?.length && <section aria-label="流程提示" className="mt-3 space-y-2 rounded border border-amber-200 p-3">{detail.diagnostics.map((problem, index) => <p key={index} className="text-xs">{problem.line ? `第 ${problem.line} 行：` : ''}{problem.message}</p>)}{dirty && <p className="text-xs text-text-muted">保存后更新引用检查结果。</p>}</section>}
          </div>
        </> : <p className="p-8 text-text-muted">选择一份流程查看详情。</p>}</main></div>
    </>}
    {dialog && <PathDialog title={{new: '新建流程', import: '导入流程', copy: '复制为我的流程', export: '导出流程', delete: '删除流程'}[dialog]} fields={fields} checks={dialog === 'import' ? [{key: 'overwrite', label: '覆盖同编号用户流程', initial: false}] : []} description={dialog === 'delete' ? `删除「${detail?.meta.name}」？` : dialog === 'import' ? '支持 FLOW.md、旧 GUIDE.md 和 ZIP。旧格式会备份转换，完成后请查看转换报告。' : dialog === 'export' ? '导出流程文档及其引用的样式与版面，不包含制图数据。' : undefined} onClose={() => setDialog(null)} onSubmit={async (values, checks) => {
      if (dialog === 'new') { await afterWrite(await guideApi.create(guideTemplate.replace('名称: 新流程', `名称: ${JSON.stringify(values.name.trim())}`)), '流程已创建。'); setEditing(true) }
      else if (dialog === 'import') { const result = await guideApi.import(values.path, checks.overwrite, values.id || undefined); await afterWrite(result.guide, result.report?.message || '流程已导入。'); setEditing(false) }
      else if (detail) {
        if (dialog === 'copy') { await afterWrite(await guideApi.copy(detail.meta.id, values.id, values.name), '已复制为可编辑流程。'); setEditing(true) }
        if (dialog === 'export') { const result = await guideApi.export(detail.meta.id, values.path, false, true); setNotice(`已导出：${result.zip_path}`) }
        if (dialog === 'delete') { await guideApi.remove(detail.meta.id); setDetail(null); setMarkdown(''); await refresh() }
      }
    }}/>}
  </div>
}
