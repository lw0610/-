'use client'
import { useMemo, useState } from 'react'
import type {
  Diagnostic,
  FlowInput,
  FlowPlan,
  ResolvedResource,
  ToolCapability,
  ResourceTypeDefinition,
} from './types'
import { resourceTypeLabel } from './editor'
import {roleSourceLabel} from './roleLabels'
export const buttonClass =
  'rounded border border-zinc-300 bg-white px-3 py-1.5 text-xs disabled:opacity-40 hover:bg-zinc-100 dark:border-zinc-700 dark:bg-zinc-900 dark:hover:bg-zinc-800'
export const fieldClass =
  'w-full rounded border border-zinc-300 bg-transparent px-2 py-1.5 text-sm dark:border-zinc-700'
const failureLabels: Record<string, string> = {
  retry_then_ask: '重试后询问',
  ask: '询问用户',
  stop: '停止流程',
  skip: '跳过本步',
}
export function StepPreview({
  plan,
  capabilities,
}: {
  plan: FlowPlan | null
  capabilities: ToolCapability[]
}) {
  const names = new Map(
    capabilities.map((item) => [item.name, item.display_name || item.name]),
  )
  const packages = new Map(capabilities.filter(item=>item.package_id).map(item=>[item.package_id!,item.package_display_name||item.package_id!]))
  if (!plan)
    return (
      <p className="text-sm text-zinc-500">
        保存流程后查看步骤和完成标准。
      </p>
    )
  return (
    <ol className="space-y-3">
      {plan.steps.map((step) => (
        <li
          key={step.id}
          className="rounded border border-zinc-200 p-3 dark:border-zinc-800"
        >
          <h3 className="font-medium">
            {step.kind === 'acquire' || step.acquisition_requirement ? '数据准备 · ' : `${step.index}. `}{step.title}
          </h3>
          <p className="mt-1 text-xs text-zinc-500">
            负责：
            {step.role_label ||
              step.role ||
              (step.kind === 'confirm' ? '用户' : '系统自动')}
          </p>
          {(step.kind === 'agent' || step.role_source || step.unresolved || step.granted_packages?.length) && <p className={`mt-1 text-xs ${step.unresolved?'text-amber-700':'text-blue-700'}`}>
            {roleSourceLabel({source:step.role_source||'scheme_name',granted:step.granted_packages,reason:step.unresolved},packages)}
            {step.unresolved && step.condition ? ' · 仅在条件满足时需要' : ''}
          </p>}
          {step.acquisition_requirement && <p className="mt-1 text-xs text-blue-700">系统步骤：获取数据（仅在自动获取或补齐时执行）</p>}
          {step.condition && (
            <p className="mt-1 text-xs">条件：{step.condition.text}</p>
          )}
          <details className="mt-2 text-sm">
            <summary className="cursor-pointer">
              {step.instructions.slice(0, 70)}
              {step.instructions.length > 70 ? '…' : ''}
            </summary>
            <p className="mt-2 whitespace-pre-wrap">{step.instructions}</p>
          </details>
          {!!step.suggested_tools?.length && (
            <p className="mt-2 text-xs">
              建议能力：
              {step.suggested_tools
                .map((name) => names.get(name) || name)
                .join('、')}
            </p>
          )}
          {!!step.refs?.resources?.length && (
            <p className="mt-2 text-xs">
              资源：{step.refs.resources.join('、')}
            </p>
          )}
          <ul className="mt-2 text-xs">
            {step.expected_outputs?.map((item) => (
              <li key={item.label}>
                产出：{item.label} · {item.type}
                {item.format ? ` / ${item.format}` : ''}
                {item.required ? ' · 必需' : ''}
              </li>
            ))}
          </ul>
          <p className="mt-2 text-xs text-zinc-500">
            失败时：{failureLabels[step.on_failure] || step.on_failure}
          </p>
        </li>
      ))}
    </ol>
  )
}
export function InputPreview({ inputs }: { inputs: FlowInput[] }) {
  if (!inputs.length)
    return <p className="text-sm text-zinc-500">没有声明输入，或尚未解析。</p>
  const types: Record<string, string> = {
    text: '文本',
    conversation_text: '对话原文',
    folder: '文件夹',
    file: '文件',
    boolean: '是/否',
    choice: '选项',
    number: '数值',
  }
  return (
    <div className="space-y-3">
      {inputs.map((input) => (
        <div
          key={input.key}
          className="rounded border border-zinc-200 p-3 dark:border-zinc-800"
        >
          <p>
            {input.label}
            {input.required && <span className="ml-1 text-red-600">必填</span>}
          </p>
          <p className="mt-1 text-xs text-zinc-500">
            {types[input.type] || input.type} · 默认值：
            {input.default == null ? '未设置' : String(input.default)}
          </p>
          {input.options?.length ? (
            <p className="text-xs">可选：{input.options.join('、')}</p>
          ) : null}
          <p className="mt-1 text-sm">{input.description}</p>
        </div>
      ))}
    </div>
  )
}
const resourceActionLabels: Record<string, string> = {
  choose_input_folder: '选择输入文件夹',
  convert_to_input: '转换为文件夹输入',
  adopt_into_flow: '移入流程',
  choose_resource: '选择正确的资源',
  update_lock: '更新资源锁定',
  upgrade_resource: '升级资源清单',
  rewrite_type: '修正资源类型',
  install_resource: '安装到模板与样式库',
  use_library: '改用本机模板与样式',
}
export function ResourcePreview({
  resource,
  onChooseInput,
  onFix,
  busy,
  types = [],
}: {
  resource: ResolvedResource
  onChooseInput?: () => void
  onFix?: (item: Diagnostic) => void
  busy?: boolean
  types?: ResourceTypeDefinition[]
}) {
  return (
    <section className="rounded border border-zinc-200 p-3 dark:border-zinc-800">
      <h3 className="text-sm font-medium">
        {resource.label} · {resourceTypeLabel(types, resource.type)}
      </h3>
      <p className={`mt-1 text-xs ${resource.problems?.some(item => item.level === 'error') ? 'text-red-600' : resource.problems?.some(item => item.level === 'warning') ? 'text-amber-700' : 'text-emerald-700'}`}>
        状态：{resource.problems?.some(item => item.level === 'error') ? '需要修复' : resource.problems?.some(item => item.level === 'warning') ? '有警告' : '可用'}
      </p>
      <p className="mt-1 text-xs text-zinc-500">
        {resource.owner === 'library_builtin' || resource.origin?.includes('builtin') ? '内置 · 只读' : resource.owner === 'library_user' || resource.origin?.includes('user') ? '用户' : resource.owner === 'bundled' ? '随包依赖' : resource.owner === 'external' ? '外部资源' : '流程自带'}
        {resource.legacy && ' · 旧式资源'}
      </p>
      {resource.id && <p className="mt-1 break-all text-xs text-zinc-500">编号：{resource.id}</p>}
      {resource.digest && <p className="mt-1 text-xs text-zinc-500" title={resource.digest}>摘要：{resource.digest.replace(/^sha256:/, '').slice(0, 8)}</p>}
      {resource.overrides && <p className="mt-1 text-xs text-blue-600">覆盖内置资源：{resource.overrides}</p>}
      {resource.overridden_by && <p className="mt-1 text-xs text-amber-700">已被用户资源覆盖：{resource.overridden_by}</p>}
      {resource.description && <p className="mt-2 text-xs">{resource.description}</p>}
      {resource.source && <p className="mt-1 break-all text-xs text-zinc-500">
        {[resource.source.author && `作者：${resource.source.author}`, resource.source.license && `许可：${resource.source.license}`, resource.source.upstream && `来源：${resource.source.upstream}`].filter(Boolean).join(' · ')}
      </p>}
      <p className="mt-1 break-all text-xs text-zinc-500">
        {resource.entry || resource.root || resource.location}
      </p>
      {resource.base_dir && (
        <p className="mt-1 break-all text-xs">基准目录：{resource.base_dir}</p>
      )}
      {resource.base_dir_from_input && <p className="mt-1 text-xs">文件位于输入「{resource.base_dir_from_input}」，运行前选择文件夹。</p>}
      {resource.type === 'data_list' && resource.base_dir_from_input && onChooseInput && (
        <button
          disabled={busy}
          className={`${buttonClass} mt-2`}
          onClick={onChooseInput}
        >
          选择输入文件夹
        </button>
      )}
      <ul className="mt-2 space-y-2 text-xs">
        {resource.entries?.map((entry, i) => (
          <li key={`${entry.name}-${i}`} className="break-all">
            <strong>{entry.name}</strong> · {entry.exists_unknown || entry.exists == null ? '运行时检查' : entry.exists ? '存在' : '缺失'}
            <p className="text-zinc-500">{entry.path}</p>
            {entry.extra?.label_field && (
              <p>标注字段：{entry.extra.label_field}</p>
            )}
          </li>
        ))}
      </ul>
      {resource.problems?.map((item, i) => (
        <div key={i} className="mt-2 text-xs text-amber-700">
          <p>{item.message}</p>
          {onFix && item.fix && [...new Set(item.fix.actions?.length ? item.fix.actions : [item.fix.action])]
            .filter(action => resourceActionLabels[action] && !(action === 'choose_input_folder' && resource.base_dir_from_input && onChooseInput))
            .map(action => (
              <button
                key={action}
                disabled={busy}
                className={`${buttonClass} mt-2 mr-2`}
                onClick={() => onFix({ ...item, fix: { ...item.fix!, action } })}
              >
                {resourceActionLabels[action]}
              </button>
            ))}
        </div>
      ))}
    </section>
  )
}
export function Handbook({
  capabilities,
  initialQuery = '',
}: {
  capabilities: ToolCapability[]
  initialQuery?: string
}) {
  const [query, setQuery] = useState(initialQuery)
  const items = useMemo(
    () =>
      capabilities.filter((item) =>
        `${item.display_name} ${item.name} ${item.description} ${item.keywords?.join(' ')}`
          .toLowerCase()
          .includes(query.toLowerCase()),
      ),
    [capabilities, query],
  )
  const groups = new Map<string, ToolCapability[]>()
  for (const item of items) {
    const label = item.package_display_name || '其他能力'
    groups.set(label, [...(groups.get(label) || []), item])
  }
  return (
    <section>
      <label className="text-sm">
        搜索能力
        <input
          className={`${fieldClass} mt-1`}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="如：排版、数据加载、标注"
        />
      </label>
      {[...groups.entries()]
        .sort(([a], [b]) => a.localeCompare(b, 'zh-CN'))
        .map(([label, tools]) => (
          <section key={label} className="mt-4">
            <h3 className="text-sm font-semibold">
              {label}（{tools.length}）
            </h3>
            <ul className="mt-2 space-y-2">
              {tools.map((item) => (
                <li
                  key={item.name}
                  className="rounded border border-zinc-200 p-3 text-sm dark:border-zinc-800"
                >
                  <p className="font-medium">
                    {item.display_name || item.name}
                  </p>
                  <p className="mt-1 text-xs text-zinc-500">
                    {item.description}
                  </p>
                  <p className="mt-1 text-xs">
                    写流程时可以描述：{item.display_name || item.description}
                  </p>
                  <p className="mt-1 text-xs text-zinc-400">
                    {item.keywords?.join('、')}
                  </p>
                </li>
              ))}
            </ul>
          </section>
        ))}
    </section>
  )
}
