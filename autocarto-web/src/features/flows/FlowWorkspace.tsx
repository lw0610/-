'use client'
import { useCallback, useEffect, useRef, useState } from 'react'
import Link from 'next/link'
import api, { type AgentConfigSummary } from '@/lib/api'
import { useConnectionStore } from '@/stores/connectionStore'
import {
  confirmDiscardChanges,
  useUnsavedChangesGuard,
} from '@/hooks/useUnsavedChangesGuard'
import { flowApi, resourceApi } from './api'
import type {
  Diagnostic,
  FlowDetail,
  FlowSummary,
  ToolCapability,
  ResourceTypeDefinition,
  ResolvedResource,
  ResourceCandidate,
} from './types'
import {
  flowTemplate,
  templateSections,
  insertResourceReference,
  rewriteResourceType,
} from './editor'
import {
  buttonClass,
  fieldClass,
  Handbook,
  ResourcePreview,
  StepPreview,
} from './PreviewPanels'
import PathDialog, { type DialogField } from './PathDialog'
import InputMemoryPanel from './InputMemoryPanel'
import ResourceLibrary from './ResourceLibrary'
import PlanStatusBadge from './PlanStatusBadge'

type Tab = 'steps' | 'resources' | 'inputs' | 'handbook'
type Dialog = 'new' | 'import' | 'copy' | 'export' | 'asset'
export default function FlowWorkspace() {
  const ready = useConnectionStore((state) => state.backendReady)
  const [page, setPage] = useState<'flows' | 'library'>('flows'),
    [scheme, setScheme] = useState('default')
  const [locationReady, setLocationReady] = useState(false)
  const linkedFlow = useRef('')
  const [schemes, setSchemes] = useState<AgentConfigSummary[]>([]),
    [items, setItems] = useState<FlowSummary[]>([])
  const [detail, setDetail] = useState<FlowDetail | null>(null),
    [markdown, setMarkdown] = useState('')
  const [capabilities, setCapabilities] = useState<ToolCapability[]>([]),
    [tab, setTab] = useState<Tab>('steps')
  const [resourceTypes, setResourceTypes] = useState<ResourceTypeDefinition[]>([])
  const [resourceChoice, setResourceChoice] = useState<{ resource: ResolvedResource; candidates: ResourceCandidate[] } | null>(null)
  const [dialog, setDialog] = useState<Dialog | null>(null)
  const [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [notice, setNotice] = useState(''),
    [search, setSearch] = useState('')
  const listSequence = useRef(0),
    schemeInitialized = useRef(false),
    currentScheme = useRef(scheme)
  currentScheme.current = scheme
  const requestSequence = useRef(0),
    editorRef = useRef<HTMLTextAreaElement>(null)
  const dirty = Boolean(detail && markdown !== detail.flow_md)
  const readOnly = detail?.source === 'builtin'
  const canRun = Boolean(detail?.plan && !detail.diagnostics.some(item => item.level === 'error') && !dirty && !busy)
  useUnsavedChangesGuard(dirty)
  useEffect(() => {
    const query = new URLSearchParams(window.location.search)
    const linkedScheme = query.get('scheme')?.trim()
    linkedFlow.current = query.get('flow')?.trim() || ''
    if (linkedScheme) setScheme(linkedScheme)
    // A link carries the selected scheme rather than the running scheme.
    if (linkedScheme || linkedFlow.current) schemeInitialized.current = true
    setLocationReady(true)
  }, [])
  const refresh = useCallback(async () => {
    const ticket = ++listSequence.current
    const result = await flowApi.list(scheme)
    if (ticket === listSequence.current) setItems(result)
  }, [scheme])
  useEffect(() => {
    if (!ready || !locationReady) return
    const listed = refresh()
    if (linkedFlow.current) {
      const id = linkedFlow.current
      linkedFlow.current = ''
      const sequence = ++requestSequence.current
      setBusy(true)
      void listed.then(async () => {
        if (sequence !== requestSequence.current || currentScheme.current !== scheme) return
        const result = await flowApi.get(id, scheme)
        if (sequence !== requestSequence.current || currentScheme.current !== scheme) return
        setDetail(result)
        setMarkdown(result.flow_md)
        setError('')
      }).catch((err) => {
        if (sequence === requestSequence.current) setError(String(err))
      }).finally(() => {
        if (sequence === requestSequence.current) setBusy(false)
      })
    } else {
      void listed.catch((err) => setError(String(err)))
    }
    void flowApi
      .capabilities()
      .then(setCapabilities)
      .catch((err) => setError(String(err)))
    void resourceApi.types().then(setResourceTypes).catch((err) => setError(String(err)))
    void api
      .getAgentConfigs()
      .then((values) => {
        setSchemes(values)
        if (!schemeInitialized.current) {
          schemeInitialized.current = true
          const active = values.find((item) => item.is_active)
          if (active) setScheme(active.config_name)
        }
      })
      .catch((err) => setError(String(err)))
  }, [ready, locationReady, refresh, scheme])
  const selectDetail = (value: FlowDetail) => {
    setDetail(value)
    setMarkdown(value.flow_md)
    setError('')
    setNotice('')
  }
  const perform = async (operation: () => Promise<void>) => {
    setBusy(true)
    setError('')
    setNotice('')
    try {
      await operation()
    } catch (err) {
      setError(err instanceof Error ? err.message : '操作失败')
    } finally {
      setBusy(false)
    }
  }
  const open = async (id: string) => {
    if (dirty && !confirmDiscardChanges()) return
    const sequence = ++requestSequence.current
    await perform(async () => {
      const result = await flowApi.get(id, scheme)
      if (sequence === requestSequence.current && currentScheme.current === scheme) selectDetail(result)
    })
  }
  const refreshAfterWrite = async () => {
    try {
      await refresh()
    } catch (err) {
      // The mutation succeeded. A failed list refresh must not leave the
      // creation dialog open and invite the user to submit the same ID again.
      setError(`操作已完成，但流程列表刷新失败：${err instanceof Error ? err.message : '请稍后重新打开流程中心。'}`)
    }
  }
  const save = async () => {
    if (!detail || readOnly) return
    const saved = await flowApi.save(detail.meta.id, markdown, scheme)
    selectDetail(saved)
    setTab('steps')
    await refreshAfterWrite()
    setNotice(saved.plan ? '已保存，步骤预览已更新，旧版本已备份。' : '已保存，旧版本已备份。请查看流程中的问题提示。')
  }
  const insert = (text: string) => {
    const editor = editorRef.current
    const start = editor?.selectionStart ?? markdown.length,
      end = editor?.selectionEnd ?? markdown.length
    setMarkdown(markdown.slice(0, start) + text + markdown.slice(end))
    requestAnimationFrame(() => {
      editor?.focus()
      editor?.setSelectionRange(start + text.length, start + text.length)
    })
  }
  const resourceForDiagnostic = (item: Diagnostic) => {
    const key = String(item.fix?.resource_key || item.fix?.resource || item.resource || '')
    return detail?.plan?.resources.find(resource => [resource.key, resource.binding_key, resource.base_dir_binding, resource.id, resource.label].some(value => Boolean(value) && value === key))
  }
  const editableResource = () => {
    if (readOnly) {
      setDialog('copy')
      setNotice('请先复制内置流程，再修改资源声明或锁定。')
      return false
    }
    if (dirty) {
      setNotice('请先保存流程内容，再修改资源锁定。')
      return false
    }
    return true
  }
  const repairResource = async (action: string, resource: ResolvedResource, resourceID?: string) => {
    if (!detail) return
    selectDetail(await flowApi.repairResource(detail.meta.id, action, resource.key, resourceID, scheme))
    await refreshAfterWrite()
    setNotice('已更新资源。')
  }
  const fixResource = (item: Diagnostic) => {
    const action = item.fix?.action
    if (action === 'choose_input_folder') {
      setTab('inputs')
      setNotice('可在输入页选择文件夹，或试运行时选择本次数据。')
      return
    }
    void perform(async () => {
      if (!detail) return
      if (['convert_to_input', 'adopt_into_flow', 'choose_resource', 'update_lock', 'upgrade_resource', 'rewrite_type', 'install_resource', 'use_library'].includes(action || '')) {
        const resource = resourceForDiagnostic(item)
        if (!resource) throw new Error('未找到对应资源，请重新打开流程后重试')
        if (action === 'adopt_into_flow') {
          if (!editableResource()) return
          selectDetail(await flowApi.adoptResource(detail.meta.id, resource.id || '', resource.label, scheme))
          await refreshAfterWrite()
          return
        }
        if (action === 'rewrite_type') {
          if (readOnly) { editableResource(); return }
          setMarkdown(rewriteResourceType(markdown, resource, item.fix?.actual_type || resource.type, resourceTypes))
          setNotice('已修正草稿中的资源类型，请保存以更新步骤预览。')
          return
        }
        if (action === 'upgrade_resource' && resource.owner === 'library_user' && resource.id) {
          if (dirty) { setNotice('请先保存流程内容，再升级资源。'); return }
          await resourceApi.upgrade(resource.id)
          selectDetail(await flowApi.get(detail.meta.id, scheme))
          await refreshAfterWrite()
          setNotice('已升级资源清单。')
          return
        }
        if (!editableResource()) return
        if (action === 'choose_resource') {
          const candidates = item.fix?.candidates || (await resourceApi.list()).filter(candidate =>
            candidate.type === resource.type && candidate.name === (resource.name || resource.label),
          ).flatMap(candidate => candidate.id ? [{ ...candidate, id: candidate.id }] : [])
          setResourceChoice({ resource, candidates })
          return
        }
        await repairResource(action!, resource)
        return
      }
    })
  }
  const dialogFields = (): DialogField[] => {
    switch (dialog) {
      case 'new':
        return [
          { key: 'id', label: '流程编号（小写字母、数字、连字符）' },
          { key: 'name', label: '流程名称', initial: '新流程' },
        ]
      case 'import':
        return [
          { key: 'path', label: '文件夹、ZIP 或 FLOW.md 路径', picker: 'both' },
          { key: 'id', label: '另存为新编号（可选）', optional: true },
        ]
      case 'copy':
        return [
          { key: 'id', label: '新流程编号' },
          {
            key: 'name',
            label: '新流程名称',
            initial: `${detail?.meta.name || ''}（副本）`,
          },
        ]
      case 'export':
        return [{ key: 'path', label: '导出 ZIP 的完整路径' }]
      case 'asset':
        return [
          { key: 'path', label: '资源文件或文件夹路径', picker: 'both' },
          { key: 'directory', label: 'resources 下的子目录', initial: 'data' },
        ]
      default:
        return []
    }
  }
  return (
    <div className="flex min-h-0 flex-1 flex-col text-zinc-900 dark:text-zinc-100">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b border-zinc-200 bg-white px-5 py-4 dark:border-zinc-800 dark:bg-zinc-900">
        <div>
          <h1 className="text-lg font-semibold">流程中心</h1>
          <p className="mt-1 text-xs text-zinc-500">
            用自然语言描述做法，管理流程与模板与样式。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <label className="text-xs">
            当前方案
            <select
              aria-label="当前方案"
              disabled={busy}
              className={`${fieldClass} ml-2 w-auto`}
              value={scheme}
              onChange={(event) => {
                if (!dirty || confirmDiscardChanges()) {
                  requestSequence.current++
                  linkedFlow.current = ''
                  setScheme(event.target.value)
                  setDetail(null)
                  setMarkdown('')
                }
              }}
            >
              {!schemes.some((item) => item.config_name === 'default') && (
                <option value="default">default</option>
              )}
              {schemes.map((item) => (
                <option key={item.config_name} value={item.config_name}>
                  {item.config_name}
                  {item.is_active ? '（运行中）' : ''}
                </option>
              ))}
            </select>
          </label>
          <Link href="/settings/agents" className={buttonClass}>
            配置方案
          </Link>
        </div>
      </header>
      <nav
        aria-label="流程中心页签"
        className="flex gap-2 border-b border-zinc-200 px-5 py-2 dark:border-zinc-800"
      >
        {(
          [
            ['flows', '流程包'],
            ['library', '模板与样式'],
          ] as const
        ).map(([key, label]) => (
          <button
            key={key}
            aria-pressed={page === key}
            className={`${buttonClass} ${page === key ? 'font-semibold text-blue-600' : ''}`}
            onClick={() => setPage(key)}
          >
            {label}
          </button>
        ))}
      </nav>
      {!ready && (
        <p className="p-5 text-sm text-amber-700">正在等待后端连接…</p>
      )}
      {error && (
        <p role="alert" className="px-5 py-2 text-sm text-red-600">
          {error}
        </p>
      )}
      {notice && (
        <p role="status" className="px-5 py-2 text-sm text-emerald-700">
          {notice}
        </p>
      )}
      <div className="min-h-0 flex-1 overflow-y-auto p-5">
        {page === 'library' ? (
          <ResourceLibrary
            onSelect={
              detail && !readOnly
                ? (resource, types) => {
                    setMarkdown(insertResourceReference(markdown, resource, types))
                    setPage('flows')
                    setTab('resources')
                  }
                : undefined
            }
          />
        ) : (
          <div className="flex min-h-full flex-col gap-4 xl:flex-row">
            <aside className="w-full shrink-0 xl:w-64">
              <div className="flex gap-2">
                <button
                  className={buttonClass}
                  disabled={busy || !ready}
                  onClick={() => {
                    if (!dirty || confirmDiscardChanges()) setDialog('new')
                  }}
                >
                  新建流程
                </button>
                <button
                  className={buttonClass}
                  disabled={busy || !ready}
                  onClick={() => setDialog('import')}
                >
                  导入
                </button>
              </div>
              <input
                aria-label="搜索流程"
                className={`${fieldClass} mt-3`}
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder="搜索流程"
              />
              <ul className="mt-3 space-y-2">
                {items
                  .filter((item) =>
                    `${item.name} ${item.description}`.includes(search),
                  )
                  .map((item) => (
                    <li key={item.id}>
                      <button
                        disabled={busy}
                        className={`w-full rounded border p-3 text-left ${detail?.meta.id === item.id ? 'border-blue-500 bg-blue-50 dark:bg-blue-950/30' : 'border-zinc-200 dark:border-zinc-800'}`}
                        onClick={() => void open(item.id)}
                      >
                        <p className="text-sm font-medium">{item.name}</p>
                        <p className="mt-1 text-xs text-zinc-500">
                          {item.description}
                        </p>
                        <p className="mt-2 text-xs">
                          {item.source === 'builtin' ? '内置' : '用户'} ·{' '}
                          {item.version} · {item.enabled ? '已启用' : '未启用'}
                        </p>
                        {(item.diagnostics_summary.errors > 0 || item.updated_at) && (
                          <p className="mt-1 text-xs text-zinc-500">
                            {[
                              item.diagnostics_summary.errors > 0
                                ? `有 ${item.diagnostics_summary.errors} 个错误`
                                : '',
                              item.updated_at
                                ? new Date(item.updated_at).toLocaleDateString()
                                : '',
                            ].filter(Boolean).join(' · ')}
                          </p>
                        )}
                      </button>
                    </li>
                  ))}
              </ul>
              {!items.length && ready && (
                <p className="mt-5 text-sm text-zinc-500">
                  暂无流程，可导入流程包或从模板新建。
                </p>
              )}
            </aside>
            {detail ? (
              <section className="min-w-0 flex-1">
                <div className="mb-3 flex flex-wrap items-center gap-2">
                  <h2 className="mr-auto font-semibold">
                    {detail.meta.name}
                    {readOnly ? ' · 内置只读' : dirty ? ' · 未保存' : ''}
                  </h2>
                  {detail.plan && <PlanStatusBadge plan={detail.plan} diagnostics={detail.diagnostics} />}
                  <button
                    disabled={busy}
                    className={buttonClass}
                    onClick={() => {
                      if (!dirty || confirmDiscardChanges()) setDialog('copy')
                    }}
                  >
                    复制为我的流程
                  </button>
                  <button
                    disabled={busy}
                    className={buttonClass}
                    onClick={() => setDialog('export')}
                  >
                    导出
                  </button>
                  <button
                    disabled={busy || readOnly}
                    className={buttonClass}
                    onClick={() => {
                      if (
                        window.confirm(
                          `删除流程「${detail.meta.name}」及其资源和历史版本？`,
                        )
                      )
                        void perform(async () => {
                          await flowApi.remove(detail.meta.id)
                          setDetail(null)
                          setMarkdown('')
                          await refresh()
                        })
                    }}
                  >
                    删除流程
                  </button>
                </div>
                {detail.diagnostics.some(item => item.level === 'error') && (
                  <div role="alert" className="mb-3 rounded border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30">
                    <p>已保存的流程存在以下问题，解决后才能试运行：</p>
                    <ul className="mt-1 list-disc space-y-1 pl-5">
                      {detail.diagnostics.filter(item => item.level === 'error').map((item, index) => (
                        <li key={`${item.code}-${index}`}>
                          {item.line ? `第 ${item.line} 行：` : ''}{item.message}
                        </li>
                      ))}
                    </ul>
                    <p className="mt-2 text-xs">请修改流程内容或方案配置；资源相关问题可在“资源”页处理。</p>
                  </div>
                )}
                <div className="grid min-h-[520px] gap-4 lg:grid-cols-2">
                  <div className="flex min-w-0 flex-col">
                    <label className="mb-2 text-xs">
                      插入模板段落
                      <select
                        aria-label="插入模板段落"
                        disabled={readOnly || busy}
                        className={`${fieldClass} mt-1`}
                        value=""
                        onChange={(event) => {
                          const key = event.target
                            .value as keyof typeof templateSections
                          if (templateSections[key])
                            insert(templateSections[key])
                        }}
                      >
                        <option value="">选择插入位置后添加…</option>
                        <option value="step">插入步骤</option>
                        <option value="resource">插入资源引用</option>
                        <option value="condition">插入条件</option>
                      </select>
                    </label>
                    <textarea
                      ref={editorRef}
                      aria-label="流程内容"
                      spellCheck={false}
                      readOnly={readOnly || busy}
                      className="min-h-[400px] flex-1 resize-y rounded border border-zinc-300 bg-white p-3 font-mono text-xs leading-6 dark:border-zinc-700 dark:bg-zinc-950"
                      value={markdown}
                      onChange={(event) => setMarkdown(event.target.value)}
                    />
                    <p className="mt-2 text-xs text-zinc-500">
                      内置流程需要复制后修改。保存时保留历史版本。
                    </p>
                  </div>
                  <div className="min-w-0 rounded border border-zinc-200 dark:border-zinc-800">
                    <nav
                      className="flex flex-wrap gap-1 border-b border-zinc-200 p-2 dark:border-zinc-800"
                      aria-label="流程详情页签"
                    >
                      {(
                        [
                          ['steps', '步骤预览'],
                          ['resources', '资源'],
                          ['inputs', '输入'],
                          ['handbook', '能力手册'],
                        ] as const
                      ).map(([key, label]) => (
                        <button
                          key={key}
                          aria-pressed={tab === key}
                          className={`${buttonClass} ${tab === key ? 'font-semibold text-blue-600' : ''}`}
                          onClick={() => setTab(key)}
                        >
                          {label}
                        </button>
                      ))}
                    </nav>
                    <div className="max-h-[650px] overflow-y-auto p-3">
                      {tab === 'steps' && (
                        <StepPreview
                          plan={detail.plan}
                          capabilities={capabilities}
                        />
                      )}
                      {tab === 'inputs' && (
                        <InputMemoryPanel id={detail.meta.id} inputs={detail.plan?.inputs || []} />
                      )}
                      {tab === 'handbook' && (
                        <Handbook capabilities={capabilities} />
                      )}
                      {tab === 'resources' && (
                        <div className="space-y-3">
                          <button
                            disabled={busy || readOnly || dirty}
                            className={buttonClass}
                            onClick={() => setDialog('asset')}
                          >
                            上传 / 替换资源
                          </button>
                          <button
                            className={`${buttonClass} ml-2`}
                            onClick={() => setPage('library')}
                          >
                            从模板与样式库选择
                          </button>
                          <p className="text-xs text-zinc-500">
                            选择模板与样式后，可以将引用插入到当前流程。
                          </p>
                          {detail.plan?.resources?.length ? detail.plan.resources.map(resource => (
                            <ResourcePreview
                              types={resourceTypes}
                              key={resource.key}
                              resource={{ ...resource, problems: [...new Map([
                                ...(resource.problems || []),
                                ...detail.diagnostics.filter(item => resourceForDiagnostic(item)?.key === resource.key),
                              ].map(item => [`${item.code}/${item.line || ''}/${item.message}`, item])).values()] }}
                              busy={busy}
                              onFix={item => fixResource({ ...item, fix: item.fix ? { ...item.fix, resource_key: resource.key } : undefined })}
                              onChooseInput={() => setTab('inputs')}
                            />
                          )) : <p className="text-sm text-zinc-500">尚无已解析的资源声明。保存流程后，可在这里查看资源和设置数据目录。</p>}
                          <details className="rounded border border-zinc-200 p-3 dark:border-zinc-800">
                            <summary className="cursor-pointer text-xs text-zinc-500">流程包内文件（{detail.resources?.length || 0}）</summary>
                            <div className="mt-2 space-y-2">
                          {detail.resources?.map((item) => (
                            <div
                              key={item.path}
                              className="flex items-center gap-2 rounded border border-zinc-200 p-2 text-xs dark:border-zinc-800"
                            >
                              <span className="min-w-0 flex-1 break-all">
                                {item.path} · {(item.size / 1024).toFixed(1)} KB
                                · {item.exists ? '存在' : '缺失'}
                              </span>
                              <button
                                disabled={busy || readOnly || dirty}
                                className={buttonClass}
                                onClick={() => {
                                  if (window.confirm(`删除资源 ${item.path}？`))
                                    void perform(async () => {
                                      await flowApi.removeResource(
                                        detail.meta.id,
                                        item.path,
                                      )
                                      selectDetail(
                                        await flowApi.get(
                                          detail.meta.id,
                                          scheme,
                                        ),
                                      )
                                    })
                                }}
                              >
                                删除
                              </button>
                            </div>
                          ))}
                            </div>
                          </details>
                        </div>
                      )}
                    </div>
                  </div>
                </div>
                <footer className="mt-4">
                  <div className="flex items-center gap-2">
                    <button
                      disabled={busy || readOnly || !dirty}
                      className={buttonClass}
                      onClick={() => void perform(save)}
                    >
                      保存
                    </button>
                    <Link
                      aria-disabled={!canRun}
                      href={`/chat?flow=${encodeURIComponent(detail.meta.id)}`}
                      onClick={(event) => {
                        if (!canRun) event.preventDefault()
                      }}
                      className={`${buttonClass} ${!canRun ? 'pointer-events-none opacity-40' : ''}`}
                    >
                      试运行
                    </Link>
                  </div>
                  {dirty && <p className="mt-2 text-xs text-zinc-500">步骤预览显示已保存的内容；保存后更新预览并可试运行。</p>}
                </footer>
              </section>
            ) : (
              <section className="flex flex-1 items-center justify-center rounded border border-dashed border-zinc-300 p-10 text-sm text-zinc-500 dark:border-zinc-700">
                选择一个流程，查看步骤、输入和资源。
              </section>
            )}
          </div>
        )}
      </div>
      {dialog && (
        <PathDialog
          key={dialog}
          title={
            {
              new: '从模板新建流程',
              import: '导入流程包',
              copy: '复制为我的流程',
              export: '导出流程包',
              asset: '上传或替换资源',
            }[dialog]
          }
          fields={dialogFields()}
          pendingMessage={['new', 'import', 'copy'].includes(dialog)
            ? '正在保存流程…'
            : undefined}
          checks={
            dialog === 'import'
              ? [
                  {
                    key: 'overwrite',
                    label: '覆盖同编号流程（旧版本自动备份）',
                  },
                ]
              : dialog === 'export'
                ? [
                  { key: 'include', label: '包含实际数据文件' },
                  { key: 'library', label: '附带依赖的模板与样式', initial: true },
                ]
                : []
          }
          onClose={() => setDialog(null)}
          onSubmit={async (values, checks) => {
            switch (dialog) {
              case 'new': {
                const created = await flowApi.create(
                  flowTemplate.replace(
                    '名称: 新流程',
                    `名称: ${JSON.stringify(values.name)}`,
                  ),
                  values.id,
                )
                selectDetail(created)
                setTab('steps')
                setNotice('流程已创建。')
                break
              }
              case 'import': {
                if (dirty && !confirmDiscardChanges())
                  throw new Error('已保留当前未保存内容')
                const result = await flowApi.import(
                  values.path,
                  Boolean(checks.overwrite),
                  values.id || undefined,
                )
                selectDetail(result.flow)
                setTab('steps')
                break
              }
              case 'copy':
                if (detail) {
                  selectDetail(
                    await flowApi.copy(detail.meta.id, values.id, values.name),
                  )
                  setTab('steps')
                }
                break
              case 'export':
                if (detail) {
                  const result = await flowApi.export(
                    detail.meta.id,
                    values.path,
                    Boolean(checks.include),
                    Boolean(checks.library),
                  )
                  setNotice(`已导出：${result.zip_path}`)
                }
                break
              case 'asset':
                if (detail) {
                  await flowApi.importResource(
                    detail.meta.id,
                    values.path,
                    values.directory,
                  )
                  selectDetail(await flowApi.get(detail.meta.id, scheme))
                }
                break
            }
            await refreshAfterWrite()
          }}
        />
      )}
      {resourceChoice && <PathDialog
        title={`为「${resourceChoice.resource.label}」选择资源`}
        fields={[{
          key: 'id', label: '资源候选',
          options: resourceChoice.candidates.map(candidate => ({
            value: candidate.id,
            label: `${candidate.name} · ${candidate.owner === 'library_builtin' ? '内置' : '用户'} · ${candidate.id}`,
          })),
        }]}
        description={resourceChoice.candidates.length ? '选择会记录到当前流程的资源锁定中。' : '没有可选资源，请先到模板与样式库导入所需资源。'}
        submitDisabled={!resourceChoice.candidates.length}
        onClose={() => setResourceChoice(null)}
        onSubmit={async values => {
          if (!resourceChoice.candidates.some(candidate => candidate.id === values.id)) throw new Error('请选择有效的资源')
          await repairResource('choose_resource', resourceChoice.resource, values.id)
        }}
      />}
    </div>
  )
}
