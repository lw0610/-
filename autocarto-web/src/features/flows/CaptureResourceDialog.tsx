'use client'
import { useEffect, useState } from 'react'
import { Dialog, DialogBackdrop, DialogPanel, DialogTitle } from '@headlessui/react'
import { resourceApi } from './api'
import { buttonClass, fieldClass } from './PreviewPanels'
import type { CaptureRequest, LibraryItem, ProjectLayer, ProjectLayout, ResolvedResource } from './types'

export default function CaptureResourceDialog({ instance, items, onClose, onSaved }: {
  instance: string; items: LibraryItem[]; onClose: () => void; onSaved: (result: ResolvedResource) => Promise<void>
}) {
  const [kind, setKind] = useState<CaptureRequest['kind']>('layout')
  const [name, setName] = useState(''), [layout, setLayout] = useState(''), [mainMap, setMainMap] = useState('')
  const [layouts, setLayouts] = useState<ProjectLayout[]>([]), [layers, setLayers] = useState<ProjectLayer[]>([])
  const [selected, setSelected] = useState<string[]>([])
  const [loading, setLoading] = useState(true), [busy, setBusy] = useState(false), [error, setError] = useState('')
  const [conflict, setConflict] = useState(false), [replaceID, setReplaceID] = useState('')
  useEffect(() => {
    let active = true
    setLoading(true); setError(''); setLayout(''); setMainMap(''); setSelected([]); setConflict(false); setReplaceID('')
    void (kind === 'layout' ? resourceApi.layouts(instance).then(result => {
      if (active) { setLayouts(result.layouts); setLayout(result.layouts[0]?.name || '') }
    }) : resourceApi.layers(instance).then(result => {
      if (active) {
        const vectors = result.items.filter(item => item.type === 'vector')
        setLayers(vectors)
        setSelected(kind === 'style_set' ? vectors.filter(item => item.visible).map(item => item.id) : [])
      }
    })).catch(reason => { if (active) setError(reason instanceof Error ? reason.message : '工程目录读取失败') })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [instance, kind])
  const maps = layouts.find(item => item.name === layout)?.map_items || []
  const valid = name.trim() && (kind === 'layout' ? layout && maps.length > 0 && (maps.length === 1 || mainMap) : selected.length > 0)
  const save = async (replace?: string) => {
    if (!valid || busy || loading) return
    setBusy(true); setError('')
    try {
      const result = await resourceApi.capture({ instance_id: instance, kind, name: name.trim(),
        ...(kind === 'layout' ? { layout, ...(mainMap ? { main_map_item: mainMap } : {}) } : { layers: selected }),
        ...(replace ? { replace_id: replace } : {}),
      })
      await onSaved(result)
      onClose()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '保存失败')
      if (reason && typeof reason === 'object' && 'status' in reason && reason.status === 409) setConflict(true)
    } finally { setBusy(false) }
  }
  return <Dialog open onClose={() => { if (!busy) onClose() }} className="relative z-50">
    <DialogBackdrop className="fixed inset-0 bg-black/30" />
    <div className="fixed inset-0 flex items-center justify-center p-4"><DialogPanel as="form" onSubmit={event => { event.preventDefault(); void save() }} className="max-h-[85vh] w-full max-w-lg space-y-4 overflow-auto rounded-lg bg-white p-5 shadow-xl dark:bg-zinc-900">
      <DialogTitle className="font-semibold">从当前工程保存</DialogTitle>
      <label className="block text-sm">保存种类<select className={fieldClass} disabled={busy} value={kind} onChange={event => setKind(event.target.value as CaptureRequest['kind'])}><option value="layout">版面</option><option value="style_set">图层样式 → 样式集</option><option value="style">单个图层 → 样式</option></select></label>
      {loading ? <p>正在读取当前工程…</p> : kind === 'layout' ? <>
        <label className="block text-sm">工程版面<select className={fieldClass} disabled={busy} value={layout} onChange={event => { setLayout(event.target.value); setMainMap('') }}><option value="">请选择版面</option>{layouts.map(item => <option key={item.name} value={item.name}>{item.name}</option>)}</select></label>
        {maps.length > 1 && <label className="block text-sm">主地图<select className={fieldClass} required value={mainMap} disabled={busy} onChange={event => setMainMap(event.target.value)}><option value="">请选择主地图项</option>{maps.map(item => <option key={item.uuid || item.id} value={item.uuid || item.id}>{item.id || item.name || '未命名地图'} · {item.uuid}</option>)}</select></label>}
        {layout && !maps.length && <p role="alert">此版面没有地图项，请先在 QGIS 中添加地图。</p>}
      </> : kind === 'style' ? <label className="block text-sm">工程图层<select className={fieldClass} value={selected[0] || ''} disabled={busy} onChange={event => setSelected(event.target.value ? [event.target.value] : [])}><option value="">请选择图层</option>{layers.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label> : <fieldset disabled={busy}><legend className="text-sm">选择图层（默认勾选可见矢量图层）</legend>{layers.map(item => <label key={item.id} className="mt-2 flex gap-2 text-sm"><input type="checkbox" checked={selected.includes(item.id)} onChange={event => setSelected(current => event.target.checked ? [...current, item.id] : current.filter(id => id !== item.id))}/>{item.name}{item.visible ? '' : '（隐藏）'}</label>)}{!layers.length && <p>当前工程没有可导出的矢量图层。</p>}</fieldset>}
      <label className="block text-sm">保存名称<input className={fieldClass} required value={name} disabled={busy} onChange={event => { setName(event.target.value); setConflict(false) }}/></label>
      {error && <p role="alert" className="text-sm text-red-600">{error}</p>}
      {conflict && <div className="space-y-2 text-sm"><p>可以修改名称另存，也可选择用户资源并确认替换。</p><select aria-label="替换已有模板或样式" className={fieldClass} value={replaceID} onChange={event => setReplaceID(event.target.value)}><option value="">选择已有用户资源</option>{items.filter(item => item.owner === 'library_user' && item.type === kind).map(item => <option key={item.id} value={item.id}>{item.name} · {item.id}</option>)}</select><button type="button" className={buttonClass} disabled={!replaceID || busy} onClick={() => { if (window.confirm('替换所选资源的内容？旧版本会备份，引用它的流程将使用新版本。')) void save(replaceID) }}>确认替换</button></div>}
      <div className="flex justify-end gap-2"><button type="button" className={buttonClass} disabled={busy} onClick={onClose}>取消</button><button type="submit" className={buttonClass} disabled={busy || loading || !valid}>{busy ? '正在保存…' : '保存到模板与样式库'}</button></div>
    </DialogPanel></div>
  </Dialog>
}
