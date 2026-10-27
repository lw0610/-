'use client'
import { useEffect, useState } from 'react'
import Link from 'next/link'
import { AgentFlowsConfig } from '@/lib/api'
import { flowApi } from '@/features/flows/api'
import { FlowSummary } from '@/features/flows/types'
import { EditorSection } from './shared/AgentFormParts'
export function flowReadiness(flow: FlowSummary) {
  const roles = flow.roles
  if (flow.diagnostics_summary?.errors > 0 || roles?.unresolved)
    return { label: '有错误', tab: 'roles', tone: 'text-red-600' }
  if (roles?.conditional_unresolved)
    return { label: '部分步骤有条件限制', tab: 'roles', tone: 'text-amber-700' }
  const added =
    (roles?.augmented || 0) +
    (roles?.flow_role || 0) +
    (roles?.executor_default || 0)
  return {
    label: added ? `自动补齐 ${added} 项` : '可运行',
    tab: added ? 'roles' : 'steps',
    tone: added ? 'text-blue-600' : 'text-emerald-700',
  }
}
export default function AgentFlowSelection({
  scheme,
  value,
  onChange,
}: {
  scheme: string
  value?: AgentFlowsConfig
  onChange: (next: AgentFlowsConfig | undefined) => void
}) {
  const [flows, setFlows] = useState<FlowSummary[]>([]),
    [loading, setLoading] = useState(true),
    [error, setError] = useState('')
  useEffect(() => {
    let current = true
    setLoading(true)
    setError('')
    void flowApi
      .list(scheme)
      .then((items) => {
        if (current) setFlows(items)
      })
      .catch((error) => {
        if (current)
          setError(error instanceof Error ? error.message : '流程列表加载失败')
      })
      .finally(() => {
        if (current) setLoading(false)
      })
    return () => {
      current = false
    }
  }, [scheme])
  const selected = value ? value.enabled : flows.map((flow) => flow.id),
    missing =
      value?.enabled.filter((id) => !flows.some((flow) => flow.id === id)) || []
  const href = (id: string, tab: string) =>
    `/flows?flow=${encodeURIComponent(id)}&scheme=${encodeURIComponent(scheme)}&tab=${tab}`
  return (
    <EditorSection
      title="启用的流程"
      description="流程按步骤安排角色，工具包决定角色具备哪些能力。未指定清单时全部启用。"
    >
      <div className="mb-2 flex items-center justify-between text-xs">
        <span>{value ? `已选 ${selected.length} 个流程` : '默认全部启用'}</span>
        <button
          type="button"
          onClick={() => onChange(undefined)}
          className="text-blue-600"
        >
          恢复全部启用
        </button>
      </div>
      {loading ? (
        <p className="text-xs text-zinc-500">加载流程中…</p>
      ) : error ? (
        <p role="alert" className="text-xs text-amber-700">
          {error}。当前启用设置已保留。
        </p>
      ) : !flows.length ? (
        <p className="text-xs text-zinc-500">
          暂无流程包，可前往流程中心导入或新建。
        </p>
      ) : (
        <ul className="space-y-2">
          {flows.map((flow) => {
            const status = flowReadiness(flow)
            return (
              <li
                key={flow.id}
                className="flex flex-wrap items-start justify-between gap-3 rounded border border-zinc-200 p-3 text-xs dark:border-zinc-700"
              >
                <label className="flex gap-2">
                  <input
                    type="checkbox"
                    checked={selected.includes(flow.id)}
                    onChange={(event) =>
                      onChange({
                        ...value,
                        enabled: event.target.checked
                          ? [...new Set([...selected, flow.id])]
                          : selected.filter((id) => id !== flow.id),
                      })
                    }
                  />
                  <span>
                    <Link
                      href={href(flow.id, 'steps')}
                      className="font-medium text-blue-600"
                    >
                      {flow.name}
                    </Link>
                    <span className="ml-2 text-zinc-500">
                      {flow.version} ·{' '}
                      {flow.source === 'builtin' ? '内置' : '用户'}
                    </span>
                    <span className="mt-1 block text-zinc-500">
                      {flow.description}
                    </span>
                  </span>
                </label>
                <div className="flex flex-col gap-1">
                  <Link
                    className={status.tone}
                    href={href(flow.id, status.tab)}
                  >
                    {status.label}
                  </Link>
                  {Boolean(flow.data?.acquirable) && (
                    <Link
                      className="text-blue-600"
                      href={href(flow.id, 'requirements')}
                    >
                      可自动获取数据
                      {flow.data!.available === 0 ? '（来源待配置）' : ''}
                    </Link>
                  )}
                  {Boolean(flow.data?.items) &&
                    flow.data!.items > flow.data!.acquirable && (
                      <Link
                        className="text-zinc-500"
                        href={href(flow.id, 'requirements')}
                      >
                        {flow.data!.items - flow.data!.acquirable} 项由用户提供
                      </Link>
                    )}
                </div>
              </li>
            )
          })}
        </ul>
      )}
      {missing.length > 0 && !loading && (
        <p role="alert" className="mt-2 text-xs text-amber-700">
          以下已启用流程当前未找到，设置将保留：{missing.join('、')}
        </p>
      )}
    </EditorSection>
  )
}
