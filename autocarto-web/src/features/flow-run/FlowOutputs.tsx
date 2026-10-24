'use client'
import { useState } from 'react'
import { invoke } from '@tauri-apps/api/core'
import { isDesktopRuntime } from '@/lib/config-form-helpers'

function Value({ value, onPath }: { value: unknown; onPath: (path: string) => void }) {
  if (Array.isArray(value)) return <ul className="space-y-1">{value.map((item, index) => <li key={index}><Value value={item} onPath={onPath}/></li>)}</ul>
  if (value && typeof value === 'object') return <dl className="space-y-1">{Object.entries(value).map(([key, item]) => <div key={key}><dt className="font-medium">{key}</dt><dd className="ml-2"><Value value={item} onPath={onPath}/></dd></div>)}</dl>
  if (typeof value !== 'string') return <span>{String(value ?? '')}</span>
  if (/^https?:\/\//i.test(value)) return <a href={value} target="_blank" rel="noreferrer" className="break-all text-blue-600 underline">{value}</a>
  const path = /[\\/]/.test(value) || /\.(?:png|jpe?g|webp|tiff?|pdf|csv|tsv|xlsx?|geojson|gpkg|qgs|qgz|json|txt|md|zip)$/i.test(value)
  return path ? <button type="button" className="break-all text-left text-blue-600 underline" onClick={() => onPath(value)}>{value}</button> : <span className="whitespace-pre-wrap break-words">{value}</span>
}
export default function FlowOutputs({ outputs }: { outputs: Record<string, unknown> }) {
  const [feedback, setFeedback] = useState('')
  const open = async (path: string) => {
    try {
      if (isDesktopRuntime()) { await invoke('open_local_path', { path }); setFeedback('') }
      else {
        if (!navigator.clipboard?.writeText) { setFeedback('浏览器无法自动复制，请手工复制路径；桌面应用可点击打开成果。'); return }
        await navigator.clipboard.writeText(path); setFeedback('路径已复制。请在桌面应用中点击打开成果。')
      }
    } catch (error) { setFeedback(error instanceof Error ? error.message : `打开失败：${String(error)}`) }
  }
  return <div className="text-xs"><dl className="space-y-1">{Object.entries(outputs).map(([label, value]) => <div key={label}><dt className="font-medium">{label}</dt><dd className="ml-2"><Value value={value} onPath={path => void open(path)}/></dd></div>)}</dl>{feedback && <p role="status" className="mt-1 text-amber-700">{feedback}</p>}</div>
}
