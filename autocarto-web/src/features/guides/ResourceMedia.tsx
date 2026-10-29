'use client'
import { useEffect, useState } from 'react'
import { Dialog, DialogBackdrop, DialogPanel, DialogTitle } from '@headlessui/react'
import { useInstanceStore } from '@/stores/instanceStore'
import { loadResourcePreview } from './api'
import { buttonClass } from './PreviewPanels'
import PreviewImage from './PreviewImage'
import StyleLayerPreview from './StyleLayerPreview'
import type { ResourceIdentity } from './types'

type VisualResource = ResourceIdentity & {type?: string; label?: string}
export const geometryLabel = (value?: string) => ({ point: '点', line: '线', polygon: '面', raster: '栅格', Point: '点', LineString: '线', Polygon: '面' }[value || ''] || value || '未说明几何')

function useResourceImage(resource: VisualResource) {
  const [url, setURL] = useState(''), [error, setError] = useState('')
  const connected = useInstanceStore(state => state.instances.some(item => item.status === 'running'))
  useEffect(() => {
    setURL(''); setError('')
    let active = true, objectURL = '', timer: ReturnType<typeof setTimeout> | undefined, attempts = 0
    const controller = new AbortController()
    if (!resource.id) return
    const id = resource.id
    const load = async () => {
      try {
        const blob = await loadResourcePreview(id, undefined, controller.signal)
        if (!active || !blob.type.startsWith('image/')) return
        const next = URL.createObjectURL(blob)
        if (objectURL) URL.revokeObjectURL(objectURL)
        objectURL = next
        setURL(next); setError('')
      } catch (reason) {
        if (active && !controller.signal.aborted) setError(reason instanceof Error ? reason.message : '预览暂不可用')
      } finally {
        if (active && connected && resource.preview_status === 'pending' && ++attempts < 20) timer = setTimeout(() => void load(), 5000)
      }
    }
    void load()
    return () => { active = false; controller.abort(); if (timer) clearTimeout(timer); if (objectURL) URL.revokeObjectURL(objectURL) }
  // A regenerated preview keeps the content digest; a freshly fetched resource
  // object must still reload its image after that operation.
  }, [resource, connected])
  return {url, error, connected}
}

function PreviewPlaceholder({resource, error, connected}: {resource: VisualResource; error: string; connected: boolean}) {
  return <span className="px-3 text-center text-xs text-text-muted">{error || (resource.preview_status === 'failed' ? '预览生成失败，可重新生成' : connected ? '正在准备预览…' : '连接 QGIS 后生成预览')}</span>
}

export function ResourceThumbnail({ resource, className = '' }: { resource: VisualResource; className?: string }) {
  const {url, error, connected} = useResourceImage(resource)
  const [longLegend, setLongLegend] = useState(false)
  const name = resource.name || resource.label || '资源'
  const ratio = resource.type === 'layout' ? (resource.orientation === 'portrait' ? 'aspect-[1/1.414]' : 'aspect-[1.414/1]') : 'aspect-[24/13]'
  return <div className={`relative flex ${ratio} items-center justify-center overflow-hidden rounded bg-zinc-100 dark:bg-zinc-800 ${className}`}>
    {url ? <>
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img src={url} alt={`${name}预览`} onLoad={event => setLongLegend(resource.type === 'style' && event.currentTarget.naturalHeight > event.currentTarget.naturalWidth * 1.2)} className={`h-full w-full ${longLegend ? 'object-cover object-top' : 'object-contain'}`} />{longLegend && <span className="absolute inset-x-0 bottom-0 bg-white/95 px-2 py-1 text-center text-xs text-text-muted dark:bg-zinc-900/95">展开查看图例</span>}</> : <PreviewPlaceholder resource={resource} error={error} connected={connected}/>}
  </div>
}

function ResourcePreviewDialog({resource, initialMode, onClose}: {resource: VisualResource; initialMode: 'symbol' | 'layer'; onClose: () => void}) {
  const [mode, setMode] = useState(initialMode)
  const {url, error, connected} = useResourceImage(resource)
  const name = resource.name || resource.label || '资源'
  const style = resource.type === 'style'
  return <Dialog open onClose={onClose} className="relative z-50"><DialogBackdrop className="fixed inset-0 bg-black/50"/><div className="fixed inset-0 flex items-center justify-center p-4"><DialogPanel className="max-h-[95vh] w-full max-w-5xl overflow-auto rounded-xl bg-white p-5 shadow-xl text-text-primary dark:bg-zinc-900">
    <div className="mb-4 flex items-center justify-between gap-3"><DialogTitle className="font-semibold">{name}</DialogTitle><button type="button" className={buttonClass} onClick={onClose}>关闭预览</button></div>
    {style && <><div role="tablist" aria-label="样式预览方式" className="mb-3 flex gap-2">{([{id: 'symbol', name: '符号图例'}, {id: 'layer', name: '用图层预览'}] as const).map(tab => <button type="button" role="tab" key={tab.id} aria-selected={mode === tab.id} className={`${buttonClass} aria-selected:border-blue-500 aria-selected:bg-blue-50 aria-selected:text-blue-700 dark:aria-selected:bg-blue-950 dark:aria-selected:text-blue-300`} onClick={() => setMode(tab.id)}>{tab.name}</button>)}</div><p className="mb-3 text-xs text-text-muted">符号图例用于查看样式特征；完整标注与字段规则可用图层预览。</p></>}
    {mode === 'layer' && resource.id && style ? <StyleLayerPreview resourceId={resource.id} geometry={resource.geometry}/> : url ? <PreviewImage key={url} url={url} alt={`${name}${style ? '符号图例预览' : '完整预览'}`}/> : <div className="flex h-40 items-center justify-center rounded border"><PreviewPlaceholder resource={resource} error={error} connected={connected}/></div>}
    {resource.description && <p className="mt-3 text-sm">{resource.description}</p>}<p className="mt-2 text-xs text-text-muted">{style ? `${geometryLabel(resource.geometry)} · 标注字段：${resource.label_field || '无'}` : [resource.paper, resource.orientation === 'portrait' ? '纵向' : resource.orientation === 'landscape' ? '横向' : resource.orientation, resource.elements?.join('、')].filter(Boolean).join(' · ')}</p>
  </DialogPanel></div></Dialog>
}

export default function ResourceMedia({ resource, expandable = true }: { resource: VisualResource; expandable?: boolean }) {
  const [mode, setMode] = useState<'symbol' | 'layer' | null>(null)
  const name = resource.name || resource.label || '资源'
  return <>
    {expandable ? <div className="space-y-2"><button type="button" aria-label={`放大${name}预览`} className="block w-full text-left" onClick={() => setMode('symbol')}><ResourceThumbnail resource={resource}/></button>{resource.type === 'style' && resource.id && <button type="button" className={buttonClass} onClick={() => setMode('layer')}>用图层预览</button>}</div> : <ResourceThumbnail resource={resource}/>}
    {mode && <ResourcePreviewDialog key={resource.id} resource={resource} initialMode={mode} onClose={() => setMode(null)}/>}
  </>
}
