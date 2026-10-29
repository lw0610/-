'use client'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useInstanceStore } from '@/stores/instanceStore'
import { loadStyleLayerPreview, resourceApi } from './api'
import { buttonClass, fieldClass } from './PreviewPanels'
import PreviewImage from './PreviewImage'
import type { ProjectLayer } from './types'

export default function StyleLayerPreview({ resourceId, geometry }: { resourceId: string; geometry?: string }) {
  const instance = useInstanceStore(state => state.instances.find(item => item.id === state.activeInstanceId && item.status === 'running'))
  const instanceId = instance?.id
  const [layers, setLayers] = useState<ProjectLayer[]>([]), [selected, setSelected] = useState('')
  const [loading, setLoading] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState(''), [url, setURL] = useState('')
  const activeRequest = useRef<AbortController | null>(null), activeURL = useRef('')
  const dispose = useCallback(() => {
    activeRequest.current?.abort(); activeRequest.current = null
    if (activeURL.current) { URL.revokeObjectURL(activeURL.current); activeURL.current = '' }
  }, [])
  useEffect(() => {
    let active = true
    dispose(); setURL(''); setBusy(false); setSelected(''); setLayers([]); setError(''); setLoading(Boolean(instanceId))
    if (instanceId) void resourceApi.layers(instanceId).then(result => {
      if (active) setLayers(result.items.filter(item => geometry === 'raster' ? item.type === 'raster' : item.type === 'vector'))
    }).catch(reason => { if (active) setError(reason instanceof Error ? reason.message : '工程图层读取失败') })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false; dispose() }
  }, [instanceId, resourceId, geometry, dispose])
  const reset = () => { dispose(); setURL(''); setBusy(false); setError('') }
  const preview = async () => {
    if (!instanceId || !selected || busy) return
    reset()
    const request = new AbortController(); activeRequest.current = request; setBusy(true)
    try {
      const blob = await loadStyleLayerPreview(resourceId, instanceId, selected, request.signal)
      if (request.signal.aborted || activeRequest.current !== request) return
      activeURL.current = URL.createObjectURL(blob); setURL(activeURL.current)
    } catch (reason) {
      if (!request.signal.aborted && activeRequest.current === request) setError(reason instanceof Error ? reason.message : '图层预览失败')
    } finally {
      if (activeRequest.current === request) { activeRequest.current = null; setBusy(false) }
    }
  }
  if (!instanceId) return <p className="rounded border p-4 text-sm text-text-muted">请先连接并选择一个 QGIS 工程，再用图层预览样式。</p>
  return <div className="space-y-3">
    <p className="text-xs text-text-muted">选择当前工程中的图层，查看此样式的实际效果。预览使用临时副本，不会修改工程图层。</p>
    {loading ? <p role="status">正在读取工程图层…</p> : layers.length ? <div className="flex flex-wrap items-end gap-2">
      <label className="min-w-48 flex-1 text-sm">预览图层<select className={`${fieldClass} mt-1`} value={selected} onChange={event => { reset(); setSelected(event.target.value) }}><option value="">请选择图层</option>{layers.map(layer => <option key={layer.id} value={layer.id}>{layer.name}</option>)}</select></label>
      <button type="button" className={buttonClass} disabled={!selected || busy} onClick={() => void preview()}>{busy ? '正在生成…' : '生成图层预览'}</button>
      {busy && <button type="button" className={buttonClass} onClick={reset}>取消生成</button>}
    </div> : !error && <p className="text-sm text-text-muted">当前工程没有可供此样式预览的{geometry === 'raster' ? '栅格' : '矢量'}图层。</p>}
    {error && <p role="alert" className="text-sm text-red-600">{error}</p>}
    {busy && <p role="status" className="text-xs text-text-muted">正在读取所选图层并渲染，完成后显示结果。</p>}
    {url && <PreviewImage key={url} url={url} alt={`${layers.find(layer => layer.id === selected)?.name || '所选图层'}套用样式后的预览`}/>}
  </div>
}
