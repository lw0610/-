'use client'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useInstanceStore } from '@/stores/instanceStore'
import CaptureResourceDialog from './CaptureResourceDialog'
import { flowApi, resourceApi } from './api'
import type { LibraryItem, ResolvedResource, ResourceReference, ResourceTypeDefinition } from './types'
import { buttonClass, fieldClass, ResourcePreview } from './PreviewPanels'
import PathDialog, { type DialogField } from './PathDialog'
import ResourceMedia from './ResourceMedia'

type ResourceDialog =
  | { kind: 'import'; path?: string; rename?: boolean }
  | { kind: 'copy'; item: LibraryItem; override: boolean }
  | { kind: 'replace'; item: LibraryItem; references: ResourceReference[]; path?: string }
  | { kind: 'delete'; item: LibraryItem; references: ResourceReference[] }

const builtin = (item: { owner?: string; origin?: string }) =>
  item.owner === 'library_builtin' || Boolean(item.origin?.includes('builtin'))
const identity = (item: { id?: string }) => {
  if (!item.id) throw new Error('资源缺少编号，请刷新资源库后重试')
  return item.id
}
const filterButtonClass = `${buttonClass} aria-pressed:border-blue-500 aria-pressed:bg-blue-50 aria-pressed:font-semibold aria-pressed:text-blue-700 aria-pressed:hover:bg-blue-100 dark:aria-pressed:border-blue-400 dark:aria-pressed:bg-blue-950 dark:aria-pressed:text-blue-300 dark:aria-pressed:hover:bg-blue-900`

export default function ResourceLibrary({ onSelect }: { onSelect?: (resource: ResolvedResource, types: ResourceTypeDefinition[]) => void }) {
  const [items, setItems] = useState<LibraryItem[]>([])
  const [types, setTypes] = useState<ResourceTypeDefinition[]>([])
  const [selected, setSelected] = useState<ResolvedResource | null>(null)
  const [references, setReferences] = useState<ResourceReference[] | null>(null)
  const [type, setType] = useState('')
  const [retired, setRetired] = useState<LibraryItem[]>([])
  const [capture, setCapture] = useState(false)
  const [aliases, setAliases] = useState<Record<string, string>>({})
  const instance = useInstanceStore(state => state.instances.find(item => item.id === state.activeInstanceId && item.status === 'running'))
  const [filter, setFilter] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [dialog, setDialog] = useState<ResourceDialog | null>(null)
  const [conflict, setConflict] = useState<{ path: string; name: string } | null>(null)
  const [replacementID, setReplacementID] = useState('')
  const request = useRef(0)
  const refresh = useCallback(async () => {
    const [result, old] = await Promise.all([resourceApi.list(), resourceApi.retired()])
    setItems(result)
    setRetired(old)
    return result
  }, [])
  useEffect(() => {
    let active = true
    void Promise.all([resourceApi.list(), resourceApi.types(), resourceApi.retired()]).then(([listed, definitions, old]) => {
      if (!active) return
      setItems(listed)
      setTypes(definitions.filter(item => item.public))
      setRetired(old)
    }).catch(err => { if (active) setError(err instanceof Error ? err.message : '资源库加载失败') })
    return () => { active = false }
  }, [])
  useEffect(() => {
    setAliases(Object.fromEntries((selected?.entries || []).map(entry => [entry.name, Array.isArray(entry.fields?.aliases) ? entry.fields.aliases.join('、') : entry.extra?.aliases || ''])))
  }, [selected])
  const perform = async (operation: () => Promise<void>) => {
    setBusy(true)
    setError('')
    setNotice('')
    try { await operation() }
    catch (err) { setError(err instanceof Error ? err.message : '操作失败') }
    finally { setBusy(false) }
  }
  const afterWrite = async (result: ResolvedResource | null, message: string) => {
    setSelected(result)
    setReferences(null)
    setNotice(message)
    try { await refresh() }
    catch { setError('操作已完成，但资源列表刷新失败，请重新打开模板与样式页。') }
  }
  const open = (item: LibraryItem) => {
    const sequence = ++request.current
    void perform(async () => {
      const result = await resourceApi.get(identity(item))
      if (sequence === request.current) { setSelected(result); setReferences(null) }
    })
  }
  const showImpact = async (kind: 'replace' | 'delete', item: LibraryItem, path?: string) => {
    const result = await resourceApi.references(identity(item))
    setConflict(null)
    setDialog({ kind, item, references: result, ...(kind === 'replace' ? { path } : {}) })
  }
  const fields = (): DialogField[] => {
    if (!dialog) return []
    if (dialog.kind === 'import') return [
      { key: 'path', label: '资源文件或文件夹路径', initial: dialog.path, picker: 'both' },
      { key: 'name', label: dialog.rename ? '新的资源名称' : '资源名称（可选）', optional: !dialog.rename },
    ]
    if (dialog.kind === 'copy') return [{ key: 'name', label: '资源名称', initial: dialog.override ? dialog.item.name : `${dialog.item.name}（副本）` }]
    if (dialog.kind === 'replace') return [{ key: 'path', label: '替换为文件或文件夹', picker: 'both', initial: dialog.path }]
    return []
  }
  const impact = dialog && (dialog.kind === 'replace' || dialog.kind === 'delete') ? dialog.references : null
  const visible = items.filter(item => (!type || item.type === type) && `${item.name} ${item.id || ''}`.toLowerCase().includes(filter.toLowerCase()))
  const sourceName = (id: string) => items.find(item => item.id === id)?.name || id
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end gap-3">
        <div className="flex flex-wrap gap-2" aria-label="模板与样式筛选">
          <button className={filterButtonClass} aria-pressed={!type} onClick={() => setType('')}>全部</button>
          {types.map(item => <button key={item.type} className={filterButtonClass} aria-pressed={type === item.type} onClick={() => setType(item.type)}>{item.label}</button>)}
        </div>
        <button disabled={busy || !types.length} className={buttonClass} onClick={() => { setConflict(null); setDialog({ kind: 'import' }) }}>导入模板或样式</button>
        <button disabled={busy || !instance} title={instance ? undefined : '请先连接 QGIS 工程'} className={buttonClass} onClick={() => setCapture(true)}>从当前工程保存</button>
        {!instance && <span className="text-xs text-zinc-500">请先连接 QGIS 工程，才能保存版面和图层样式。</span>}
        <input aria-label="搜索模板与样式" className={`${fieldClass} max-w-xs`} value={filter} onChange={event => setFilter(event.target.value)} placeholder="搜索资源名称或编号" />
      </div>
      <p className="text-xs text-zinc-500">同名资源需要明确选择。使用“复制并覆盖内置”时，引用该内置资源的流程会使用你保存的资源。</p>
      {error && <p role="alert" className="text-sm text-red-600">{error}</p>}
      {notice && <p role="status" className="text-sm text-emerald-700">{notice}</p>}
      <div className="grid gap-4 lg:grid-cols-2">
        <ul aria-label="模板与样式列表" className="space-y-2">
          {visible.map(item => <li key={item.id || `${item.origin}/${item.path}`} className="rounded border border-zinc-200 p-3 dark:border-zinc-800">
            <button className="text-left text-sm font-medium" disabled={busy} onClick={() => open(item)}>{item.name}</button>
            <p className="mt-1 text-xs text-zinc-500">{builtin(item) ? '内置 · 只读' : '用户'} · {(item.size / 1024).toFixed(1)} KB</p>
            <p className="mt-1 break-all text-xs text-zinc-500">编号：{item.id || '缺少编号'}</p>
            {item.overrides && <p className="mt-1 text-xs text-blue-600">覆盖内置「{sourceName(item.overrides)}」</p>}
            {item.overridden_by && <p className="mt-1 text-xs text-amber-700">已被用户资源覆盖：{sourceName(item.overridden_by)}</p>}
            {item.legacy && <p className="mt-1 text-xs text-amber-700">旧式资源，尚无资源清单</p>}
            {item.problems?.map((problem, i) => <p key={i} className="mt-1 text-xs text-amber-700">{problem.message}</p>)}
            <div className="mt-2 flex flex-wrap gap-2">
              <button disabled={busy} className={buttonClass} onClick={() => setDialog({ kind: 'copy', item, override: false })}>复制为我的资源</button>
              {builtin(item) ? <button disabled={busy || Boolean(item.overridden_by)} className={buttonClass} onClick={() => setDialog({ kind: 'copy', item, override: true })}>复制并覆盖内置</button> : <>
                <button disabled={busy} className={buttonClass} onClick={() => void perform(() => showImpact('replace', item))}>替换内容</button>
                <button disabled={busy} className={buttonClass} onClick={() => void perform(() => showImpact('delete', item))}>删除</button>
                {item.legacy && <button disabled={busy} className={buttonClass} onClick={() => void perform(async () => { await afterWrite(await resourceApi.upgrade(identity(item)), '已生成资源清单。') })}>升级</button>}
              </>}
              <button disabled={busy} className={buttonClass} onClick={() => void perform(async () => {
                const [result, usage] = await Promise.all([resourceApi.get(identity(item)), resourceApi.references(identity(item))])
                setSelected(result); setReferences(usage)
              })}>查看引用</button>
            </div>
          </li>)}
          {!visible.length && <li className="py-8 text-sm text-zinc-500">暂无这类资源。</li>}
        </ul>
        <div>
          {selected ? <>
            <ResourcePreview resource={selected} types={types} busy={busy} />
            <ResourceMedia resource={selected} />
            {selected.type === 'style_set' && selected.owner === 'library_user' && <section className="mt-3 space-y-2" aria-label="调整样式集别名">
              <p className="text-xs">每个样式对应一个图层。可填写其他图层名称作为别名，用顿号分隔。</p>
              {selected.entries.map(entry => <label key={entry.name} className="block text-xs">{entry.name}的别名<input className={fieldClass} value={aliases[entry.name] || ''} onChange={event => setAliases(current => ({ ...current, [entry.name]: event.target.value }))}/></label>)}
              <button className={buttonClass} disabled={busy} onClick={() => void perform(async () => { await afterWrite(await resourceApi.updateAliases(identity(selected), aliases), '已保存别名，旧版本已备份。') })}>保存别名</button>
            </section>}
            {references !== null && <section aria-label="引用此资源的流程" className="mt-3 rounded border border-zinc-200 p-3 dark:border-zinc-800">
              <p className="text-sm font-medium">引用此资源的流程（{references.length}）</p>
              <ReferenceList items={references} />
            </section>}
            {onSelect && <button disabled={busy || !types.length || selected.problems?.some(item => item.level === 'error')} className={`${buttonClass} mt-3`} onClick={() => onSelect(selected, types)}>引用到当前流程</button>}
          </> : <p className="text-sm text-zinc-500">选择资源，查看内容、覆盖关系、条目和存在状态。</p>}
        </div>
      </div>
      {!!retired.length && <details className="rounded border p-3"><summary>旧资源（待迁移） · {retired.length}</summary>
        {retired.map(item => <div key={item.id} className="my-3 text-sm"><p>{item.name}</p><ReferenceList items={item.references || []}/>
          {(item.references || []).map(ref => <button key={ref.flow_id} className={`${buttonClass} mt-2 mr-2`} disabled={busy} onClick={() => void perform(async () => {
            if (ref.source === 'builtin') { setNotice('请先在流程中心复制内置流程，再把旧资源移入副本。'); return }
            await flowApi.adoptResource(ref.flow_id, identity(item), ref.label || item.name)
            await afterWrite(null, `已移入流程「${ref.flow_name}」。`)
          })}>移入流程「{ref.flow_name}」</button>)}
          {!item.references?.length && <p className="text-xs text-zinc-500">没有引用此资源的流程；请先在用户流程中声明该资源。</p>}
        </div>)}
      </details>}
      {capture && instance && <CaptureResourceDialog instance={instance.id} items={items} onClose={() => setCapture(false)} onSaved={result => afterWrite(result, '已从当前工程保存。')}/>}
      {dialog && <PathDialog
        key={`${dialog.kind}/${'item' in dialog ? dialog.item.id : `${dialog.path || ''}/${dialog.rename || false}`}`}
        title={dialog.kind === 'import' ? (dialog.rename ? '另存为新名称' : '导入模板或样式') : dialog.kind === 'copy' ? (dialog.override ? '复制并覆盖内置' : '复制为我的资源') : dialog.kind === 'replace' ? `替换「${dialog.item.name}」的内容` : `删除模板或样式「${dialog.item.name}」`}
        fields={fields()}
        submitLabel={dialog.kind === 'delete' ? '确认删除' : '确定'}
        description={<>
          {dialog.kind === 'copy' && dialog.override && <p>引用此内置资源的流程将使用你的版本；删除覆盖资源后恢复使用内置版本。</p>}
          {impact && <div><p>{impact.length ? `以下 ${impact.length} 个流程会受到影响：` : '当前没有流程引用此资源。'}</p><ReferenceList items={impact} />{dialog.kind === 'replace' && <p className="mt-2">替换前校验资源内容，旧版本会自动备份。</p>}</div>}
          {dialog.kind === 'import' && conflict && <div className="space-y-2">
            <p>同名资源已存在。选择要替换的用户资源，或另存为新名称。</p>
            <select aria-label="要替换的模板或样式" className={fieldClass} value={replacementID} onChange={event => setReplacementID(event.target.value)}>
              <option value="">选择现有用户资源</option>
              {items.filter(item => (!type || item.type === type) && !builtin(item)).map(item => <option key={item.id} value={item.id}>{item.name} · {item.id}</option>)}
            </select>
            <div className="flex gap-2">
              <button type="button" disabled={!replacementID || busy} className={buttonClass} onClick={() => void perform(async () => {
                const item = items.find(item => item.id === replacementID)
                if (item) await showImpact('replace', item, conflict.path)
              })}>替换现有资源</button>
              <button type="button" className={buttonClass} onClick={() => { setDialog({ kind: 'import', path: conflict.path, rename: true }); setConflict(null) }}>另存为新名称</button>
            </div>
          </div>}
        </>}
        onClose={() => { setDialog(null); setConflict(null) }}
        onSubmit={async values => {
          if (dialog.kind === 'import') {
            let result: ResolvedResource
            try { result = await resourceApi.import(values.path, values.name) }
            catch (err) {
              if (err && typeof err === 'object' && 'status' in err && err.status === 409) {
                setConflict({ path: values.path, name: values.name })
                await refresh()
              }
              throw err
            }
            await afterWrite(result, '已导入模板或样式。')
          } else if (dialog.kind === 'copy') {
            await afterWrite(await resourceApi.copy(identity(dialog.item), dialog.override, values.name), dialog.override ? '已复制并覆盖内置资源。' : '已复制为我的资源。')
          } else if (dialog.kind === 'replace') {
            await afterWrite(await resourceApi.replace(identity(dialog.item), values.path), '资源已替换，旧版本已备份。')
          } else if (dialog.kind === 'delete') {
            await resourceApi.remove(identity(dialog.item), dialog.references.length > 0)
            await afterWrite(null, '模板或样式已删除。')

          }
        }}
      />}
    </div>
  )
}

function ReferenceList({ items }: { items: ResourceReference[] }) {
  if (!items.length) return <p className="mt-2 text-xs text-zinc-500">暂无引用。</p>
  return <ul className="mt-2 space-y-1 text-xs">{items.map(item => <li key={`${item.source}/${item.flow_id}/${item.via}`}>{item.flow_name}（{item.flow_id}） · {item.source === 'builtin' ? '内置流程' : '用户流程'} · {item.via === 'lock' ? '已锁定引用' : '按名称引用'}</li>)}</ul>
}
