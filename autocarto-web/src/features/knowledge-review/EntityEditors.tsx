'use client'

import type { ReactNode } from 'react'
import { fieldClass, itemId, itemOptions, items, record, referencesTo, type GraphItem, type KnowledgeGraph, type ReviewTab } from './model'
import GeocodingDetails, { manualGeocodingPatch } from './GeocodingDetails'

export interface EditorProps {
  item: GraphItem; graph: KnowledgeGraph; onChange: (patch: Record<string, unknown>) => void; disabled?: boolean
}
export function Field({ title, children }: { title: string; children: ReactNode }) {
  return <label className="block text-sm text-zinc-700 dark:text-zinc-300">
    <span className="font-medium">
      {title}
    </span>
    {children}
  </label>
}
export function TextField({ title, value, onChange, multiline = false }: { title: string; value: unknown; onChange: (value: string) => void; multiline?: boolean }) {
  return <Field title={title}>
    {multiline ? <textarea
      className={fieldClass}
      rows={4}
      value={String(value || '')}
      onChange={(event) => onChange(event.target.value)}
    /> : <input
      className={fieldClass}
      value={String(value || '')}
      onChange={(event) => onChange(event.target.value)}
    />}
  </Field>
}
export function SelectField({ title, value, options, onChange }: { title: string; value: unknown; options: Array<[string, string]>; onChange: (value: string) => void }) {
  const current = String(value || '')
  return <Field title={title}>
    <select
      className={fieldClass}
      value={current}
      onChange={(event) => onChange(event.target.value)}
    >

      <option value="">请选择</option>

      {current && !options.some(([id]) => id === current) && <option value={current} title={current}>当前关联不可用，请重新选择</option>}

      {options.map(([id, text]) => <option
        key={id}
        value={id}
      >
        {text}
      </option>)}

    </select>
  </Field>
}
export function ActorsField({ title, value, graph, onChange }: { title: string; value: unknown; graph: KnowledgeGraph; onChange: (refs: string[]) => void }) {
  const actors = Array.isArray(value) ? value as string[] : []
  return <fieldset className="text-sm">
    <legend className="mb-2 font-medium">{title}</legend>
    <div className="flex flex-wrap gap-3">
      {itemOptions(graph, 'actors').map(([id, name]) => <label key={id} className="flex items-center gap-1.5">
        <input type="checkbox" checked={actors.includes(id)} onChange={(event) => onChange(event.target.checked ? [...actors, id] : actors.filter((ref) => ref !== id))} />{name}
      </label>)}
      {items(graph, 'actors').length === 0 && <span className="text-zinc-500">暂无部队，可在部队页新增。</span>}
    </div>
  </fieldset>
}
export default function EntityEditors({ item, graph, onChange, tab, disabled }: EditorProps & { tab: ReviewTab }) {
  const related = referencesTo(graph, tab, itemId(item, tab))
  const located = typeof item.lon === 'number' && typeof item.lat === 'number'
  return <fieldset
    disabled={disabled}
    className="space-y-4"
  >

    <TextField
      title={tab === 'actors' ? '部队名称' : tab === 'events' ? '事件名称' : '地点名称'}
      value={item.name}
      onChange={(name) => onChange(tab === 'spatial_entities' ? { name, ...manualGeocodingPatch(item, '地点名称已人工修改，查询结果仅供参考。'), ...(located ? { coordinate_source: item.entity_kind === 'cartographic_control' ? 'derived_control' : 'manual', coordinate_confidence: undefined } : {}) } : { name })}
    />

    {tab === 'actors' && <>

      <TextField
        title="别名（每行一个）"
        value={Array.isArray(item.aliases) ? item.aliases.join('\n') : ''}
        onChange={(value) => onChange({ aliases: value.split('\n') })}
        multiline
      />


    </>}

    {tab === 'events' && <>

      <div className="grid grid-cols-2 gap-4">

        <SelectField
          title="事件类型"
          value={item.event_type}
          onChange={(event_type) => onChange({ event_type })}
          options={[
            ['departure', '出发'], ['arrival', '到达'], ['passage', '经过'], ['battle', '战斗'], ['meeting', '会议'], ['rendezvous', '会合'], ['base', '驻地'], ['turn', '转向'], ['other', '其他'],
          ]}
        />

        <TextField
          title="时间（原文或人工补充）"
          value={item.time_text}
          onChange={(time_text) => onChange({ time_text })}
        />

      </div>

      <SelectField
        title="事件地点"
        value={item.location_ref}
        onChange={(location_ref) => onChange({ location_ref })}
        options={itemOptions(graph, 'spatial_entities').filter(([id]) => items(graph, 'spatial_entities').find((place) => itemId(place, 'spatial_entities') === id)?.entity_kind !== 'cartographic_control')}
      />

      <ActorsField title="参与部队" value={item.actor_refs} graph={graph} onChange={(actor_refs) => onChange({ actor_refs })} />

      <TextField
        title="事件描述"
        value={item.description}
        onChange={(description) => onChange({ description })}
        multiline
      />

    </>}

    {tab === 'spatial_entities' && <>

      <div className="grid grid-cols-2 gap-4">

        <SelectField
          title="空间对象类型"
          value={item.entity_kind}
          onChange={(entity_kind) => onChange({ entity_kind, factual: entity_kind !== 'cartographic_control', coordinate_source: located ? (entity_kind === 'cartographic_control' ? 'derived_control' : 'manual') : undefined, coordinate_confidence: undefined, ...manualGeocodingPatch(item, '地点类型已人工修改，查询结果仅供参考。'), map_role: entity_kind === 'cartographic_control' ? 'control' : 'background' })}
          options={item.entity_kind === 'cartographic_control' ? [['cartographic_control', '制图控制点']] : [['place', '地点'], ['region', '区域']]}
        />

        <SelectField
          title="地图用途"
          value={item.map_role}
          onChange={(map_role) => onChange({ map_role })}
          options={item.entity_kind === 'cartographic_control' ? [['control', '控制点']] : [['route_node', '路线节点'], ['event_anchor', '事件锚点'], ['background', '背景地点']]}
        />

      </div>

      <div className="grid grid-cols-2 gap-4">
        {(['lon', 'lat'] as const).map((field) => <Field
          key={field}
          title={field === 'lon' ? '经度（−180 至 180）' : '纬度（−90 至 90）'}
        >
          <input
            className={fieldClass}
            type="number"
            step="any"
            min={field === 'lon' ? -180 : -90}
            max={field === 'lon' ? 180 : 90}
            value={typeof item[field] === 'number' || typeof item[field] === 'string' ? String(item[field]) : ''}
            onChange={(event) => onChange({ [field]: event.target.value === '' ? undefined : Number(event.target.value), coordinate_source: event.target.value !== '' || typeof item[field === 'lon' ? 'lat' : 'lon'] === 'number' ? (item.entity_kind === 'cartographic_control' ? 'derived_control' : 'manual') : undefined, coordinate_confidence: undefined, ...manualGeocodingPatch(item) })}
          />
        </Field>)}
      </div>

      <p className="text-xs text-zinc-500">
        {located ? `坐标来源：${({ source_text: '原文提供', manual: '人工录入', gazetteer: record(item.geocoding).provider === 'tianditu' ? '天地图' : '地名资料', llm_estimated: '模型预估', derived_control: '制图派生控制点' } as Record<string, string>)[String(item.coordinate_source || '')] || '未记录'}` : '尚无坐标，可使用“自动补全坐标”。'}
      </p>

      <GeocodingDetails item={item} />

      {item.entity_kind === 'cartographic_control' && <TextField
        title="制图控制点生成/调整原因"
        value={item.generation_reason}
        onChange={(generation_reason) => onChange({ generation_reason })}
        multiline
      />}

      <TextField
        title="地点说明"
        value={item.description}
        onChange={(description) => onChange({ description })}
        multiline
      />

      <p className="text-xs text-zinc-500">
        {item.entity_kind === 'cartographic_control' ? '制图控制点不是历史事实地点，保留 derived_control 来源及调整原因。' : '坐标自动回填；手动修改后标记为人工录入，并随草稿保存。'}
      </p>

    </>}

    {related.length > 0 && <section className="rounded bg-zinc-50 p-3 text-xs dark:bg-zinc-900">
      <h4 className="mb-1 font-medium">关联内容（按真实引用计算）</h4>
      <ul className="list-inside list-disc space-y-1">
        {related.map((text, index) => <li key={`${text}-${index}`}>
          {text}
        </li>)}
      </ul>
    </section>}

  </fieldset>
}
