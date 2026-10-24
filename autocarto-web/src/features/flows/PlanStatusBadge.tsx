'use client'
import type { Diagnostic, FlowPlan } from './types'

export default function PlanStatusBadge({ plan, diagnostics = plan.diagnostics || [] }: {
  plan: FlowPlan
  diagnostics?: Diagnostic[]
}) {
  const hasErrors = diagnostics.some(item => item.level === 'error')
  return <span className="inline-flex items-center rounded bg-zinc-100 px-2 py-1 text-xs dark:bg-zinc-800" aria-live="polite"
    title={hasErrors ? '流程存在需要修复的问题，请查看对应提示。' : '已根据保存的流程内容生成步骤预览。'}>
    {hasErrors ? '需要修复' : '已解析'}
  </span>
}
