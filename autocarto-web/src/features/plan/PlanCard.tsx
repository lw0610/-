'use client'

import { useEffect, useState } from 'react'
import api from '@/lib/api'
import { pickDirectoryPath, pickFilePath, isDesktopRuntime } from '@/lib/config-form-helpers'
import { useInstanceStore } from '@/stores/instanceStore'
import { useChatStore } from '@/stores/chatStore'
import { planApi } from './api'
import { useFlowRunStore } from './store'
import { resourceApi } from '@/features/guides/api'
import ResourceMedia from '@/features/guides/ResourceMedia'
import type { LibraryItem } from '@/features/guides/types'
import type { DataFindResult, PlanData, PlanSnapshot } from './types'
import { planSourceName, planSources, selectedPlanSource } from './sources'

export function requestPlanChange(text: string) { window.dispatchEvent(new CustomEvent('autocarto:compose-message', { detail: { text } })) }
const valueText = (value: unknown) => typeof value === 'string' ? value : JSON.stringify(value)
const fromLabels: Record<string, string> = { conversation: '本次对话', user: '您提供', project: '工程图层', session: '本会话文件', step: '由步骤产生', acquire: '自动获取' }
export default function PlanCard({ snapshot, executionMode }: { snapshot: PlanSnapshot; executionMode: 'plan' | 'build' }) {
  const [busy, setBusy] = useState(false), [error, setError] = useState(''), [editing, setEditing] = useState<string | null>(null)
  const plan = snapshot.plan
  const [styles, setStyles] = useState<LibraryItem[]>([])
  const styleNames = plan.data.map(item => item.style || '').filter(Boolean).join('\n')
  useEffect(() => {
    if (!styleNames) return
    let active = true
    void resourceApi.list().then(items => { if (active) setStyles(items.filter(item => item.type === 'style')) }).catch(() => {})
    return () => { active = false }
  }, [styleNames])
  const actionable = ['draft', 'awaiting_confirmation', 'preview', 'ready'].includes(snapshot.status)
  const run = async (operation: () => Promise<PlanSnapshot>) => {
    setBusy(true); setError('')
    try { useFlowRunStore.getState().applyPlan(await operation(), snapshot.session_id) }
    catch (reason) { setError(reason instanceof Error ? reason.message : '操作未完成') }
    finally { setBusy(false) }
  }
  return <section aria-label={`制图计划 ${plan.goal}`} id={`plan-${plan.plan_id}`} className="mb-3 space-y-3 rounded border border-blue-200 bg-surface-raised p-3 text-xs">
    <header className="flex justify-between gap-2"><h2 className="text-sm font-semibold">制图计划{plan.guide && ` · ${plan.guide.name || plan.guide.id}`}</h2><span>v{plan.version}</span></header>
    <p className="font-medium">{plan.goal}</p>
    {!!plan.facts.length && <div aria-label="已知信息"><h3 className="mb-1 font-medium">已知信息</h3>{plan.facts.map((fact, index) => <p key={index}>{fact.name}：{valueText(fact.value)} <span className="text-text-muted">（{fact.from}）</span>{actionable && <button disabled={busy} className="ml-2 text-blue-600" onClick={() => requestPlanChange(`把${fact.name}改为`)}>修改</button>}</p>)}</div>}
    {!!plan.assumptions.length && <div aria-label="采用的假设"><h3 className="mb-1 font-medium">采用的假设</h3>{plan.assumptions.map((item, index) => <p key={index}>{item.text}{item.impact === 'high' && '（影响结果）'}{actionable && <button disabled={busy} className="ml-2 text-blue-600" onClick={() => requestPlanChange(`请调整计划中的假设“${item.text}”：`)}>修改</button>}</p>)}</div>}
    <div aria-label="计划数据"><h3 className="mb-1 font-medium">数据与来源</h3>{plan.data.map(data => <div key={data.id} className="my-1 rounded border p-2"><div className="flex justify-between gap-2"><span>{data.layer_name || data.name} · {data.from === 'step' && data.step ? `由第 ${plan.steps.findIndex(step => step.id === data.step) + 1} 步产生` : fromLabels[data.from]}{data.path && `：${data.path}`}{data.from === 'project' && `：${data.layer_name || data.layer || data.name}`}{data.step && `（${plan.steps.find(step => step.id === data.step)?.title || data.step}）`}</span>{actionable && <button disabled={busy} className="shrink-0 text-blue-600" onClick={() => setEditing(editing === data.id ? null : data.id)}>修改来源</button>}</div>
      {data.find_id && snapshot.find_results?.[data.find_id] && <SourceSummary result={snapshot.find_results[data.find_id]} choice={data.choice}/>}
      {data.style && <div className="mt-2 flex items-center gap-2"><span>样式「{data.style}」</span>{styles.find(item => item.name === data.style || item.id === data.style) && <div className="w-28"><ResourceMedia resource={styles.find(item => item.name === data.style || item.id === data.style)!}/></div>}</div>}
      {data.find_id && snapshot.find_results?.[data.find_id] && <FindDetails result={snapshot.find_results[data.find_id]} choice={data.choice} />}
      {editing === data.id && <DataEditor snapshot={snapshot} data={data} onDone={() => setEditing(null)} />}
    </div>)}</div>
    <details><summary className="cursor-pointer">步骤 · {plan.steps.map((step, index) => `${index + 1} ${step.title}`).join(' → ')}</summary><ol className="mt-2 space-y-2">{plan.steps.map((step, index) => <li key={step.id}><strong>{index + 1}. {step.title}</strong><p>{step.instructions}</p><p>能力：{step.kind === 'acquire' ? '数据获取' : (step.capabilities || []).join('、') || '系统'}</p>{!!step.layers?.length && <p>图层：{step.layers.join('、')}</p>}{!!step.outputs?.length && <p>产出：{step.outputs.map(item => item.label).join('、')}</p>}</li>)}</ol></details>
    <p>产出：{plan.deliverables.map(item => item.label).join('、') || '按步骤记录'}</p>
    {!!plan.checks.length && <details><summary>完成检查</summary><ul>{plan.checks.map((check, index) => <li key={index}>{check}</li>)}</ul></details>}
    {snapshot.confirmation_reasons?.map((reason, index) => <p key={index} className="text-amber-700">{reason}</p>)}
    {executionMode === 'plan' && actionable && <p>切换到 Build 后执行</p>}
    <div className="flex gap-2">{actionable && executionMode === 'build' && ['ready', 'awaiting_confirmation', 'preview'].includes(snapshot.status) && <button disabled={busy} className="rounded bg-blue-600 px-3 py-1 text-white disabled:opacity-50" onClick={() => void run(() => planApi.start(snapshot))}>开始</button>}{actionable && <button disabled={busy} className="rounded border px-3 py-1" onClick={() => void run(() => planApi.cancel(snapshot))}>取消</button>}{snapshot.status === 'cancelled' && <p>该计划已取消</p>}{snapshot.status === 'ready' && <p>计划已通过检查，正在准备执行</p>}</div>
    {busy && <p role="status">正在处理…</p>}{error && <p role="alert" className="text-red-700">{error}</p>}
  </section>
}

function SourceSummary({ result, choice }: { result: DataFindResult; choice?: PlanData['choice'] }) {
  return <div className="mt-1 space-y-1">{result.rows.map(row => {
    const source = selectedPlanSource(result, row, choice)
    const candidate = source?.candidate
    return <p key={row.row}>{source ? `${planSourceName(source)} · ${candidate?.match_reason || candidate?.reasons?.join('；') || ''}` : '尚无可用来源'}{(candidate?.requires_confirmation || candidate?.confidence === 'confirm') && <strong className="ml-2 rounded bg-amber-100 px-2 py-0.5 text-amber-900">请确认</strong>}</p>
  })}</div>
}

function FindDetails({ result, choice }: { result: DataFindResult; choice?: PlanData['choice'] }) {
  return <details className="mt-1"><summary className="cursor-pointer">查看候选来源与依据</summary>{(result.rows || []).map(row => {
    const source = selectedPlanSource(result, row, choice)
    const candidate = source?.candidate
    return <div key={row.row} className="mt-2"><p>{row.row}：{source ? planSourceName(source) : '尚无可用来源'}</p>{candidate?.reasons?.map((reason, index) => <p key={index}>{reason}</p>)}{candidate?.notes?.map((note, index) => <p key={index} className="text-text-muted">{note}</p>)}{row.excluded?.map((item, index) => <p key={index} className="text-text-muted">未采用 {item.source_id}：{item.reason}</p>)}</div>
  })}</details>
}

function DataEditor({ snapshot, data, onDone }: { snapshot: PlanSnapshot; data: PlanData; onDone: () => void }) {
  const [kind, setKind] = useState<'acquire' | 'user' | 'project'>(data.from === 'project' ? 'project' : data.from === 'user' ? 'user' : 'acquire')
  const [path, setPath] = useState(data.path || ''), [layer, setLayer] = useState(data.layer || '')
  const [layers, setLayers] = useState<{ id: string; name: string }[]>([])
  const [found, setFound] = useState<DataFindResult | undefined>(data.find_id ? snapshot.find_results?.[data.find_id] : undefined)
  const [choice, setChoice] = useState<Record<string, string>>(typeof data.choice === 'object' ? data.choice : {})
  const [busy, setBusy] = useState(false), [error, setError] = useState('')
  const activeInstance = useInstanceStore(state => state.activeInstanceId)
  const bound = useChatStore(state => state.sessionRuntimeStates[snapshot.session_id]?.bound_instance_id)
  const select = async (value: typeof kind) => {
    setKind(value); setError('')
    try {
      if (value === 'acquire' && data.find_id) setFound(await planApi.find(snapshot.session_id, data.find_id))
      if (value === 'project') {
        const instance = bound || activeInstance
        if (!instance) throw new Error('请连接当前工程后选择图层')
        setLayers((await api.get<{ items: { id: string; name: string }[] }>(`/api/instances/${encodeURIComponent(instance)}/layers`)).items || [])
      }
    } catch (reason) { setError(reason instanceof Error ? reason.message : '来源列表读取失败') }
  }
  const browse = async (folder: boolean) => { try { const selected = await (folder ? pickDirectoryPath() : pickFilePath()); if (selected) setPath(selected) } catch (reason) { setError(String(reason)) } }
  const save = async () => {
    setBusy(true); setError('')
    try {
      const update = kind === 'user' ? { from: kind, path } : kind === 'project' ? { from: kind, layer } : { from: kind, choice: Object.keys(choice).length ? choice : data.choice || 'auto' }
      useFlowRunStore.getState().applyPlan(await planApi.editData(snapshot, data.id, update), snapshot.session_id); onDone()
    } catch (reason) { setError(reason instanceof Error ? reason.message : '来源修改失败') }
    finally { setBusy(false) }
  }
  return <div className="mt-2 space-y-2 rounded bg-surface-overlay p-2" aria-label={`修改数据 ${data.name}`}>
    <select aria-label="数据来源方式" value={kind} disabled={busy} onChange={event => void select(event.target.value as typeof kind)}><option value="acquire">换来源</option><option value="user">我来提供</option><option value="project">用工程图层</option></select>
    {kind === 'user' && <div><input aria-label="数据路径" value={path} onChange={event => setPath(event.target.value)} className="w-full rounded border p-1" />{isDesktopRuntime() && <><button onClick={() => void browse(false)}>选择文件</button><button className="ml-2" onClick={() => void browse(true)}>选择文件夹</button></>}</div>}
    {kind === 'project' && <select aria-label="工程图层" value={layer} onChange={event => setLayer(event.target.value)} onFocus={() => { if (!layers.length) void select('project') }}><option value="">请选择</option>{layers.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select>}
    {kind === 'acquire' && <>{!data.find_id && <p>该数据尚无候选查询，请在对话中说明要获取的数据。</p>}{data.find_id && !found && <button onClick={() => void select('acquire')}>查看可用来源</button>}{found?.rows?.map(row => {
      const selected = selectedPlanSource(found, row, Object.keys(choice).length ? choice : data.choice)
      return <label key={row.row} className="block">{row.row}<select aria-label={row.row} className="ml-2 max-w-full" value={selected?.candidate.id || ''} onChange={event => setChoice(current => ({ ...current, [row.row]: event.target.value }))}>
        {!selected && <option value="" disabled>请选择可用来源</option>}
        {planSources(found, row).map(source => <option key={source.candidate.id} value={source.candidate.id} disabled={!source.candidate.available}>{planSourceName(source)} · {source.candidate.reasons?.join('；')}</option>)}
      </select>{row.excluded?.map((item, index) => <p key={index} className="text-text-muted">{item.source_id}：{item.reason}</p>)}</label>
    })}</>}
    {error && <p role="alert" className="text-red-700">{error}</p>}<button disabled={busy || kind === 'acquire' && !data.find_id || kind === 'user' && !path.trim() || kind === 'project' && !layer} className="rounded border px-2 py-1" onClick={() => void save()}>保存来源</button><button disabled={busy} className="ml-2" onClick={onDone}>关闭</button>
  </div>
}
