'use client'

import { useState } from 'react'
import { ActorsField, Field, SelectField, TextField, type EditorProps } from './EntityEditors'
import { buttonClass, fieldClass, itemId, itemOptions, items, newId, type CompassBearing, type GraphItem, type RouteNode, type RoutePath, type SpatialConstraint } from './model'

export default function RouteEditor({ item, graph, onChange, disabled }: EditorProps) {
  const [hint, setHint] = useState('')
  const paths = (Array.isArray(item.paths) ? item.paths : []) as RoutePath[]
  const mainPath = paths.find((path) => path.role === 'main')
  const resolveNode = (node: RouteNode) => node.ref_type === 'event'
    ? String(items(graph, 'events').find((event) => itemId(event, 'events') === node.ref_id)?.location_ref || '') : node.ref_id
  const mainIds = Array.from(new Set((mainPath?.nodes || []).map(resolveNode).filter(Boolean)))
  const nodeOptions: Array<[string, string]> = [
    ...itemOptions(graph, 'spatial_entities').filter(([id]) => items(graph, 'spatial_entities').find((place) => itemId(place, 'spatial_entities') === id)?.map_role !== 'background').map(([id, name]): [string, string] => [`spatial_entity:${id}`, `${items(graph, 'spatial_entities').find((place) => itemId(place, 'spatial_entities') === id)?.entity_kind === 'cartographic_control' ? '制图控制点' : '地点'} · ${name}`]),
    ...itemOptions(graph, 'events').map(([id, name]): [string, string] => [`event:${id}`, `事件 · ${name}`]),
  ]
  const patchPath = (id: string, patch: Partial<RoutePath>) => onChange({ paths: paths.map((path) => path.path_id === id ? { ...path, ...patch } : path) })
  const setTopology = (topology: string) => {
    const secondary = paths.filter((path) => path.role !== 'main')
    if ((topology === 'standard' && secondary.length) || (topology === 'branch' && secondary.length > 1)) {
      setHint(topology === 'standard' ? '请先删除支路/汇入路径，再改为普通路线。' : '当前适配器只支持一条分叉支路，请先减少汇入路径。'); return
    }
    setHint('')
    onChange({ topology, style_ref: undefined, paths: paths.map((path) => path.role === 'main' ? path : { ...path, role: topology === 'branch' ? 'branch' : 'incoming', direction: topology === 'branch' ? 'away_from_attach' : 'toward_attach' }) })
  }
  return <fieldset
    disabled={disabled}
    className="space-y-4"
  >

    <TextField
      title="路线名称"
      value={item.name}
      onChange={(name) => onChange({ name })}
    />

    <div className="grid grid-cols-2 gap-4">

      <SelectField
        title="路线类型"
        value={item.topology}
        onChange={setTopology}
        options={[['standard', '普通路线'], ['branch', '分叉'], ['multi_start', '多起点汇合']]}
      />

      <SelectField
        title="敌我属性"
        value={item.side}
        onChange={(side) => onChange({ side, style_ref: undefined })}
        options={[['friendly', '我方'], ['enemy', '敌方'], ['neutral', '中立']]}
      />

    </div>

    {hint && <p
      role="alert"
      className="text-xs text-amber-700"
    >
      {hint}
    </p>}

    <p className="text-xs text-zinc-500">每条路线恰有一条主路径；普通路径至少两个节点，仅有目标地点时可通过来向生成进攻示意箭头。分叉仅支持一条支路；汇合可有多条汇入路径。连接点必须在主路径上，不应在支路节点中重复。</p>

    <p className="text-xs text-zinc-500">背景地点不能直接加入路线；请先在地点页明确其地图用途。</p>

    {paths.some((path) => path.nodes?.some((node) => items(graph, 'spatial_entities').some((place) => itemId(place, 'spatial_entities') === node.ref_id && place.entity_kind === 'cartographic_control'))) && <p className="text-xs text-amber-700">此路线含旧版手工控制点。新的空间约束会在制图时重新计算，旧版手工控制点仍使用已保存坐标。</p>}

    {paths.map((path, index) => <section
      key={path.path_id}
      className="space-y-3 rounded border border-zinc-200 p-3 dark:border-zinc-700"
    >

      <div className="flex items-center justify-between gap-2">
        <h4 className="text-sm font-semibold">{path.role === 'main' ? '主路径' : path.role === 'branch' ? '分叉支路' : '汇入路径'} · {index + 1}</h4>
        {path.role !== 'main' && <button
          type="button"
          className={buttonClass}
          onClick={() => { if (window.confirm('删除此路径及其节点？地点与事件本身不会删除。')) onChange({ paths: paths.filter((candidate) => candidate.path_id !== path.path_id) }) }}
        >删除路径</button>}
      </div>

      <ActorsField title={`路径 ${index + 1} 所属部队`} value={path.actor_refs} graph={graph} onChange={(actor_refs) => patchPath(path.path_id, { actor_refs })} />
      {!path.actor_refs?.length && <p className="text-xs text-amber-700 dark:text-amber-300">尚未标明此路径的行动主体。请依据原文选择，不会自动套用其他路径的部队。</p>}

      {path.role !== 'main' && <SelectField
        title="连接主路径的地点"
        value={path.attach_ref}
        onChange={(attach_ref) => patchPath(path.path_id, { attach_ref })}
        options={itemOptions(graph, 'spatial_entities').filter(([id]) => mainIds.includes(id))}
      />}

      {(path.nodes || []).map((node, nodeIndex) => <div
        key={`${path.path_id}-${nodeIndex}`}
        className="flex items-center gap-1.5"
      >

        <span className="w-5 text-xs text-zinc-500">
          {nodeIndex + 1}
        </span>

        <select
          aria-label={`路径 ${index + 1} 节点 ${nodeIndex + 1}`}
          className={`${fieldClass} mt-0 min-w-0 flex-1`}
          value={`${node.ref_type}:${node.ref_id}`}
          onChange={(event) => {
            const split = event.target.value.indexOf(':')
            const nextNode = { ...node, ref_type: event.target.value.slice(0, split), ref_id: event.target.value.slice(split + 1) } as RouteNode
            patchPath(path.path_id, { nodes: path.nodes.map((current, at) => at === nodeIndex ? nextNode : current) })
          }}
        >
          {!nodeOptions.some(([id]) => id === `${node.ref_type}:${node.ref_id}`) && <option value={`${node.ref_type}:${node.ref_id}`}>当前节点不可用，请重新选择</option>}
          {nodeOptions.filter(([id]) => !items(graph, 'spatial_entities').some((place) => place.entity_kind === 'cartographic_control' && id === `spatial_entity:${itemId(place, 'spatial_entities')}`) || id === `${node.ref_type}:${node.ref_id}`).map(([id, text]) => <option
            key={id}
            value={id}
          >
            {text}
          </option>)}
        </select>

        {[-1, 1].map((offset) => <button
          type="button"
          key={offset}
          className={buttonClass}
          aria-label={`${offset < 0 ? '上移' : '下移'}路径 ${index + 1} 节点 ${nodeIndex + 1}`}
          disabled={nodeIndex + offset < 0 || nodeIndex + offset >= path.nodes.length}
          onClick={() => { const nodes = [...path.nodes];[nodes[nodeIndex], nodes[nodeIndex + offset]] = [nodes[nodeIndex + offset], nodes[nodeIndex]]; patchPath(path.path_id, { nodes }) }}
        >
          {offset < 0 ? '↑' : '↓'}
        </button>)}

        <button
          type="button"
          className={buttonClass}
          aria-label={`删除路径 ${index + 1} 节点 ${nodeIndex + 1}`}
          onClick={() => patchPath(path.path_id, { nodes: path.nodes.filter((_, at) => at !== nodeIndex) })}
        >移除</button>

      </div>)}

      <Field title="添加节点">
        <select
          className={fieldClass}
          value=""
          onChange={(event) => { const split = event.target.value.indexOf(':'); patchPath(path.path_id, { nodes: [...(path.nodes || []), { ref_type: event.target.value.slice(0, split) as RouteNode['ref_type'], ref_id: event.target.value.slice(split + 1) }] }) }}
        >
          <option value="">选择地点或事件后添加</option>
          {nodeOptions.filter(([id]) => !items(graph, 'spatial_entities').some((place) => place.entity_kind === 'cartographic_control' && id === `spatial_entity:${itemId(place, 'spatial_entities')}`)).map(([id, text]) => <option
            key={id}
            value={id}
          >
            {text}
          </option>)}
        </select>
      </Field>

      <PathConstraints
        path={path}
        places={items(graph, 'spatial_entities').filter((place) => place.entity_kind !== 'cartographic_control')}
        nodeIds={(path.nodes || []).map(resolveNode)}
        onChange={(patch) => patchPath(path.path_id, patch)}
      />

    </section>)}

    {item.topology !== 'standard' && <button
      type="button"
      className={buttonClass}
      disabled={item.topology === 'branch' && paths.some((path) => path.role !== 'main')}
      onClick={() => onChange({ paths: [...paths, { path_id: newId('path'), role: item.topology === 'branch' ? 'branch' : 'incoming', direction: item.topology === 'branch' ? 'away_from_attach' : 'toward_attach', attach_ref: mainIds[0] || '', nodes: [], actor_refs: [] }] })}
    >添加{item.topology === 'branch' ? '分叉支路' : '汇入路径'}</button>}

  </fieldset>
}

const bearingOptions: Array<[CompassBearing, string]> = [['N', '北'], ['NE', '东北'], ['E', '东'], ['SE', '东南'], ['S', '南'], ['SW', '西南'], ['W', '西'], ['NW', '西北']]
function PathConstraints({ path, places, nodeIds, onChange }: {
  path: RoutePath; places: GraphItem[]; nodeIds: string[]; onChange: (patch: Partial<RoutePath>) => void
}) {
  const constraints = path.spatial_constraints || []
  // Attachments are implicit in stored secondary paths, but take part in geometry.
  const attachedNodeIds = [...nodeIds]
  if (path.attach_ref && path.role === 'branch' && attachedNodeIds[0] !== path.attach_ref) attachedNodeIds.unshift(path.attach_ref)
  if (path.attach_ref && path.role === 'incoming' && attachedNodeIds[attachedNodeIds.length - 1] !== path.attach_ref) attachedNodeIds.push(path.attach_ref)
  const factualIds = new Set(places.map((place) => String(place.entity_id)))
  const effectiveNodeIds = attachedNodeIds.filter((id) => factualIds.has(id))
  const firstNode = factualIds.has(effectiveNodeIds[0]) ? effectiveNodeIds[0] : undefined
  const options: Array<[string, string]> = places.filter((place) => effectiveNodeIds.includes(String(place.entity_id))).map((place) => [String(place.entity_id), String(place.name || '未命名')])
  const segments = effectiveNodeIds.slice(0, -1).flatMap((after_ref, index) => {
    const before_ref = effectiveNodeIds[index + 1]
    if (!factualIds.has(after_ref) || !factualIds.has(before_ref)) return []
    const occurrence = effectiveNodeIds.slice(0, index + 1).filter((id, at) => id === after_ref && effectiveNodeIds[at + 1] === before_ref).length
    return [{ index, after_ref, before_ref, occurrence }]
  })
  const patch = (at: number, value: Partial<SpatialConstraint>) => onChange({ spatial_constraints: constraints.map((constraint, index) => index === at ? { ...constraint, ...value } : constraint) })
  const name = (id?: string) => String(places.find((place) => place.entity_id === id)?.name || '目标地点')
  const updateDistance = (key: 'display_length_m' | 'offset_m', value: string) => {
    const control_options = { ...path.control_options }
    if (value === '') delete control_options[key]
    else control_options[key] = Number(value) * 1000
    onChange({ control_options: Object.keys(control_options).length ? control_options : undefined })
  }
  return <section className="space-y-3 border-t border-zinc-200 pt-3 dark:border-zinc-700" aria-label="路线空间约束">
    <h5 className="text-sm font-medium">路线来向与经过方位</h5>
    <p className="text-xs text-zinc-500">只控制路线形状。定位参照与自动计算的控制点不会加入地点列表，也不会显示名称和圆点。</p>
    {constraints.map((constraint, at) => {
      const segment = segments.find((entry) => entry.after_ref === constraint.after_ref && entry.before_ref === constraint.before_ref && entry.occurrence === (constraint.occurrence ?? 1))
      const intervalValid = constraint.kind === 'approach_from' ? Boolean(path.role !== 'branch' && firstNode && constraint.target_ref === firstNode) : Boolean(segment)
      const bearing = bearingOptions.find(([key]) => key === constraint.bearing)?.[1] || '指定'
      const description = constraint.kind === 'approach_from'
        ? `从${name(constraint.target_ref)}${bearing}方向进入`
        : `经过${constraint.reference?.name || '参照地点'}${constraint.relation === 'outside' ? `以${bearing}` : `${bearing}部`}`
      return <div key={at} className="space-y-3 rounded bg-zinc-50 p-3 dark:bg-zinc-900">
        <div className="flex items-center justify-between gap-2">
          <p className="text-xs">{description}</p>
          <button type="button" className={buttonClass} onClick={() => onChange({ spatial_constraints: constraints.filter((_, index) => index !== at) })}>移除此约束</button>
        </div>
        {!intervalValid && <p className="text-xs text-amber-700">{constraint.kind === 'approach_from' && path.role === 'branch' ? '分叉支路从连接点出发，来向应设置在主路径或汇入路径。' : '节点顺序已变化，此约束的目标或相邻节点不匹配；请调整下方关联节点。'}</p>}
        <div className="grid grid-cols-2 gap-3">
          <SelectField title={`约束 ${at + 1} 方位`} value={constraint.bearing} options={bearingOptions} onChange={(value) => patch(at, { bearing: value as CompassBearing })} />
          {constraint.kind === 'approach_from'
            ? <SelectField title={`约束 ${at + 1} 目标地点`} value={constraint.target_ref} options={options.filter(([id]) => id === firstNode)} onChange={(target_ref) => patch(at, { target_ref })} />
            : <SelectField title={`约束 ${at + 1} 相对位置`} value={constraint.relation || 'within'} options={[["within", "参照区域内部（如东部）"], ["outside", "参照区域外侧（如以东）"]]} onChange={(value) => patch(at, { relation: value as 'within' | 'outside' })} />}
        </div>
        {constraint.kind === 'via_relative' && <>
          <TextField title={`约束 ${at + 1} 定位参照`} value={constraint.reference?.name} onChange={(referenceName) => patch(at, { reference: { name: referenceName } })} />
          <SelectField
            title={`约束 ${at + 1} 适用路段`}
            value={segment ? String(segment.index) : 'unavailable'}
            options={segments.map((entry) => [String(entry.index), `${entry.index + 1}. ${name(entry.after_ref)} → ${name(entry.before_ref)}${segments.filter((candidate) => candidate.after_ref === entry.after_ref && candidate.before_ref === entry.before_ref).length > 1 ? `（第 ${entry.occurrence} 次）` : ''}`])}
            onChange={(value) => {
              const selected = segments.find((entry) => String(entry.index) === value)
              if (selected) patch(at, { after_ref: selected.after_ref, before_ref: selected.before_ref, occurrence: selected.occurrence })
            }}
          />
          <p className="text-xs text-zinc-500">{typeof constraint.reference?.lon === 'number' && typeof constraint.reference?.lat === 'number' ? `参照坐标：${constraint.reference.lon.toFixed(5)}, ${constraint.reference.lat.toFixed(5)}` : '保存后可使用“自动补全坐标”定位此参照。'}</p>
        </>}
      </div>
    })}
    <div className="flex flex-wrap gap-2">
      <button type="button" className={buttonClass} title={path.role === 'branch' ? '分叉支路从连接点出发，来向应设置在主路径或汇入路径。' : undefined} disabled={path.role === 'branch' || !firstNode || constraints.some((constraint) => constraint.kind === 'approach_from')} onClick={() => onChange({ spatial_constraints: [...constraints, { kind: 'approach_from', target_ref: firstNode, bearing: 'NW' }] })}>添加来向</button>
      <button type="button" className={buttonClass} disabled={!segments.length} onClick={() => onChange({ spatial_constraints: [...constraints, { kind: 'via_relative', after_ref: segments[0]?.after_ref, before_ref: segments[0]?.before_ref, occurrence: segments[0]?.occurrence, bearing: 'E', relation: 'within', reference: { name: '' } }] })}>添加经过方位</button>
    </div>
    {constraints.length > 0 && <details className="text-xs text-zinc-500">
      <summary className="cursor-pointer">制图尺寸（留空自动计算）</summary>
      <div className="mt-2 grid grid-cols-2 gap-3">
        {constraints.some((constraint) => constraint.kind === 'approach_from') && <Field title="来向箭头示意长度（公里）"><input className={fieldClass} type="number" min="0.001" step="any" value={path.control_options?.display_length_m === undefined ? '' : path.control_options.display_length_m / 1000} onChange={(event) => updateDistance('display_length_m', event.target.value)} /></Field>}
        {constraints.some((constraint) => constraint.kind === 'via_relative') && <Field title="经过方位偏移距离（公里）"><input className={fieldClass} type="number" min="0.001" step="any" value={path.control_options?.offset_m === undefined ? '' : path.control_options.offset_m / 1000} onChange={(event) => updateDistance('offset_m', event.target.value)} /></Field>}
      </div>
      <p className="mt-2">这些尺寸只影响示意形状，不代表历史行军距离。</p>
    </details>}
  </section>
}
