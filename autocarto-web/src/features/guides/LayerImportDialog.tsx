'use client'
import { useState } from 'react'
import { isDesktopRuntime, pickDirectoryPath } from '@/lib/config-form-helpers'
import { layerApi } from './api'
import { buttonClass, fieldClass } from './PreviewPanels'
import type { LayerCatalog, LayerManifest, LayerScan } from './types'

export default function LayerImportDialog({categories, initialPath = '', onClose, onImported}: {
  categories: LayerCatalog['categories']; initialPath?: string; onClose: () => void; onImported: (count: number) => void
}) {
  const [path, setPath] = useState(initialPath), [pack, setPack] = useState('')
  const [scan, setScan] = useState<LayerScan | null>(null), [skipped, setSkipped] = useState<string[]>([])
  const [busy, setBusy] = useState(false), [error, setError] = useState('')
  const perform = async (operation: () => Promise<void>) => {
    setBusy(true); setError('')
    try { await operation() } catch (reason) { setError(reason instanceof Error ? reason.message : '导入失败') } finally { setBusy(false) }
  }
  const edit = (id: string, patch: Partial<LayerManifest>) => setScan(value => value && ({...value, entries: value.entries.map(entry => entry.manifest.id === id ? {...entry, manifest: {...entry.manifest, ...patch}} : entry)}))
  return <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"><section role="dialog" aria-modal="true" aria-label="导入内置图层" className="max-h-[90vh] w-full max-w-5xl overflow-auto rounded-lg bg-surface-raised p-5 text-sm">
    <h2 className="mb-3 text-lg font-semibold">导入内置图层</h2><p className="mb-3 text-xs text-text-muted">扫描文件夹后确认每个图层的名称和用途。导入只建立登记，原始文件仍保留在原位置。</p>
    <label className="block">数据文件夹<input className={fieldClass} value={path} onChange={event => setPath(event.target.value)} /></label>
    <div className="my-2 flex gap-2">{isDesktopRuntime() && <button className={buttonClass} disabled={busy} onClick={() => void perform(async () => { const value = await pickDirectoryPath(); if (value) setPath(value) })}>选择文件夹</button>}<button className={buttonClass} disabled={busy || !path.trim()} onClick={() => void perform(async () => { const value = await layerApi.scan(path.trim(), pack || undefined); setScan(value); setPack(value.pack); setSkipped([]) })}>{busy ? '处理中…' : '扫描文件夹'}</button></div>
    {scan && <><label className="block">图层包名称<input className={fieldClass} value={pack} onChange={event => setPack(event.target.value)} /></label>
      {scan.messages?.map((message, index) => <p key={index} className="my-2 text-amber-700">{message}</p>)}
      <div className="mt-3 space-y-3">{scan.entries.map(({manifest: item, available_fields, messages, needs_confirmation}) => <fieldset key={item.id} className="rounded border p-3" aria-label={item.name}>
        <legend><label className="flex items-center gap-2"><input type="checkbox" checked={!skipped.includes(item.id)} onChange={event => setSkipped(value => event.target.checked ? value.filter(id => id !== item.id) : [...value, item.id])}/>{item.file}{item.sublayer ? ` · ${item.sublayer}` : ''}</label></legend>
        <div className="grid gap-3 sm:grid-cols-3">
          <label>名称<input className={fieldClass} value={item.name} onChange={event => edit(item.id, {name: event.target.value})}/></label>
          <label>类别<select className={fieldClass} value={item.category} onChange={event => edit(item.id, {category: event.target.value})}><option value="">请选择类别</option>{categories.map(category => <option key={category.id} value={category.id}>{category.name}</option>)}</select></label>
          <label>注记字段<select className={fieldClass} value={item.label_field || ''} onChange={event => edit(item.id, {label_field: event.target.value})}><option value="">不设置</option>{available_fields.map(field => <option key={field}>{field}</option>)}</select></label>
          <label>确认坐标系<input className={fieldClass} value={item.crs || ''} placeholder="例如 EPSG:4326" onChange={event => edit(item.id, {crs: event.target.value})}/></label>
          <label>适用比例尺分母（从）<input className={fieldClass} type="number" min="1" value={item.scale?.best_from || ''} onChange={event => edit(item.id, {scale: {...item.scale, best_from: Number(event.target.value) || undefined}})}/></label>
          <label>适用比例尺分母（到）<input className={fieldClass} type="number" min="1" value={item.scale?.best_to || ''} onChange={event => edit(item.id, {scale: {...item.scale, best_to: Number(event.target.value) || undefined}})}/></label>
        </div><p className="mt-2 text-xs text-text-muted">{item.geometry} · {item.feature_count ?? '未知'} 个要素 · 文件声明坐标系：{item.declared_crs || '未声明'}</p>
        {needs_confirmation && <p className="mt-1 text-amber-700">请确认上述识别结果后导入。</p>}{messages?.map((message, index) => <p key={index} className="mt-1 text-xs">{message}</p>)}
      </fieldset>)}</div>{!scan.entries.length && <p className="mt-3">此文件夹没有可导入的图层。</p>}</>}
    {error && <p role="alert" className="mt-3 text-red-700">{error}</p>}
    <div className="mt-4 flex justify-end gap-2"><button className={buttonClass} disabled={busy} onClick={onClose}>取消</button><button className={buttonClass} disabled={busy || !scan || !pack.trim() || scan.entries.every(entry => skipped.includes(entry.manifest.id))} onClick={() => void perform(async () => { const result = await layerApi.import(pack.trim(), scan!.entries.filter(entry => !skipped.includes(entry.manifest.id)).map(entry => entry.manifest)); onImported(result.items.length) })}>确认导入</button></div>
  </section></div>
}
