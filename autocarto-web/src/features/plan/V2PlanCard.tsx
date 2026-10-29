'use client'
import { useEffect, useState } from 'react'
import { pickDirectoryPath, pickFilePath, isDesktopRuntime } from '@/lib/config-form-helpers'
import { useInstanceStore } from '@/stores/instanceStore'
import { useChatStore } from '@/stores/chatStore'
import { layerApi, resourceApi } from '@/features/guides/api'
import ResourceMedia from '@/features/guides/ResourceMedia'
import type { LayerItem, LibraryItem, ProjectLayer } from '@/features/guides/types'
import { planApi } from './api'
import { useFlowRunStore } from './store'
import { requestPlanChange } from './PlanCard'
import type { PlanData, PlanPrecheck, PlanSnapshot } from './types'

const sourceLabels: Record<string, string> = {user: '您提供', project: '工程图层', builtin: '内置图层', retrieve: '自动检索'}
const statusLabels = {available: '可用', confirm: '请确认', unavailable: '不可用'}
const methodLabels: Record<string, string> = {osm: 'OpenStreetMap', osm_extract: 'OSM 提取包', overpass: 'Overpass', ne: 'Natural Earth', agent: '取数智能体', gee: 'Google Earth Engine', tianditu: '天地图', portal: '数据门户', web_search: '联网搜索', statistics: '统计库', builtin: '内置图层', store: '数据仓'}
const formatMethod = (method: string) => methodLabels[method] || method
export default function V2PlanCard({snapshot, executionMode}: {snapshot: PlanSnapshot; executionMode: 'plan' | 'build'}) {
  const {plan} = snapshot
  const [editing, setEditing] = useState<string | null>(null), [styles, setStyles] = useState<LibraryItem[]>([])
  const [busy, setBusy] = useState(false), [error, setError] = useState('')
  const styleNames = plan.data.map(item => item.style || '').filter(Boolean).join('\n')
  useEffect(() => {
    if (!styleNames) return
    let active = true
    void resourceApi.list().then(items => { if (active) setStyles(items.filter(item => item.type === 'style')) }).catch(reason => { if (active) setError(`样式预览读取失败：${reason instanceof Error ? reason.message : reason}`) })
    return () => { active = false }
  }, [styleNames])
  const actionable = ['draft', 'awaiting_confirmation', 'preview', 'ready', 'needs_data'].includes(snapshot.status)
  const unavailable = snapshot.precheck?.some(item => item.status === 'unavailable')
  const names = new Map([...plan.data.map(item => [item.id, item.name] as const), ...plan.steps.flatMap(step => (step.produces || []).map(output => [output.id, `${step.title}的产出（${output.id}）`] as const))])
  const perform = async (operation: () => Promise<PlanSnapshot>) => {
    setBusy(true); setError('')
    try { useFlowRunStore.getState().applyPlan(await operation(), snapshot.session_id) } catch (reason) { setError(reason instanceof Error ? reason.message : '操作未完成') } finally { setBusy(false) }
  }
  return <section aria-label={`制图计划 ${plan.goal}`} id={`plan-${plan.plan_id}`} className="mb-3 space-y-3 rounded border border-blue-200 bg-surface-raised p-3 text-xs">
    <header><h2 className="text-sm font-semibold">制图计划{plan.guide ? ` · ${typeof plan.guide === 'string' ? plan.guide : plan.guide.name || plan.guide.id}` : ''}</h2></header><p className="font-medium">{plan.goal}</p>
    {!!plan.facts.length && <div aria-label="已知信息"><h3 className="mb-1 font-medium">已知信息</h3>{plan.facts.map((fact, index) => <p key={index}>{fact.name}：{typeof fact.value === 'string' ? fact.value : JSON.stringify(fact.value)} <span className="text-text-muted">（{fact.from}）</span>{actionable && <button className="ml-2 text-blue-600" onClick={() => requestPlanChange(`把${fact.name}改为`)}>修改</button>}</p>)}</div>}
    {!!plan.assumptions.length && <div aria-label="采用的假设"><h3 className="mb-1 font-medium">采用的假设</h3>{plan.assumptions.map((item, index) => <p key={index}>{item.text}{item.impact === 'high' ? '（影响结果）' : ''}{actionable && <button className="ml-2 text-blue-600" onClick={() => requestPlanChange(`请调整计划中的假设“${item.text}”：`)}>修改</button>}</p>)}</div>}
    <div aria-label="计划数据"><h3 className="font-medium">数据与来源</h3>{plan.data.map(data => {
      const check = snapshot.precheck?.find(item => item.data_id === data.id), style = styles.find(item => item.name === data.style || item.id === data.style)
      return <div key={data.id} className="my-2 space-y-2 rounded border p-2"><div className="flex items-start justify-between gap-2"><strong>{data.name}</strong>{check && <span className={`rounded px-2 py-0.5 ${check.status === 'available' ? 'bg-emerald-50 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-200' : check.status === 'confirm' ? 'bg-amber-50 text-amber-800 dark:bg-amber-950 dark:text-amber-200' : 'bg-red-50 text-red-800 dark:bg-red-950 dark:text-red-200'}`}>{statusLabels[check.status]}</span>}</div>
        <p className="break-all">来源：{sourceLabels[data.source || ''] || data.source}{data.ref ? `「${data.ref}」` : ''}{check?.pack ? `（${check.pack}）` : ''}{data.source === 'retrieve' && data.method ? ` · ${formatMethod(data.method)}` : ''}</p>
        {check?.reason && <p>依据：{check.reason}</p>}{check?.suggestion && <p className="text-amber-700">{check.suggestion}</p>}
        {data.extent && <p>范围：{data.extent.from ? `${names.get(data.extent.from) || data.extent.from} 外扩${Math.round((data.extent.expand || 0) * 100)}%（执行时计算）` : data.extent.region || data.extent.bbox?.join(', ')}</p>}
        {data.style && <div className="flex items-center gap-2"><span>样式「{data.style}」</span>{style && <div className="w-28"><ResourceMedia resource={style}/></div>}</div>}
        {data.source === 'retrieve' && <details><summary className="cursor-pointer">检索方式与取舍</summary><p className="mt-1">顺序：{(check?.methods || (data.method ? [data.method] : [])).map(formatMethod).join(' → ') || '执行前按类别、范围与比例尺选择'}</p>{data.need?.description && <p>要求：{data.need.description}</p>}{!!data.need?.fields?.length && <p>所需字段：{data.need.fields.join('、')}</p>}{check?.selection && <p>取舍与简化：{check.selection}</p>}<p>预计耗时：{check?.estimated_seconds ? `约 ${Math.ceil(check.estimated_seconds / 60)} 分钟` : '暂无该来源的实测参考'}</p></details>}
        {actionable && <button className="text-blue-600" disabled={busy} onClick={() => setEditing(editing === data.id ? null : data.id)}>修改来源</button>}
        {editing === data.id && <SourceEditor snapshot={snapshot} data={data} check={check} onDone={() => setEditing(null)}/>}</div>
    })}</div>
    <details><summary className="cursor-pointer">步骤 · {plan.steps.map((step, index) => `${index + 1} ${step.title}`).join(' → ')}</summary><ol className="mt-2 space-y-2">{plan.steps.map((step, index) => <li key={step.id}><strong>{index + 1}. {step.title}</strong><p>使用：{(step.inputs || []).map(id => names.get(id) || id).join('、') || '无需额外数据'}</p><p>产出：{(step.produces || []).map(item => item.id).join('、') || '完成本步骤操作'}</p></li>)}</ol></details>
    <p>成果：{plan.deliverables.map(item => typeof item === 'string' ? item : item.label).join('、')}</p>
    {!!plan.checks.length && <details><summary>成图后请核对</summary><ul className="mt-1 list-inside list-disc">{plan.checks.map((item, index) => <li key={index}>{item}</li>)}</ul></details>}
    {!!snapshot.notes?.length && <details><summary>计划整理说明</summary>{snapshot.notes.map((note, index) => <p key={index}>{note}</p>)}</details>}
    {snapshot.confirmation_reasons?.map((reason, index) => <p key={index} className="text-amber-700">{reason}</p>)}
    {unavailable && <p>请先处理不可用的数据，再开始执行。</p>}{executionMode === 'plan' && actionable && <p>切换到 Build 后执行</p>}
    {actionable && <div className="flex gap-2">{executionMode === 'build' && ['ready', 'awaiting_confirmation', 'preview'].includes(snapshot.status) && <button className="rounded bg-blue-600 px-3 py-1 text-white disabled:opacity-50" disabled={busy || unavailable} onClick={() => void perform(() => planApi.start(snapshot))}>开始</button>}<button className="rounded border px-3 py-1" disabled={busy} onClick={() => void perform(() => planApi.cancel(snapshot))}>取消</button></div>}
    {busy && <p role="status">正在处理…</p>}{error && <p role="alert" className="text-red-700">{error}</p>}
  </section>
}
export function SourceEditor({snapshot, data, check, onDone}: {snapshot: PlanSnapshot; data: PlanData; check?: PlanPrecheck; onDone: () => void}) {
  const [source, setSource] = useState(data.source || 'user'), [ref, setRef] = useState(data.ref || ''), [method, setMethod] = useState(data.method || '')
  const [layers, setLayers] = useState<ProjectLayer[]>([]), [builtin, setBuiltin] = useState<LayerItem[]>([])
  const [busy, setBusy] = useState(false), [error, setError] = useState('')
  const active = useInstanceStore(state => state.activeInstanceId), bound = useChatStore(state => state.sessionRuntimeStates[snapshot.session_id]?.bound_instance_id)
  useEffect(() => {
    if (source !== 'builtin' && source !== 'project') return
    let current = true
    const promise = source === 'builtin' ? layerApi.list().then(value => { if (current) setBuiltin(value.items) }) : bound || active ? resourceApi.layers((bound || active)!).then(value => { if (current) setLayers(value.items) }) : Promise.reject(new Error('请连接当前工程后选择图层'))
    void promise.catch(reason => { if (current) setError(reason instanceof Error ? reason.message : '来源读取失败') })
    return () => { current = false }
  }, [source, bound, active])
  const save = async () => {
    setBusy(true); setError('')
    try { useFlowRunStore.getState().applyPlan(await planApi.editData(snapshot, data.id, source === 'retrieve' ? {source, ref: '', method} : {source, ref: ref.trim(), method: ''}), snapshot.session_id); onDone() } catch (reason) { setError(reason instanceof Error ? reason.message : '来源修改失败') } finally { setBusy(false) }
  }
  const browse = async (folder: boolean) => { try { const value = await (folder ? pickDirectoryPath() : pickFilePath()); if (value) setRef(value) } catch (reason) { setError(String(reason)) } }
  return <div aria-label={`修改数据 ${data.name}`} className="space-y-2 rounded bg-surface-overlay p-2"><label className="block">数据来源方式<select className="ml-2 rounded border p-1" value={source} onChange={event => { setSource(event.target.value as typeof source); setRef(''); setError('') }}>{Object.entries(sourceLabels).map(([key, label]) => <option key={key} value={key}>{label}</option>)}</select></label>
    {source === 'user' && <><input aria-label="数据路径" className="w-full rounded border p-1" value={ref} onChange={event => setRef(event.target.value)} placeholder="文件或文件夹路径"/>{isDesktopRuntime() && <><button onClick={() => void browse(false)}>选择文件</button><button className="ml-2" onClick={() => void browse(true)}>选择文件夹</button></>}</>}
    {source === 'project' && <select aria-label="工程图层" className="w-full rounded border p-1" value={ref} onChange={event => setRef(event.target.value)}><option value="">请选择工程图层</option>{layers.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select>}
    {source === 'builtin' && <select aria-label="内置图层" className="w-full rounded border p-1" value={ref} onChange={event => setRef(event.target.value)}><option value="">请选择内置图层</option>{builtin.map(item => <option key={item.id} value={item.name}>{item.name}（{item.pack}）</option>)}</select>}
    {source === 'retrieve' && <label className="block">检索方式<select className="ml-2 rounded border p-1" value={method} onChange={event => setMethod(event.target.value)}><option value="">自动选择</option>{[...new Set([...(check?.methods || []), ...(data.method ? [data.method] : []), 'osm', 'gee', 'tianditu', 'portal', 'web_search'])].map(item => <option key={item} value={item}>{formatMethod(item)}</option>)}</select></label>}
    <p className="text-text-muted">保存后重新检查此数据是否可用。</p>{error && <p role="alert" className="text-red-700">{error}</p>}<button className="rounded border px-2 py-1" disabled={busy || source !== 'retrieve' && !ref.trim()} onClick={() => void save()}>{busy ? '正在检查…' : '保存来源'}</button><button className="ml-2" disabled={busy} onClick={onDone}>关闭</button>
  </div>
}
