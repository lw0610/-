'use client'
import { useCallback, useEffect, useState } from 'react'
import { layerApi, loadLayerPreview } from './api'
import LayerImportDialog from './LayerImportDialog'
import PreviewImage from './PreviewImage'
import { buttonClass, fieldClass } from './PreviewPanels'
import type { LayerCatalog, LayerItem, LayerManifest } from './types'

const geometryLabels: Record<string, string> = {point: '点', line: '线', polygon: '面', raster: '栅格'}
export function LayerPreview({item, large = false, revision = 0}: {item: LayerItem; large?: boolean; revision?: number}) {
  const [url, setURL] = useState(''), [error, setError] = useState('')
  useEffect(() => {
    const controller = new AbortController(); let objectURL = ''
    setURL(''); setError('')
    void loadLayerPreview(item.id, controller.signal).then(blob => {
      if (controller.signal.aborted) return
      objectURL = URL.createObjectURL(blob); setURL(objectURL)
    }).catch(reason => { if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : '预览暂不可用') })
    return () => { controller.abort(); if (objectURL) URL.revokeObjectURL(objectURL) }
  }, [item.id, item.digest, item.preview, revision])
  if (error) return <p className="p-3 text-xs text-text-muted">{error}</p>
  if (!url) return <p className="p-3 text-xs text-text-muted">正在读取预览…</p>
  if (large) return <PreviewImage url={url} alt={`${item.name}图层预览`}/>
  // eslint-disable-next-line @next/next/no-img-element
  return <img src={url} alt={`${item.name}图层预览`} className="h-28 w-full rounded bg-white object-contain" />
}
function ExtentMap({bbox}: {bbox: number[]}) {
  const [west, south, east, north] = bbox
  return <svg viewBox="0 0 360 180" role="img" aria-label="图层覆盖范围示意" className="mt-2 w-full max-w-xs rounded border bg-blue-50 dark:bg-blue-950"><path d="M0 90H360M180 0V180M90 0V180M270 0V180M0 45H360M0 135H360" stroke="currentColor" opacity="0.12"/><rect x={west + 180} y={90 - north} width={Math.max(east - west, 1)} height={Math.max(north - south, 1)} fill="#3b82f6" opacity="0.5"/><text x="5" y="14" fontSize="9" fill="currentColor">全球经纬度定位</text></svg>
}
const layerGroup = (item: LayerItem, categoryName: string) => {
  const text = `${item.category} ${categoryName}`
  return /行政|admin|boundary|province|prefecture|county/.test(text) ? '行政区' : /水|河|湖|water|river|lake/.test(text) ? '水系' : /交通|路|rail|road|transport/.test(text) ? '交通' : /居民|聚落|settlement|place/.test(text) ? '居民地' : '其他'
}
export default function LayerLibrary({onSelect}: {onSelect?: (item: LayerItem) => void}) {
  const [catalog, setCatalog] = useState<LayerCatalog>({items: [], categories: []}), [search, setSearch] = useState('')
  const [selected, setSelected] = useState<LayerItem | null>(null), [edit, setEdit] = useState<Partial<LayerManifest> | null>(null)
  const [importPath, setImportPath] = useState<string | null>(null), [preview, setPreview] = useState(false), [deleting, setDeleting] = useState(false)
  const [previewRevision, setPreviewRevision] = useState(0)
  const [busy, setBusy] = useState(false), [error, setError] = useState(''), [notice, setNotice] = useState('')
  const refresh = useCallback(async () => { const value = await layerApi.list(); setCatalog(value); return value }, [])
  useEffect(() => { void refresh().catch(reason => setError(String(reason))) }, [refresh])
  const perform = async (operation: () => Promise<void>) => { setBusy(true); setError(''); setNotice(''); try { await operation() } catch (reason) { setError(reason instanceof Error ? reason.message : '操作失败') } finally { setBusy(false) } }
  const matches = catalog.items.filter(item => `${item.name} ${item.aliases?.join(' ')} ${item.pack} ${item.category} ${catalog.categories.find(category => category.id === item.category)?.name || ''}`.toLowerCase().includes(search.toLowerCase()))
  const packs = [...new Set(matches.map(item => item.pack))]
  return <section aria-label="内置图层库" className="space-y-4">
    <div className="flex flex-wrap gap-2"><input aria-label="搜索内置图层" className={`${fieldClass} max-w-sm`} placeholder="搜索名称、别名或类别" value={search} onChange={event => setSearch(event.target.value)}/><button className={buttonClass} onClick={() => setImportPath('')}>导入图层文件夹</button></div>
    <p className="text-xs text-text-muted">登记可重复使用的本机图层。在流程正文中写“内置图层「名称」”即可引用。</p>
    {catalog.migration_suggestions?.map(item => <div key={item.path} className="rounded border border-blue-200 p-3 text-xs"><p>{item.message}</p><p className="mt-1 break-all text-text-muted">{item.path}</p><button className={`${buttonClass} mt-2`} onClick={() => setImportPath(item.path)}>扫描并导入已有数据</button></div>)}
    {error && <p role="alert" className="text-red-700">{error}</p>}{notice && <p role="status" className="text-emerald-700">{notice}</p>}
    <div className={selected ? 'grid gap-5 xl:grid-cols-[minmax(0,1fr)_360px]' : ''}><div className="space-y-5">{packs.map(pack => <section key={pack} aria-label={`图层包 ${pack}`}><h2 className="mb-2 font-semibold">{pack}</h2>{['行政区', '水系', '交通', '居民地', '其他'].map(group => {
      const items = matches.filter(item => item.pack === pack && layerGroup(item, catalog.categories.find(category => category.id === item.category)?.name || '') === group)
      if (!items.length) return null
      return <div key={group} className="mb-3"><h3 className="mb-2 text-xs text-text-muted">{group}</h3><ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">{items.map(item => <li key={item.id}><button className="w-full rounded border p-3 text-left aria-pressed:border-blue-500 aria-pressed:bg-blue-50 dark:aria-pressed:bg-blue-950" aria-pressed={selected?.id === item.id} onClick={() => { setSelected(item); setEdit(null); setDeleting(false) }}><LayerPreview item={item} revision={previewRevision}/><strong className="mt-2 block">{item.name}</strong><p className="mt-1 text-xs text-text-muted">{geometryLabels[item.geometry] || item.geometry} · {item.feature_count ?? '未知'} 个要素 · {item.scope === 'system' ? '系统内置' : '用户导入'}</p>{item.status !== 'ready' && <p className="mt-1 text-xs text-amber-700">{{missing: '原文件不存在', changed: '原文件已变化', needs_confirmation: '元信息待确认'}[item.status] || item.status}</p>}</button></li>)}</ul></div>
    })}</section>)}{!matches.length && <p className="py-8 text-text-muted">{search ? '没有匹配的图层。' : '尚未登记图层，请导入数据文件夹。'}</p>}</div>
    {selected && <aside className="min-w-0 space-y-3 rounded border p-4" aria-label="图层详情"><div className="flex items-start justify-between gap-2"><h2 className="font-semibold">{selected.name}</h2><button onClick={() => setSelected(null)} aria-label="关闭图层详情">×</button></div>
      <LayerPreview item={selected} revision={previewRevision}/><button className={buttonClass} onClick={() => setPreview(true)}>完整预览</button>{onSelect && <button className={`${buttonClass} ml-2`} onClick={() => onSelect(selected)}>插入流程引用</button>}
      <dl className="space-y-2 text-xs"><div><dt className="text-text-muted">所属包 / 类别</dt><dd>{selected.pack} / {catalog.categories.find(item => item.id === selected.category)?.name || selected.category}</dd></div><div><dt className="text-text-muted">别名</dt><dd>{selected.aliases?.join('、') || '未设置'}</dd></div><div><dt className="text-text-muted">覆盖范围</dt><dd>{selected.coverage?.regions?.join('、') || '未标注地区'}{selected.coverage?.bbox?.length === 4 && <><p>{selected.coverage.bbox.join(', ')}</p><ExtentMap bbox={selected.coverage.bbox}/></>}</dd></div><div><dt className="text-text-muted">推荐比例尺</dt><dd>{selected.scale?.best_from || selected.scale?.best_to ? `1:${selected.scale.best_from || '不限'} ～ 1:${selected.scale.best_to || '不限'}` : '未设置'}</dd></div><div><dt className="text-text-muted">注记字段 / 默认样式</dt><dd>{selected.label_field || '未设置'} / {selected.default_style || '未设置'}</dd></div><div><dt className="text-text-muted">坐标系</dt><dd>文件声明：{selected.declared_crs || '未声明'}<br/>实际使用：{selected.crs || '未确认'}</dd></div><div><dt className="text-text-muted">来源 / 许可</dt><dd>{selected.source?.title || '未记录'} · {selected.source?.license || '未记录许可'}<p>{selected.source?.attribution}</p><p>{selected.compliance || '未记录合规说明'}</p>{selected.source?.url && <a href={selected.source.url} target="_blank" rel="noreferrer" className="break-all text-blue-600">查看来源</a>}</dd></div><div><dt className="text-text-muted">原始文件</dt><dd className="break-all">{selected.file}{selected.sublayer && ` · ${selected.sublayer}`}</dd></div></dl>
      {selected.issues?.map((issue, index) => <p key={index} className="text-xs text-amber-700">{issue}</p>)}
      <div className="flex flex-wrap gap-2"><button className={buttonClass} disabled={busy} onClick={() => void perform(async () => { setSelected(await layerApi.regeneratePreview(selected.id)); setPreviewRevision(value => value + 1); await refresh(); setNotice('预览已更新。') })}>更新预览</button>{selected.scope === 'user' && <><button className={buttonClass} disabled={busy} onClick={() => setEdit({name: selected.name, aliases: selected.aliases || [], label_field: selected.label_field || '', crs: selected.crs || '', scale: selected.scale || {}})}>编辑元信息</button><button className={buttonClass} disabled={busy} onClick={() => setDeleting(true)}>删除登记</button></>}</div>
      {edit && <form className="space-y-2 text-xs" onSubmit={event => { event.preventDefault(); void perform(async () => { setSelected(await layerApi.update(selected.id, {...edit, aliases: edit.aliases?.map(value => value.trim()).filter(Boolean)})); setEdit(null); await refresh(); setNotice('元信息已保存。') }) }}><label className="block">图层名称<input required className={fieldClass} value={edit.name} onChange={event => setEdit({...edit, name: event.target.value})}/></label><label className="block">别名（逗号分隔）<input className={fieldClass} value={edit.aliases?.join('，')} onChange={event => setEdit({...edit, aliases: event.target.value.split(/[,，]/)})}/></label><label className="block">注记字段<input className={fieldClass} value={edit.label_field} onChange={event => setEdit({...edit, label_field: event.target.value})}/></label><label className="block">实际使用坐标系<input className={fieldClass} value={edit.crs} onChange={event => setEdit({...edit, crs: event.target.value})}/></label>{(['best_from', 'best_to'] as const).map((key, index) => <label key={key} className="block">比例尺分母（{index ? '到' : '从'}）<input type="number" min="1" className={fieldClass} value={edit.scale?.[key] || ''} onChange={event => setEdit({...edit, scale: {...edit.scale, [key]: Number(event.target.value) || undefined}})}/></label>)}<button className={buttonClass} disabled={busy}>保存元信息</button><button type="button" className={`${buttonClass} ml-2`} onClick={() => setEdit(null)}>取消编辑</button></form>}
      {deleting && <div className="rounded border border-amber-200 p-2 text-xs"><p>删除「{selected.name}」的登记？原始数据文件保留，引用此图层的流程需要重新选择数据。</p><button className={`${buttonClass} mt-2`} disabled={busy} onClick={() => void perform(async () => { await layerApi.remove(selected.id); setSelected(null); setDeleting(false); await refresh(); setNotice('已删除登记，原始文件保留。') })}>确认删除登记</button><button className={`${buttonClass} ml-2`} onClick={() => setDeleting(false)}>取消</button></div>}
    </aside>}</div>
    {importPath !== null && <LayerImportDialog categories={catalog.categories} initialPath={importPath} onClose={() => setImportPath(null)} onImported={count => { setImportPath(null); setNotice(`已导入 ${count} 个图层。`); void refresh().catch(reason => setError(String(reason))) }}/>} 
    {preview && selected && <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"><section role="dialog" aria-modal="true" aria-label={`${selected.name}完整预览`} className="w-full max-w-5xl rounded-lg bg-surface-raised p-4"><header className="mb-3 flex justify-between"><h2>{selected.name}</h2><button className={buttonClass} onClick={() => setPreview(false)}>关闭预览</button></header><LayerPreview item={selected} large revision={previewRevision}/></section></div>}
  </section>
}
