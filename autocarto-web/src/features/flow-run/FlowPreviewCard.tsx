'use client'
import { useEffect, useState } from 'react'
import { StepPreview } from '@/features/flows/PreviewPanels'
import { flowApi } from '@/features/flows/api'
import type { ToolCapability } from '@/features/flows/types'
import type { FlowPreview } from './types'
import { record } from './model'

export function parseFlowPreview(value: unknown): FlowPreview | null {
  if (typeof value === 'string') {
    try { return parseFlowPreview(JSON.parse(value)) } catch { return null }
  }
  const raw = record(value), plan = record(raw.plan)
  if (Array.isArray(plan.steps) && Array.isArray(plan.inputs) && typeof plan.flow_id === 'string') return raw as unknown as FlowPreview
  for (const key of ['data', 'result']) if (raw[key]) {
    const nested = parseFlowPreview(raw[key])
    if (nested) return nested
  }
  return null
}
export default function FlowPreviewCard({ preview }: { preview: FlowPreview }) {
  const [capabilities, setCapabilities] = useState<ToolCapability[]>([])
  useEffect(() => { let current = true; void flowApi.capabilities().then(value => { if (current) setCapabilities(value) }).catch(() => undefined); return () => { current = false } }, [])
  return <section aria-label={`流程预览 ${preview.name || preview.plan.name}`} className="mb-3 rounded border border-amber-300 bg-surface-raised p-3 dark:border-amber-800"><h2 className="mb-2 text-sm font-semibold">流程预览：{preview.name || preview.plan.name}</h2><p className="mb-2 text-xs text-amber-700">当前只做预览。切换到 Build 后可执行此流程。</p>
    {preview.diagnostics?.map((item, index) => <p key={index} className={item.level === 'error' ? 'mb-1 text-xs text-red-700' : 'mb-1 text-xs text-amber-700'}>{item.level === 'error' ? '错误' : item.level === 'warning' ? '警告' : '提示'}：{item.message}</p>)}
    {preview.input_check?.missing?.length ? <p className="mb-2 text-xs text-amber-700">需要补充：{preview.input_check.missing.map(String).join('、')}</p> : preview.input_check?.valid && <p className="mb-2 text-xs text-green-700">输入已齐全。</p>}
    <StepPreview plan={preview.plan} capabilities={capabilities}/>
  </section>
}
