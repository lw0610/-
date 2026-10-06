'use client'

import { record, type GraphItem } from './model'

export function manualGeocodingPatch(item: GraphItem, reason = '坐标已人工修改，查询结果仅供参考。'): Record<string, unknown> {
  const lookup = record(item.geocoding)
  return Object.keys(lookup).length ? { geocoding: { ...lookup, status: 'overridden', selected: undefined, selected_name: undefined, selected_address: undefined, reason } } : {}
}

function coordinates(candidate: Record<string, unknown>) {
  return typeof candidate.lon === 'number' && Number.isFinite(candidate.lon) && Math.abs(candidate.lon) <= 180
    && typeof candidate.lat === 'number' && Number.isFinite(candidate.lat) && Math.abs(candidate.lat) <= 90
}

export default function GeocodingDetails({ item }: { item: GraphItem }) {
  const lookup = record(item.geocoding)
  if (!Object.keys(lookup).length) return null
  const candidates = Array.isArray(lookup.candidates) ? lookup.candidates.map(record).filter(coordinates) : []
  const selected = record(lookup.selected)
  const currentStatus = String(lookup.status || '')
  const status = currentStatus === 'overridden' ? '人工修改' : item.coordinate_source === 'llm_estimated' || currentStatus === 'estimated' ? '模型预估' : currentStatus === 'resolved' ? '自动匹配' : ''
  const metadata = [
    ['原文地名', lookup.original_name || item.name],
    ['现代名称', lookup.modern_name],
    ['查询名称', lookup.query],
    ['行政区线索', lookup.administrative_hint],
    ['匹配地点', selected.name || lookup.selected_name],
    ['匹配地址', selected.address || lookup.selected_address],
    ['定位坐标', coordinates(item) ? `${String(item.lon)}, ${String(item.lat)}` : ''],
  ].filter(([, value]) => typeof value === 'string' && value.trim())
  return <section aria-label="地点查询结果" className="space-y-3 rounded border border-zinc-200 bg-zinc-50 p-3 text-xs dark:border-zinc-800 dark:bg-zinc-900">
    <h4 className="font-medium">定位信息{status ? ` · ${status}` : ''}</h4>
    <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1">
      {metadata.map(([title, value]) => <div key={String(title)} className="contents">
        <dt className="text-zinc-500">{String(title)}</dt><dd className="break-words">{String(value)}</dd>
      </div>)}
    </dl>
    {candidates.length > 0 && <details>
      <summary className="cursor-pointer font-medium">查询记录（{candidates.length}）</summary>
      <ul className="mt-2 space-y-2">
        {candidates.map((candidate, index) => <li key={`${String(candidate.source_id || candidate.name || '')}-${index}`} className="rounded border border-zinc-200 p-2 dark:border-zinc-700">
          <p className="font-medium">{String(candidate.name || '未命名候选')}</p>
          {typeof candidate.address === 'string' && candidate.address && <p className="mt-1 break-words text-zinc-500">{candidate.address}</p>}
          <p className="mt-1 tabular-nums">经度 {String(candidate.lon)} · 纬度 {String(candidate.lat)}</p>
        </li>)}
      </ul>
    </details>}
  </section>
}
