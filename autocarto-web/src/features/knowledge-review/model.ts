import type { InterruptRequestData, WorkflowBlock, WorkflowEntry } from '@/types/message'

export type ReviewTab = 'routes' | 'events' | 'spatial_entities' | 'actors' | 'callouts'
export type GraphItem = Record<string, unknown> & { name?: string; evidence_refs?: string[] }
export type RouteNode = { ref_type: 'spatial_entity' | 'event'; ref_id: string }
export type CompassBearing = 'N' | 'NE' | 'E' | 'SE' | 'S' | 'SW' | 'W' | 'NW'
export type SpatialConstraint = Record<string, unknown> & {
  kind: 'approach_from' | 'via_relative'; bearing: CompassBearing
  target_ref?: string; after_ref?: string; before_ref?: string; occurrence?: number; relation?: 'within' | 'outside'
  reference?: GraphItem
}
export type RoutePath = Record<string, unknown> & {
  path_id: string; role: string; direction: string; nodes: RouteNode[]; attach_ref?: string; actor_refs?: string[]
  spatial_constraints?: SpatialConstraint[]; control_options?: { display_length_m?: number; offset_m?: number }
}
export type KnowledgeGraph = Record<string, unknown> & {
  actors: GraphItem[]; spatial_entities: GraphItem[]; events: GraphItem[]
  routes: GraphItem[]; callouts: GraphItem[]; evidence?: GraphItem[]
}
export interface ReviewIssue { path?: string; code?: string; message: string }
export function isPendingReviewDiagnostic(issue: ReviewIssue): boolean {
  return issue.code === 'semantic_review_pending' || /^(?:\$\.|graph\.)?pending_issues(?:[.\[]|$)/.test(issue.path || '')
}
export function visibleReviewIssues(issues: ReviewIssue[] = []): ReviewIssue[] {
  return issues.filter((issue) => !isPendingReviewDiagnostic(issue))
}
export interface ExtractionCompletion {
  version: number; extraction_complete: boolean; boundary_complete: boolean
  route_pass_complete: boolean; adaptation_complete: boolean; semantic_complete?: boolean; semantic_skipped?: boolean
}
export interface ReviewReference {
  session_id: string; review_id: string; checkpoint_id: string
  revision?: number; status?: string; summary?: unknown; description?: string; can_start?: boolean; updated_at?: string; errors?: ReviewIssue[]
}
export interface ReviewDocument extends ReviewReference {
  revision: number; status: string; graph: KnowledgeGraph; source_text: string
  errors?: ReviewIssue[]; approved_revision?: number; [key: string]: unknown
  extraction_status?: string
  extraction_completion?: ExtractionCompletion
}
export type ReviewSummary = Pick<ReviewDocument, 'revision' | 'status' | 'updated_at' | 'errors'> & { pending_count?: number; counts?: Partial<Record<ReviewTab, number>>; empty_reason?: string }
export const tabs: Array<{ key: ReviewTab; label: string }> = [
  { key: 'routes', label: '路线' }, { key: 'events', label: '事件' },
  { key: 'spatial_entities', label: '地点' }, { key: 'actors', label: '部队' },
  { key: 'callouts', label: '图文注记' },
]
export type ReviewGroup = 'content' | 'presentation'
export const reviewGroups: Array<{ key: ReviewGroup; label: string }> = [
  { key: 'content', label: '行军内容' },
  { key: 'presentation', label: '制图表达' },
]
export const tabsByGroup: Record<ReviewGroup, ReviewTab[]> = {
  content: ['routes', 'events', 'spatial_entities', 'actors'],
  presentation: ['callouts'],
}
export function groupForTab(tab: ReviewTab): ReviewGroup {
  return tabsByGroup.content.includes(tab) ? 'content' : 'presentation'
}
export const idFields: Record<ReviewTab, string> = {
  routes: 'route_id', events: 'event_id', spatial_entities: 'entity_id', actors: 'actor_id', callouts: 'callout_id',
}
export const fieldClass = 'mt-1 w-full rounded border border-zinc-300 bg-white px-2 py-1.5 text-sm text-zinc-900 disabled:opacity-60 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100'
export const buttonClass = 'rounded border border-zinc-300 px-2.5 py-1.5 text-sm hover:bg-zinc-100 disabled:opacity-50 dark:border-zinc-700 dark:hover:bg-zinc-800'
export function record(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
}
export function items(graph: KnowledgeGraph, tab: ReviewTab): GraphItem[] {
  return Array.isArray(graph[tab]) ? graph[tab] : []
}
// Display filtering must not remove controls from the graph's route reference lookup.
export function visibleItems(graph: KnowledgeGraph, tab: ReviewTab): GraphItem[] {
  return items(graph, tab).filter((item) => tab !== 'spatial_entities' || item.entity_kind !== 'cartographic_control')
}
export function reviewCounts(graph: KnowledgeGraph): Partial<Record<ReviewTab, number>> {
  return Object.fromEntries(tabs.map(({ key }) => [key, visibleItems(graph, key).length]))
}
export function hasExtractedContent(graph: KnowledgeGraph): boolean {
  return tabs.some(({ key }) => visibleItems(graph, key).length > 0)
}
export function routeReferences(graph: KnowledgeGraph): GraphItem[] {
  return items(graph, 'routes').flatMap((route) => (Array.isArray(route.paths) ? route.paths as RoutePath[] : [])
    .flatMap((path) => (path.spatial_constraints || []).flatMap((constraint) => constraint.reference?.name ? [constraint.reference] : [])))
}
export function unlocatedReferences(graph: KnowledgeGraph): GraphItem[] {
  return routeReferences(graph).filter((reference) => !hasCoordinates(reference))
}
export function hasCoordinates(item: GraphItem): boolean {
  return typeof item.lon === 'number' && Number.isFinite(item.lon) && Math.abs(item.lon) <= 180
    && typeof item.lat === 'number' && Number.isFinite(item.lat) && Math.abs(item.lat) <= 90
}
export function itemId(item: GraphItem, tab: ReviewTab) { return String(item[idFields[tab]] || '') }
export function extractionPhaseSummary(completion?: ExtractionCompletion): string {
  if (!completion) return ''
  return [
    completion.extraction_complete ? '全文抽取已完成' : '全文抽取未完成',
    completion.adaptation_complete ? '制图转换已完成' : '制图转换待完成',
  ].join(' · ')
}
export function label(item: GraphItem, graph?: KnowledgeGraph | null): string {
  if (item.callout_id && graph) {
    const anchors = items(graph, item.anchor_ref_type === 'event' ? 'events' : 'spatial_entities')
    const anchor = anchors.find((entry) => (entry.event_id || entry.entity_id) === item.anchor_ref)
    return anchor ? String(anchor.name || '图文注记') : '未关联地点的注记'
  }
  return String(item.name || item.title || '未命名')
}
export function itemOptions(graph: KnowledgeGraph, tab: ReviewTab): Array<[string, string]> {
  const list = items(graph, tab)
  const seen = new Map<string, number>()
  return list.map((item) => {
    const name = label(item, graph), ordinal = (seen.get(name) || 0) + 1
    seen.set(name, ordinal)
    return [itemId(item, tab), list.filter((entry) => label(entry, graph) === name).length > 1 ? `${name}（${ordinal}）` : name]
  })
}
export function issueLabel(issue: ReviewIssue, graph?: KnowledgeGraph | null): string {
  if (!issue.path) return ''
  const clean = issue.path.replace(/^\$\.?|^graph\./, '')
  const match = clean.match(/^(\w+)(?:\[(\d+)\])?(.*)$/)
  if (!match) return '结果内容'
  const tab = tabs.find((entry) => entry.key === match[1])
  const entry = tab && graph && match[2] !== undefined ? items(graph, tab.key)[Number(match[2])] : null
  const parts = [tab ? `${tab.label}${entry ? `「${label(entry, graph)}」` : ''}` : '结果内容']
  const labels: Record<string, string> = {
    name: '名称', aliases: '别名', location_ref: '发生地点', actor_refs: '参与部队', time_text: '时间',
    lon: '经度', lat: '纬度', map_role: '地图用途', entity_kind: '地点类型', coordinate_source: '坐标来源',
    topology: '路线类型', side: '敌我属性', attach_ref: '连接点', direction: '方向', role: '路径用途',
    title: '标题', body: '正文', anchor_ref: '关联锚点', template: '排版模板', image_path: '图片', width_mm: '注记宽度',
    evidence_refs: '原文依据', confidence: '信息来源', coordinate_confidence: '坐标来源', generation_reason: '控制点调整原因', spatial_constraints: '路线空间约束', reference: '定位参照', bearing: '方位', relation: '相对位置', target_ref: '目标地点', after_ref: '起始节点', before_ref: '结束节点', control_options: '制图参数', display_length_m: '示意长度', offset_m: '偏移距离', occurrence: '同一路段出现次数',
  }
  for (const pathPart of match[3].split('.').filter(Boolean)) {
    const indexed = pathPart.match(/^(paths|nodes|spatial_constraints)\[(\d+)\]/)
    if (indexed) parts.push(`${indexed[1] === 'paths' ? '路径' : indexed[1] === 'nodes' ? '节点' : '空间约束'} ${Number(indexed[2]) + 1}`)
    else if (labels[pathPart]) parts.push(labels[pathPart])
  }
  return parts.join(' · ')
}
export function reviewKey(ref: ReviewReference) { return `${ref.session_id}/${ref.review_id}` }
export function editable(status: string) { return status === 'awaiting_review' || status === 'compile_failed' }
export function statusLabel(status?: string) {
  return ({ awaiting_review: '可编辑', compiling: '正在生成制图数据', compiled: '制图数据已生成', compile_failed: '制图数据生成失败' } as Record<string, string>)[status || ''] || '读取状态中'
}
export function parseReviewReference(interrupt?: InterruptRequestData | null, sessionId = ''): ReviewReference | null {
  for (const context of interrupt?.interrupt_contexts || []) {
    let info = context.info
    if (typeof info === 'string') { try { info = JSON.parse(info) } catch { continue } }
    const payload = record(info)
    if (payload.type !== 'knowledge_review' || typeof payload.review_id !== 'string' || !payload.review_id.trim()) continue
    return {
      session_id: String(payload.session_id || sessionId), review_id: payload.review_id,
      checkpoint_id: String(interrupt?.checkpoint_id || ''),
      revision: typeof payload.revision === 'number' ? payload.revision : undefined,
      status: typeof payload.status === 'string' ? payload.status : undefined, summary: payload.summary,
      description: typeof payload.description === 'string' ? payload.description : undefined,
      can_start: typeof payload.can_start === 'boolean' ? payload.can_start : undefined,
      updated_at: typeof payload.updated_at === 'string' ? payload.updated_at : undefined,
      errors: Array.isArray(payload.errors) ? payload.errors as ReviewIssue[] : undefined,
    }
  }
  return null
}

export function reviewProgressSummary(ref: ReviewReference): string {
  if (ref.status === 'compile_failed') return `制图数据生成失败：${ref.errors?.[0]?.message || ref.description || '请查看失败详情。'}`
  if (ref.status === 'compiling') return '正在将已确认的知识结果转换为制图数据。'
  if (ref.status === 'compiled') return '制图数据已生成，继续执行后续制图。'
  return '知识抽取结果已保存，等待确认后继续制图。'
}

export function updateReviewProcess(blocks: WorkflowBlock[], ref: ReviewReference, activeId: string | null, timestamp: number, agent = 'orchestrator') {
  let index = blocks.findIndex(block => block.id === activeId)
  if (index < 0 && blocks.at(-1)?.entries.some(entry => entry.phase === 'knowledge_review' && entry.data?.review_id === ref.review_id)) index = blocks.length - 1
  const existing = blocks[index]
  const status: WorkflowBlock['status'] = ref.status === 'compile_failed' ? 'error' : ref.status === 'compiling' ? 'running' : ref.status === 'compiled' ? 'completed' : 'waiting_user'
  const content = reviewProgressSummary(ref)
  const entry: WorkflowEntry = { id: `knowledge-review-${ref.review_id}`, type: 'status', phase: 'knowledge_review', content, timestamp, data: { ...ref } }
  const entries = existing?.entries || []
  const updated: WorkflowBlock = {
    ...(existing || { id: `review-process-${ref.review_id}-${timestamp}`, agent, agentDisplayName: agent === 'orchestrator' ? '主智能体' : agent, startedAt: timestamp }),
    status, summary: content, updatedAt: timestamp, finishedAt: status === 'error' || status === 'completed' ? timestamp : undefined,
    entries: entries.some(item => item.id === entry.id) ? entries.map(item => item.id === entry.id ? entry : item) : [...entries, entry],
  }
  return { blocks: index < 0 ? [...blocks, updated] : blocks.map((block, i) => i === index ? updated : block), activeId: updated.id }
}
export function updateItem(graph: KnowledgeGraph, tab: ReviewTab, id: string, patch: Record<string, unknown>): KnowledgeGraph {
  return { ...graph, [tab]: items(graph, tab).map((item) => itemId(item, tab) === id ? { ...item, ...patch, [idFields[tab]]: id } : item) }
}
export function referencesTo(graph: KnowledgeGraph, tab: ReviewTab, id: string): string[] {
  const references: string[] = []
  if (tab === 'actors') {
    items(graph, 'events').forEach((event) => { if (Array.isArray(event.actor_refs) && event.actor_refs.includes(id)) references.push(`事件「${label(event)}」`) })
    items(graph, 'routes').forEach((route) => {
      if ((Array.isArray(route.actor_refs) && route.actor_refs.includes(id)) || (Array.isArray(route.paths) && route.paths.some((path) => Array.isArray(record(path).actor_refs) && (record(path).actor_refs as string[]).includes(id)))) references.push(`路线「${label(route)}」`)
    })
  }
  if (tab === 'spatial_entities' || tab === 'events') {
    if (tab === 'spatial_entities') items(graph, 'events').forEach((event) => { if (event.location_ref === id) references.push(`事件「${label(event)}」`) })
    const refType = tab === 'events' ? 'event' : 'spatial_entity'
    items(graph, 'routes').forEach((route) => {
      const paths = Array.isArray(route.paths) ? route.paths as RoutePath[] : []
      if (paths.some((path) => (tab === 'spatial_entities' && path.attach_ref === id) || path.nodes?.some((node) => node.ref_type === refType && node.ref_id === id) || (tab === 'spatial_entities' && path.spatial_constraints?.some((constraint) => [constraint.target_ref, constraint.after_ref, constraint.before_ref].includes(id))))) references.push(`路线「${label(route)}」`)
    })
    for (const key of ['callouts', 'point_presentations']) {
      const list = Array.isArray(graph[key]) ? graph[key] as GraphItem[] : []
      list.forEach((item) => { if (item.anchor_ref_type === refType && item.anchor_ref === id) references.push(`${key === 'callouts' ? '注记' : '点符号'}「${label(item, graph)}」`) })
    }
  }
  return references
}
export function newId(prefix: string) { return `${prefix}_${globalThis.crypto.randomUUID()}` }
export function createItem(tab: ReviewTab): GraphItem {
  const id = newId(tab === 'spatial_entities' ? 'place' : tab.slice(0, -1))
  const common = { [idFields[tab]]: id, name: '', evidence_refs: [] }
  switch (tab) {
    case 'actors': return { ...common, aliases: [] }
    case 'events': return { ...common, event_type: 'other', location_ref: '', actor_refs: [], time_text: '', description: '' }
    case 'spatial_entities': return { ...common, entity_kind: 'place', map_role: 'background', factual: true }
    case 'routes': return { ...common, topology: 'standard', side: 'friendly', paths: [{ path_id: newId('path'), role: 'main', direction: 'forward', nodes: [], actor_refs: [] }] }
    case 'callouts': return { callout_id: id, anchor_ref_type: 'event', anchor_ref: '', title: '', body: '', template: 'text_only', image_enabled: false, image_path: '', leader_style: 'double', width_mm: 42, priority: 0, preferred_side: 'right_top' }
  }
}
export function validateGraph(graph: KnowledgeGraph): ReviewIssue[] {
  const issues: ReviewIssue[] = []
  for (const tab of tabs) {
    const seen = new Set<string>()
    items(graph, tab.key).forEach((item, index) => {
      const id = itemId(item, tab.key), path = `${tab.key}[${index}]`
      if (!id || seen.has(id)) issues.push({ path, message: '条目 ID 缺失或重复' })
      seen.add(id)
      if (tab.key !== 'callouts' && !String(item.name || '').trim()) issues.push({ path: `${path}.name`, message: '名称不能为空' })
      if (tab.key === 'spatial_entities') {
        for (const [field, bound] of [['lon', 180], ['lat', 90]] as const) {
          if (item[field] === undefined || item[field] === null || item[field] === '') continue
          if (typeof item[field] !== 'number' || !Number.isFinite(item[field]) || Math.abs(item[field] as number) > bound) issues.push({ path: `${path}.${field}`, message: `${field === 'lon' ? '经度' : '纬度'}必须是有效范围内的数字` })
        }
      }
    })
  }
  return issues
}
export function unlocatedPlaces(graph: KnowledgeGraph): GraphItem[] {
  return visibleItems(graph, 'spatial_entities').filter((place) => !hasCoordinates(place))
}
