'use client'
import { useEffect, useState } from 'react'
import { flowApi } from './api'
import { buttonClass, InputPreview } from './PreviewPanels'
import PathDialog from './PathDialog'
import type { FlowInput } from './types'

export default function InputMemoryPanel({ id, inputs }: { id: string; inputs: FlowInput[] }) {
  const [values, setValues] = useState<Record<string, string>>({})
  const [editing, setEditing] = useState<FlowInput | null>(null)
  const [error, setError] = useState(''), [busy, setBusy] = useState(false)
  useEffect(() => {
    let current = true
    setValues({}); setError(''); setEditing(null)
    void flowApi.inputMemory(id).then(result => { if (current) setValues(result) }).catch(() => { if (current) setError('无法读取本机上次选择。') })
    return () => { current = false }
  }, [id])
  const remembered = inputs.filter(input => input.remember && ['file', 'folder'].includes(input.type))
  return <div className="space-y-3">
    <InputPreview inputs={inputs}/>
    {!!remembered.length && <section aria-label="本机记住的输入" className="space-y-3"><h3 className="text-sm font-medium">本机上次选择</h3><p className="text-xs text-zinc-500">仅用于下次预填，不随流程复制或导出。</p>
      {remembered.map(input => <div key={input.key} className="rounded border p-3 text-xs"><p>{input.label}：<span className="break-all">{values[input.key] || '尚未记住'}</span></p><div className="mt-2 flex gap-2">
        <button className={buttonClass} disabled={busy} onClick={() => setEditing(input)}>选择{input.label}{input.type === 'folder' ? '文件夹' : '文件'}</button>
        <button className={buttonClass} disabled={busy || !values[input.key]} onClick={async () => {
          setBusy(true); setError('')
          try { await flowApi.clearInputMemory(id, input.key); setValues(current => { const result = { ...current }; delete result[input.key]; return result }) }
          catch (reason) { setError(reason instanceof Error ? reason.message : '清除失败') }
          finally { setBusy(false) }
        }}>清除{input.label}记忆</button>
      </div></div>)}
    </section>}
    {error && <p role="alert" className="text-xs text-red-700">{error}</p>}
    {editing && <PathDialog title={`设置「${editing.label}」的本机上次选择`} fields={[{ key: 'value', label: '路径', initial: values[editing.key], picker: editing.type === 'folder' ? 'directory' : 'file' }]} onClose={() => setEditing(null)} onSubmit={async data => {
      await flowApi.setInputMemory(id, editing.key, data.value)
      setValues(await flowApi.inputMemory(id))
    }}/>} 
  </div>
}
