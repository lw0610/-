import { act, cleanup, fireEvent, render, renderHook, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/lib/api'
import EntityEditors from './EntityEditors'
import KnowledgeReviewCard from './KnowledgeReviewCard'
import KnowledgeReviewDialog from './KnowledgeReviewDialog'
import { reviewApi } from './api'
import { useKnowledgeReview } from './useKnowledgeReview'
import { extractionPhaseSummary, type ExtractionCompletion, type KnowledgeGraph, type ReviewDocument, type ReviewReference } from './model'

vi.mock('./api', () => ({ reviewApi: { get: vi.fn(), save: vi.fn(), geocode: vi.fn(), confirm: vi.fn() } }))
vi.mock('@/stores/sessionStore', () => ({ useSessionStore: (select: (value: { activeSessionId: string }) => unknown) => select({ activeSessionId: 'session-1' }) }))
vi.mock('@/stores/chatStore', () => ({ useChatStore: Object.assign((select: (state: { sessionRuntimeStates: Record<string, { status: string }> }) => unknown) => select({ sessionRuntimeStates: {} }), { getState: () => ({ resolveSessionInterrupt: vi.fn() }) }) }))

const reference: ReviewReference = { session_id: 'session-1', review_id: 'review-1', checkpoint_id: 'checkpoint-1' }
const candidate = { name: '庾家河镇', address: '陕西省商洛市丹凤县', lon: 110.45, lat: 33.8, source_id: 'poi-1' }
function makeDocument(): ReviewDocument {
  const graph: KnowledgeGraph = {
    actors: [], events: [], routes: [], callouts: [],
    spatial_entities: [{ entity_id: 'place-1', name: '庾家河', entity_kind: 'place', factual: true, map_role: 'route_node' }],
  }
  return { ...reference, revision: 3, status: 'awaiting_review', graph, source_text: '红军到达庾家河。', errors: [] }
}
function enrichedDocument(): ReviewDocument {
  const document = makeDocument()
  return {
    ...document, revision: 4,
    graph: { ...document.graph, spatial_entities: [{
      ...document.graph.spatial_entities[0], lon: candidate.lon, lat: candidate.lat, coordinate_source: 'gazetteer',
      geocoding: { status: 'resolved', provider: 'tianditu', original_name: '庾家河', modern_name: '庾家河镇', query: '丹凤县 庾家河镇', administrative_hint: '陕西省商洛市丹凤县', selected: candidate, candidates: [candidate] },
    }] },
  }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(reviewApi.get).mockResolvedValue(makeDocument())
  vi.mocked(reviewApi.geocode).mockResolvedValue(enrichedDocument())
})
afterEach(cleanup)

describe('empty knowledge results', () => {
  it('shows a newer compilation failure instead of cached compiling state', () => {
    render(<KnowledgeReviewCard reference={{ ...reference, revision: 3, status: 'compile_failed', updated_at: '2026-10-29T01:00:02Z', summary: { routes: 1 }, errors: [{ message: 'route_style is required' }] }} summary={{ revision: 3, status: 'compiling', updated_at: '2026-10-29T01:00:01Z' }} onOpen={vi.fn()} onConfirm={vi.fn()} />)
    expect(screen.getByRole('alert')).toHaveTextContent('route_style is required')
    expect(screen.queryByText('正在制图')).not.toBeInTheDocument()
  })

  it('shows the missing-source reason and prevents drawing an empty result', async () => {
    const counts = { routes: 0, events: 0, spatial_entities: 0, actors: 0, callouts: 0 }
    const reason = '未提取到可制图的内容。原因：原文未提供任何行军路线、地点或主体信息。'
    const card = render(<KnowledgeReviewCard reference={{ ...reference, status: 'awaiting_review', summary: counts, can_start: false, description: reason }} onOpen={vi.fn()} onConfirm={vi.fn()} />)
    expect(screen.getByText(reason)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '开始制图' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '查看并修改' })).toBeEnabled()
    // A saved edit containing a real place becomes usable; routes need not be
    // nonempty, and stale initial counts cannot keep the action disabled.
    card.rerender(<KnowledgeReviewCard reference={{ ...reference, status: 'awaiting_review', summary: counts, can_start: false }} summary={{ revision: 4, status: 'awaiting_review', counts: { ...counts, spatial_entities: 1 } }} onOpen={vi.fn()} onConfirm={vi.fn()} />)
    expect(screen.getByRole('button', { name: '开始制图' })).toBeEnabled()
    card.unmount()

    vi.mocked(reviewApi.get).mockResolvedValue({ ...makeDocument(), graph: { routes: [], events: [], spatial_entities: [], actors: [], callouts: [] }, errors: [{ code: 'empty_extraction', message: reason }] })
    const { result } = renderHook(() => useKnowledgeReview())
    await act(async () => { await result.current.show(reference, true) })
    render(<KnowledgeReviewDialog controller={result.current} />)
    expect(screen.getByText(new RegExp('原文未提供任何行军路线'))).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '开始制图' })).toBeDisabled()
    await act(async () => { await result.current.confirm() })
    expect(reviewApi.confirm).not.toHaveBeenCalled()
  })
})

describe('knowledge review coordinate enrichment', () => {
  it('applies the saved coordinate graph and revision to the extraction result being displayed', async () => {
    const { result } = renderHook(() => useKnowledgeReview())
    await act(async () => { await result.current.show(reference) })
    await act(async () => { await result.current.geocode() })
    expect(reviewApi.geocode).toHaveBeenCalledWith(reference, 3)
    expect(result.current.document?.revision).toBe(4)
    expect(result.current.draft?.spatial_entities[0]).toMatchObject({ name: '庾家河', lon: 110.45, lat: 33.8, coordinate_source: 'gazetteer' })
    expect(result.current.draft).toEqual(result.current.document?.graph)
    expect(result.current.dirty).toBe(false)
    expect(result.current.summaries['session-1/review-1'].revision).toBe(4)
    expect(result.current.notice).toBe('地点坐标已自动补全并保存到知识草稿（版本 4）。')
  })

  it('applies model estimates directly to the same coordinate fields without a selection step', async () => {
    const document = enrichedDocument()
    const estimated = { ...document, graph: { ...document.graph, spatial_entities: [{ ...document.graph.spatial_entities[0], coordinate_source: 'llm_estimated', geocoding: { status: 'estimated', provider: 'llm', original_name: '庾家河' } }] } }
    vi.mocked(reviewApi.geocode).mockResolvedValue(estimated)
    const { result } = renderHook(() => useKnowledgeReview())
    await act(async () => { await result.current.show(reference) })
    await act(async () => { await result.current.geocode() })
    expect(result.current.draft?.spatial_entities[0]).toMatchObject({ name: '庾家河', lon: 110.45, lat: 33.8, coordinate_source: 'llm_estimated' })
    expect(result.current.dirty).toBe(false)
    expect(result.current.notice).toBe('地点坐标已自动补全并保存到知识草稿（版本 4）。')
  })

  it('preserves unsaved local edits instead of querying an older revision', async () => {
    const { result } = renderHook(() => useKnowledgeReview())
    await act(async () => { await result.current.show(reference) })
    act(() => result.current.setDraft((graph) => graph ? { ...graph, spatial_entities: [{ ...graph.spatial_entities[0], description: '尚未保存的说明' }] } : graph))
    await act(async () => { await result.current.geocode() })
    expect(reviewApi.geocode).not.toHaveBeenCalled()
    expect(result.current.draft?.spatial_entities[0].description).toBe('尚未保存的说明')
    expect(result.current.notice).toContain('请先保存修改')
    expect(result.current.dirty).toBe(true)
  })

  it('blocks concurrent edits, closure, save and confirmation until lookup completes', async () => {
    const pending = deferred<ReviewDocument>()
    vi.mocked(reviewApi.geocode).mockReturnValue(pending.promise)
    const { result } = renderHook(() => useKnowledgeReview())
    await act(async () => { await result.current.show(reference) })
    let lookup!: Promise<void>
    act(() => { lookup = result.current.geocode() })
    expect(result.current.geocoding).toBe(true)
    act(() => { result.current.setDraft(null); result.current.close() })
    await act(async () => { await result.current.save(); await result.current.confirm(); await result.current.geocode() })
    expect(result.current.open).toBe(true)
    expect(result.current.draft).not.toBeNull()
    expect(reviewApi.geocode).toHaveBeenCalledTimes(1)
    expect(reviewApi.save).not.toHaveBeenCalled()
    expect(reviewApi.confirm).not.toHaveBeenCalled()
    await act(async () => { pending.resolve(enrichedDocument()); await lookup })
    expect(result.current.geocoding).toBe(false)
    expect(result.current.document?.revision).toBe(4)
  })

  it('keeps the current graph on a revision conflict and releases the mutation guard', async () => {
    vi.mocked(reviewApi.geocode).mockRejectedValue(new ApiError('版本不一致', 409))
    const { result } = renderHook(() => useKnowledgeReview())
    await act(async () => { await result.current.show(reference); })
    const graphBefore = result.current.draft
    await act(async () => { await result.current.geocode() })
    expect(result.current.error).toContain('草稿版本已变化')
    expect(result.current.draft).toEqual(graphBefore)
    expect(result.current.document?.revision).toBe(3)
    expect(result.current.geocoding).toBe(false)
    act(() => result.current.close())
    expect(result.current.open).toBe(false)
  })

  it('shows the lookup action and disables it while unsaved edits exist', async () => {
    const { result } = renderHook(() => useKnowledgeReview())
    await act(async () => { await result.current.show(reference) })
    let view!: ReturnType<typeof render>
    await act(async () => { view = render(<KnowledgeReviewDialog controller={result.current} />) })
    expect(screen.getByRole('button', { name: '自动补全坐标' })).toBeEnabled()
    expect(screen.queryByRole('region', { name: '未定位地点' })).not.toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: '地点坐标补全' })).queryByText(/待核对|须补齐坐标/)).not.toBeInTheDocument()
    act(() => result.current.setDraft((graph) => graph ? { ...graph, annotation: 'changed' } : graph))
    await act(async () => { view.rerender(<KnowledgeReviewDialog controller={result.current} />) })
    expect(screen.getByRole('button', { name: '自动补全坐标' })).toBeDisabled()
    expect(screen.getByText('请先保存当前修改，再补全坐标。')).toBeInTheDocument()
  })
})

describe('extraction results without item-by-item review', () => {
  it.each<ExtractionCompletion>([
    { version: 1, extraction_complete: true, boundary_complete: false, route_pass_complete: false, adaptation_complete: true },
    { version: 2, extraction_complete: true, boundary_complete: false, route_pass_complete: false, adaptation_complete: true, semantic_complete: false },
    { version: 3, extraction_complete: true, boundary_complete: false, route_pass_complete: false, adaptation_complete: true, semantic_complete: false, semantic_skipped: true },
  ])('does not present optional legacy review phases as unfinished for completion version $version', (completion) => {
    expect(extractionPhaseSummary(completion)).toBe('全文抽取已完成 · 制图转换已完成')
  })

  function legacyDocument(): ReviewDocument {
    const document = enrichedDocument()
    return {
      ...document,
      pending_issues: [{ id: 'pending-1', kind: 'route', phase: 'semantic_check', message: '旧路线关系待核对', subject_refs: ['place-1'] }],
      resolutions: [],
      extraction_completion: { version: 1, extraction_complete: true, boundary_complete: true, route_pass_complete: true, adaptation_complete: true, semantic_complete: false },
      errors: [
        { code: 'semantic_review_pending', path: 'pending_issues.pending-1', message: '请逐项核对并保存处理说明' },
        { code: 'invalid_reference', path: 'routes', message: '路线节点引用不存在' },
      ],
    }
  }

  it('omits old pending review presentation while preserving editable facts, sources and real data errors', async () => {
    const document = legacyDocument()
    const card = render(<KnowledgeReviewCard reference={{ ...reference, status: 'awaiting_review', summary: { spatial_entities: 1, pending_issues: 7 } }} summary={{ status: 'awaiting_review', revision: 4, pending_count: 7 }} onOpen={vi.fn()} onConfirm={vi.fn()} />)
    expect(screen.getByText('可编辑')).toBeInTheDocument()
    expect(screen.queryByText(/待核对|待处理|核对原文/)).not.toBeInTheDocument()
    card.rerender(<KnowledgeReviewCard reference={{ ...reference, status: 'awaiting_review', summary: '旧结果有七项待核对内容' }} onOpen={vi.fn()} onConfirm={vi.fn()} />)
    expect(screen.queryByText(/待核对/)).not.toBeInTheDocument()
    card.unmount()

    vi.mocked(reviewApi.get).mockResolvedValue(document)
    const { result } = renderHook(() => useKnowledgeReview())
    await act(async () => { await result.current.show(reference) })
    await act(async () => { render(<KnowledgeReviewDialog controller={result.current} />) })
    expect(screen.getByRole('heading', { name: '知识抽取结果' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: '抽取待核对问题' })).not.toBeInTheDocument()
    expect(screen.queryByText(/待核对|语义核对|核对原文|处理说明/)).not.toBeInTheDocument()
    expect(screen.getByText(/路线节点引用不存在/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '地点 (1)' }))
    expect(screen.getByRole('spinbutton', { name: /经度/ })).toHaveValue(110.45)
    expect(screen.getByText('坐标来源：天地图')).toBeInTheDocument()
    expect(result.current.document?.pending_issues).toEqual(document.pending_issues)
    expect(result.current.document?.resolutions).toEqual([])
    expect(reviewApi.save).not.toHaveBeenCalled()
    expect(reviewApi.confirm).not.toHaveBeenCalled()
  })

  it('lets the backend decide compilation eligibility and reports a neutral rejection without fabricating resolutions', async () => {
    const document = legacyDocument()
    vi.mocked(reviewApi.get).mockResolvedValue(document)
    vi.mocked(reviewApi.confirm).mockRejectedValue(new ApiError('请逐项核对后再确认制图', 422, 'KNOWLEDGE_REVIEW_INVALID', { errors: document.errors }))
    const { result } = renderHook(() => useKnowledgeReview())
    await act(async () => { await result.current.show(reference, true) })
    let view!: ReturnType<typeof render>
    await act(async () => { view = render(<KnowledgeReviewDialog controller={result.current} />) })
    expect(screen.getByRole('button', { name: '开始制图' })).toBeEnabled()
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: '开始制图' })) })
    await act(async () => { view.rerender(<KnowledgeReviewDialog controller={result.current} />) })
    expect(reviewApi.confirm).toHaveBeenCalledWith(reference, 4, expect.any(String))
    expect(screen.getByRole('alert')).toHaveTextContent('路线节点引用不存在')
    expect(screen.queryByText(/逐项核对|待核对|处理说明/)).not.toBeInTheDocument()
    expect(result.current.document?.pending_issues).toEqual(document.pending_issues)
    expect(result.current.document?.resolutions).toEqual([])
    expect(result.current.document?.status).toBe('awaiting_review')
    expect(result.current.confirmOpen).toBe(true)
    expect(reviewApi.save).not.toHaveBeenCalled()
  })
})

describe('place coordinate editor', () => {
  it('shows returned coordinates in the existing longitude and latitude fields', () => {
    const document = enrichedDocument()
    render(<EntityEditors item={document.graph.spatial_entities[0]} graph={document.graph} tab="spatial_entities" onChange={vi.fn()} />)
    expect(screen.getByRole('spinbutton', { name: /经度/ })).toHaveValue(110.45)
    expect(screen.getByRole('spinbutton', { name: /纬度/ })).toHaveValue(33.8)
    expect(screen.getByRole('textbox', { name: '地点名称' })).toHaveValue('庾家河')
    expect(screen.getByText('坐标来源：天地图')).toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: '地点查询结果' })).getByText('自动匹配', { exact: false })).toBeInTheDocument()
  })

  it('displays automatically selected coordinates with read-only lookup records', () => {
    const document = enrichedDocument()
    const item = document.graph.spatial_entities[0]
    const onChange = vi.fn()
    render(<EntityEditors item={item} graph={document.graph} tab="spatial_entities" onChange={onChange} />)
    expect(screen.getByRole('spinbutton', { name: /经度/ })).toHaveValue(110.45)
    expect(screen.getByRole('spinbutton', { name: /纬度/ })).toHaveValue(33.8)
    expect(onChange).not.toHaveBeenCalled()
    expect(screen.getByRole('textbox', { name: '地点名称' })).toHaveValue('庾家河')
    const details = screen.getByRole('region', { name: '地点查询结果' })
    expect(within(details).queryByRole('button')).not.toBeInTheDocument()
    expect(within(details).getByText('查询记录（1）').closest('details')).not.toHaveAttribute('open')
  })

  it('shows model coordinates with a neutral source label and omits old coordinate approval prose', () => {
    const document = enrichedDocument()
    const item = { ...document.graph.spatial_entities[0], coordinate_source: 'llm_estimated', geocoding: { status: 'estimated', provider: 'llm', reason: '请人工核对，等待确认后补齐坐标。' } }
    render(<EntityEditors item={item} graph={document.graph} tab="spatial_entities" onChange={vi.fn()} />)
    expect(screen.getByRole('spinbutton', { name: /经度/ })).toHaveValue(110.45)
    expect(screen.getByText('坐标来源：模型预估')).toBeInTheDocument()
    expect(screen.queryByText(/需核实|人工核对|等待确认|补齐坐标/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('marks edited coordinates as manual and invalidates a previous verified selection', () => {
    const document = enrichedDocument(), onChange = vi.fn()
    render(<EntityEditors item={document.graph.spatial_entities[0]} graph={document.graph} tab="spatial_entities" onChange={onChange} />)
    fireEvent.change(screen.getByRole('spinbutton', { name: /经度/ }), { target: { value: '111.2' } })
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ lon: 111.2, coordinate_source: 'manual', coordinate_confidence: undefined, geocoding: expect.objectContaining({ status: 'overridden', selected: undefined }) }))
  })

  it('does not display stale review prompts from an older coordinate result', () => {
    const document = enrichedDocument()
    const item = { ...document.graph.spatial_entities[0], geocoding: { status: 'needs_review', reason: '需要核对历史地名', candidates: [{ ...candidate, lat: 95 }] } }
    render(<EntityEditors item={item} graph={document.graph} tab="spatial_entities" onChange={vi.fn()} />)
    expect(screen.queryByText(/需要核对|待核对|无法选用/)).not.toBeInTheDocument()
    expect(screen.queryByText(/查询记录/)).not.toBeInTheDocument()
    expect(screen.getByRole('spinbutton', { name: /经度/ })).toHaveValue(110.45)
  })
})
