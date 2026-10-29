'use client'
import { useEffect, useState } from 'react'
import { resourceApi } from '@/features/guides/api'
import ResourceMedia from '@/features/guides/ResourceMedia'
import type { LibraryItem } from '@/features/guides/types'
import type { DataFindResult, PlanData, PlanSnapshot } from './types'
import { planSourceName, selectedPlanSource } from './sources'
import V2PlanCard from './V2PlanCard'

export function requestPlanChange(text: string) { window.dispatchEvent(new CustomEvent('autocarto:compose-message', {detail: {text}})) }
export default function PlanCard(props: {snapshot: PlanSnapshot; executionMode: 'plan' | 'build'}) {
  return props.snapshot.plan.api_version === 'carto-plan/v2' ? <V2PlanCard {...props}/> : <LegacyPlanCard snapshot={props.snapshot}/>
}
function LegacyPlanCard({snapshot}: {snapshot: PlanSnapshot}) {
  const {plan} = snapshot
  const [styles, setStyles] = useState<LibraryItem[]>([]), [error, setError] = useState('')
  const styleNames = plan.data.map(item => item.style || '').filter(Boolean).join('\n')
  useEffect(() => {
    if (!styleNames) return
    let active = true
    void resourceApi.list().then(items => { if (active) setStyles(items.filter(item => item.type === 'style')) }).catch(reason => { if (active) setError(`样式预览读取失败：${String(reason)}`) })
    return () => { active = false }
  }, [styleNames])
  const labels: Record<string, string> = {conversation:'本次对话',user:'您提供',project:'工程图层',session:'本会话文件',step:'由步骤产生',acquire:'自动获取'}
  const guide = typeof plan.guide === 'string' ? plan.guide : plan.guide?.name || plan.guide?.id
  return <section aria-label={`制图计划 ${plan.goal}`} id={`plan-${plan.plan_id}`} className="mb-3 space-y-3 rounded border border-blue-200 bg-surface-raised p-3 text-xs">
    <h2 className="text-sm font-semibold">制图计划{guide ? ` · ${guide}` : ''}</h2><p className="font-medium">{plan.goal}</p><p className="text-text-muted">旧版计划（只读）</p>
    <button className="rounded border px-2 py-1" onClick={() => requestPlanChange(`请按新格式重新生成制图计划。原始需求：${plan.goal}${guide ? `；流程：${guide}` : ''}`)}>按新格式重新生成</button>
    {!!plan.facts.length && <div aria-label="已知信息"><h3 className="font-medium">已知信息</h3>{plan.facts.map((fact, index) => <p key={index}>{fact.name}：{typeof fact.value === 'string' ? fact.value : JSON.stringify(fact.value)}（{fact.from}）</p>)}</div>}
    {!!plan.assumptions.length && <div><h3 className="font-medium">采用的假设</h3>{plan.assumptions.map((item, index) => <p key={index}>{item.text}</p>)}</div>}
    <div aria-label="计划数据"><h3 className="font-medium">数据与来源</h3>{plan.data.map(data => <div key={data.id} className="my-1 rounded border p-2"><p>{data.layer_name || data.name} · {data.from === 'step' && data.step ? `由第 ${plan.steps.findIndex(step => step.id === data.step) + 1} 步产生` : labels[data.from || '']}{data.path && `：${data.path}`}{data.from === 'project' && `：${data.layer_name || data.layer || data.name}`}</p>
      {data.find_id && snapshot.find_results?.[data.find_id] && <><SourceSummary result={snapshot.find_results[data.find_id]} choice={data.choice}/><FindDetails result={snapshot.find_results[data.find_id]} choice={data.choice}/></>}
      {data.style && <div className="mt-2 flex items-center gap-2"><span>样式「{data.style}」</span>{styles.find(item => item.name === data.style || item.id === data.style) && <div className="w-28"><ResourceMedia resource={styles.find(item => item.name === data.style || item.id === data.style)!}/></div>}</div>}
    </div>)}</div>
    <details><summary>步骤 · {plan.steps.map((step, index) => `${index + 1} ${step.title}`).join(' → ')}</summary><ol className="mt-2 space-y-2">{plan.steps.map((step, index) => <li key={step.id}><strong>{index + 1}. {step.title}</strong><p>{step.instructions}</p>{!!step.outputs?.length && <p>产出：{step.outputs.map(item => item.label).join('、')}</p>}</li>)}</ol></details>
    <p>产出：{plan.deliverables.map(item => typeof item === 'string' ? item : item.label).join('、') || '按步骤记录'}</p>
    {!!plan.checks.length && <details><summary>成图后请核对</summary>{plan.checks.map((check, index) => <p key={index}>{check}</p>)}</details>}{error && <p role="alert">{error}</p>}
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
