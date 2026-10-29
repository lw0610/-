'use client'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useInstanceStore } from '@/stores/instanceStore'
import CaptureResourceDialog from './CaptureResourceDialog'
import { resourceApi } from './api'
import type { LibraryItem, ResolvedResource, ResourceReference, ResourceTypeDefinition, ResourceWriteResult } from './types'
import { buttonClass, fieldClass } from './PreviewPanels'
import PathDialog, { type DialogField } from './PathDialog'
import ResourceMedia, { ResourceThumbnail, geometryLabel } from './ResourceMedia'

type ResourceDialog = {kind: 'import'} | {kind: 'copy' | 'replace' | 'delete' | 'export'; item: ResolvedResource; references?: ResourceReference[]}
const builtin = (item: { owner?: string; origin?: string }) => item.owner === 'library_builtin' || Boolean(item.origin?.includes('builtin'))
const identity = (item: { id?: string }) => { if (!item.id) throw new Error('资源缺少编号，请刷新资源库后重试'); return item.id }
const filterButtonClass = `${buttonClass} aria-selected:border-blue-500 aria-selected:bg-blue-50 aria-selected:font-semibold aria-selected:text-blue-700 dark:aria-selected:bg-blue-950 dark:aria-selected:text-blue-300`
const normalized = (result: ResourceWriteResult) => 'items' in result ? result : {items: [result], warnings: []}

export default function ResourceLibrary({ onSelect }: { onSelect?: (resource: ResolvedResource, types: ResourceTypeDefinition[]) => void }) {
  const [items, setItems] = useState<LibraryItem[]>([]), [types, setTypes] = useState<ResourceTypeDefinition[]>([])
  const [selected, setSelected] = useState<ResolvedResource | null>(null), [references, setReferences] = useState<ResourceReference[] | null>(null)
  const [type, setType] = useState('style'), [geometry, setGeometry] = useState(''), [origin, setOrigin] = useState(''), [filter, setFilter] = useState('')
  const [capture, setCapture] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState(''), [notice, setNotice] = useState('')
  const [dialog, setDialog] = useState<ResourceDialog | null>(null), [editing, setEditing] = useState(false)
  const [name, setName] = useState(''), [description, setDescription] = useState(''), [tags, setTags] = useState('')
  const instance = useInstanceStore(state => state.instances.find(item => item.id === state.activeInstanceId && item.status === 'running'))
  const request = useRef(0)
  const refresh = useCallback(async () => { const result = await resourceApi.list(); setItems(result.filter(item => ['style', 'layout'].includes(item.type))); return result }, [])
  useEffect(() => {
    let active = true
    void Promise.all([resourceApi.list(), resourceApi.types()]).then(([listed, definitions]) => {
      if (active) { setItems(listed.filter(item => ['style', 'layout'].includes(item.type))); setTypes(definitions.filter(item => ['style', 'layout'].includes(item.type))) }
    }).catch(reason => { if (active) setError(reason instanceof Error ? reason.message : '资源库加载失败') })
    return () => { active = false }
  }, [])
  useEffect(() => { setName(selected?.name || selected?.label || ''); setDescription(selected?.description || ''); setTags((selected?.tags || []).join('、')); setEditing(false) }, [selected])
  const perform = async (operation: () => Promise<void>) => {
    setBusy(true); setError(''); setNotice('')
    try { await operation() } catch (reason) { setError(reason instanceof Error ? reason.message : '操作失败') } finally { setBusy(false) }
  }
  const afterWrite = async (result: ResourceWriteResult | null, message: string) => {
    const batch = result ? normalized(result) : {items: [], warnings: []}
    setSelected(batch.items[0] || null); setReferences(null)
    if (batch.items[0]) setType(batch.items[0].type)
    setNotice([message, batch.items.length > 1 ? `共 ${batch.items.length} 项：${batch.items.map(item => item.name || item.label).join('、')}` : '', ...(batch.warnings || []), ...batch.items.flatMap(item => (item.problems || []).map(problem => problem.message))].filter(Boolean).join('；'))
    if (batch.items[0]?.id) { try { setReferences(await resourceApi.references(batch.items[0].id)) } catch { /* Keep usage unknown until it can be loaded. */ } }
    try { await refresh() } catch { setError('操作已完成，但列表刷新失败，请重新打开样式与版面页。') }
  }
  const open = (item: LibraryItem) => {
    const sequence = ++request.current
    void perform(async () => { const [result, usage] = await Promise.all([resourceApi.get(identity(item)), resourceApi.references(identity(item))]); if (sequence === request.current) { setSelected(result); setReferences(usage) } })
  }
  const fields: DialogField[] = dialog?.kind === 'import' ? [{key: 'path', label: '资源文件或文件夹路径', picker: 'both'}, {key: 'name', label: '资源名称（可选）', optional: true}]
    : dialog?.kind === 'copy' ? [{key: 'name', label: '资源名称', initial: `${dialog.item.name || dialog.item.label}（副本）`}]
    : dialog?.kind === 'replace' ? [{key: 'path', label: '替换为文件', picker: 'file'}]
    : dialog?.kind === 'export' ? [{key: 'path', label: '导出 ZIP 文件完整路径'}] : []
  const visible = items.filter(item => item.type === type && (!geometry || item.geometry === geometry) && (!origin || (builtin(item) ? 'builtin' : 'user') === origin) && `${item.name} ${item.description || ''} ${(item.tags || []).join(' ')}`.toLowerCase().includes(filter.toLowerCase()))
  return <div className="space-y-4">
    <div className="flex flex-wrap items-center gap-3"><div role="tablist" aria-label="资源种类" className="flex gap-2">{[{id: 'style', label: '样式'}, {id: 'layout', label: '版面'}].map(item => <button role="tab" key={item.id} aria-selected={type === item.id} className={filterButtonClass} onClick={() => { setType(item.id); setGeometry(''); setSelected(null) }}>{item.label}</button>)}</div><button disabled={busy} className={buttonClass} onClick={() => setDialog({kind: 'import'})}>导入模板或样式</button><button disabled={busy || !instance} title={instance ? undefined : '请先连接 QGIS 工程'} className={buttonClass} onClick={() => setCapture(true)}>从当前工程保存</button></div>
    <div className="flex flex-wrap gap-3">{type === 'style' && <select aria-label="样式几何类型" className={fieldClass.replace('w-full', 'w-40')} value={geometry} onChange={event => setGeometry(event.target.value)}><option value="">全部几何</option>{['point', 'line', 'polygon', 'raster'].map(value => <option key={value} value={value}>{geometryLabel(value)}</option>)}</select>}<select aria-label="资源来源" className={fieldClass.replace('w-full', 'w-40')} value={origin} onChange={event => setOrigin(event.target.value)}><option value="">全部来源</option><option value="builtin">内置</option><option value="user">用户</option></select><input aria-label="搜索样式与版面" className={`${fieldClass} max-w-sm`} value={filter} onChange={event => setFilter(event.target.value)} placeholder="搜索名称、标签或说明"/></div>
    {error && <p role="alert" className="text-sm text-red-600">{error}</p>}{notice && <p role="status" className="text-sm text-emerald-700">{notice}</p>}
    <div className={`grid gap-5 ${selected ? 'xl:grid-cols-[minmax(0,1fr)_340px]' : ''}`}><ul aria-label="样式与版面列表" className="grid content-start grid-cols-2 gap-3 lg:grid-cols-3 2xl:grid-cols-4">{visible.map(item => <li key={identity(item)}><button disabled={busy} aria-pressed={selected?.id === item.id} className="w-full overflow-hidden rounded-lg border p-2 text-left aria-pressed:border-blue-500 aria-pressed:bg-blue-50 dark:aria-pressed:bg-blue-950" onClick={() => open(item)}><ResourceThumbnail resource={item}/><strong className="mt-2 block truncate text-sm">{item.name}</strong><p className="mt-1 text-xs text-text-muted">{type === 'style' ? geometryLabel(item.geometry) : [item.paper, item.orientation].filter(Boolean).join(' · ') || '版面模板'} · {builtin(item) ? '内置' : '用户'}</p></button></li>)}{!visible.length && <li className="col-span-full py-8 text-sm text-text-muted">暂无符合条件的资源。</li>}</ul>
      {selected && <section aria-label="资源详情" className="min-w-0 space-y-3 rounded-lg border p-4"><h2 className="font-semibold">{selected.name || selected.label}</h2><ResourceMedia resource={selected}/><p className="text-xs text-text-muted">{builtin(selected) ? '内置 · 只读' : '用户'} · {selected.type === 'style' ? `适用几何：${geometryLabel(selected.geometry)}` : [selected.paper, selected.orientation].filter(Boolean).join(' · ')}</p>{selected.type === 'style' ? <p className="text-xs">标注字段：{selected.label_field || '无'}</p> : <p className="text-xs">版面元素：{selected.elements?.join('、') || '按模板'}</p>}<p className="text-sm whitespace-pre-wrap">{selected.description || '暂无说明'}</p>{!!selected.tags?.length && <p className="text-xs text-text-muted">标签：{selected.tags.join('、')}</p>}{selected.source && <p className="text-xs text-text-muted">{[selected.source.author, selected.source.license, selected.source.upstream].filter(Boolean).join(' · ')}</p>}
        <div className="flex flex-wrap gap-2"><button className={buttonClass} disabled={busy} onClick={() => setDialog({kind: 'copy', item: selected})}>复制为我的资源</button>{!builtin(selected) && <><button className={buttonClass} disabled={busy} onClick={() => setEditing(!editing)}>重命名与编辑说明</button><button className={buttonClass} disabled={busy} onClick={() => void perform(async () => setDialog({kind: 'replace', item: selected, references: await resourceApi.references(identity(selected))}))}>替换文件</button><button className={buttonClass} disabled={busy} onClick={() => void perform(async () => setDialog({kind: 'delete', item: selected, references: await resourceApi.references(identity(selected))}))}>删除</button></>}<button className={buttonClass} disabled={busy || !instance} onClick={() => void perform(async () => afterWrite(await resourceApi.regeneratePreview(identity(selected)), '已请求重新生成预览。'))}>重新生成预览</button><button className={buttonClass} disabled={busy} onClick={() => setDialog({kind: 'export', item: selected})}>导出</button></div>
        {editing && <form className="space-y-2 border-t pt-3" onSubmit={event => { event.preventDefault(); void perform(async () => afterWrite(await resourceApi.update(identity(selected), {name: name.trim(), description, tags: tags.split(/[、,，]/).map(item => item.trim()).filter(Boolean)}), '资源说明已保存。')) }}><label className="block text-xs">名称<input required className={fieldClass} value={name} onChange={event => setName(event.target.value)}/></label><label className="block text-xs">说明<textarea className={fieldClass} value={description} onChange={event => setDescription(event.target.value)}/></label><label className="block text-xs">标签（用顿号分隔）<input className={fieldClass} value={tags} onChange={event => setTags(event.target.value)}/></label>{!!references?.length && <p className="text-xs text-amber-700">此资源被下列流程引用，重命名时请留意引用更新提示。</p>}<button type="submit" disabled={busy} className={buttonClass}>保存说明</button></form>}
        <section aria-label="引用此资源的流程" className="border-t pt-3"><h3 className="text-xs font-medium">被哪些流程引用</h3>{references ? <ReferenceList items={references}/> : <p className="mt-2 text-xs text-text-muted">引用信息暂未加载，请重新选择资源。</p>}</section>{onSelect && <button disabled={busy} className={buttonClass} onClick={() => onSelect(selected, types)}>引用到当前流程</button>}
      </section>}
    </div>
    {capture && instance && <CaptureResourceDialog instance={instance.id} items={items} onClose={() => setCapture(false)} onSaved={result => afterWrite(result, '已从当前工程保存。')}/>}
    {dialog && <PathDialog key={dialog.kind} title={dialog.kind === 'import' ? '导入模板或样式' : dialog.kind === 'copy' ? '复制为我的资源' : dialog.kind === 'replace' ? `替换「${dialog.item.name || dialog.item.label}」` : dialog.kind === 'export' ? '导出资源' : `删除「${dialog.item.name || dialog.item.label}」`} fields={fields} submitLabel={dialog.kind === 'delete' ? '确认删除' : '确定'} description={dialog.kind === 'import' ? <p>支持 QML 样式、QPT 或 .layout.yaml 版面。文件夹中的资源会分别导入，同名项自动加后缀；单个文件重名时请另取名称。</p> : (dialog.kind === 'replace' || dialog.kind === 'delete') ? <><p>{dialog.references?.length ? '以下流程正在引用此资源：' : '当前没有流程引用此资源。'}</p><ReferenceList items={dialog.references || []}/>{dialog.kind === 'replace' && <p>原内容会备份，确认后替换。</p>}</> : undefined} onClose={() => setDialog(null)} onSubmit={async values => {
      if (dialog.kind === 'import') await afterWrite(await resourceApi.import(values.path, values.name), '已导入。')
      else if (dialog.kind === 'copy') await afterWrite(await resourceApi.copy(identity(dialog.item), false, values.name), '已复制为我的资源。')
      else if (dialog.kind === 'replace') await afterWrite(await resourceApi.replace(identity(dialog.item), values.path), '已替换，原内容已备份。')
      else if (dialog.kind === 'delete') { await resourceApi.remove(identity(dialog.item), Boolean(dialog.references?.length)); await afterWrite(null, '资源已删除。') }
      else { const result = await resourceApi.export(identity(dialog.item), values.path); setNotice(`已导出：${result.path || result.zip_path}`) }
    }}/>}
  </div>
}

function ReferenceList({ items }: { items: ResourceReference[] }) {
  return items.length ? <ul className="mt-2 space-y-1 text-xs">{items.map(item => <li key={`${item.source}/${item.flow_id}/${item.via}`}><a className="text-blue-600 underline" href={`/guides?guide=${encodeURIComponent(item.flow_id)}`}>{item.flow_name}</a> · {item.source === 'builtin' ? '内置' : '用户'}</li>)}</ul> : <p className="mt-2 text-xs text-text-muted">暂无引用。</p>
}
