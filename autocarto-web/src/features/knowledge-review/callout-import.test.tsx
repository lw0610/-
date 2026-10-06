import { act, cleanup, fireEvent, render, renderHook, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/lib/api'
import { pickFilePath } from '@/lib/config-form-helpers'
import CalloutEditor from './CalloutEditor'
import KnowledgeReviewDialog from './KnowledgeReviewDialog'
import { reviewApi, type CalloutImportResult } from './api'
import { createItem, issueLabel, itemOptions, referencesTo, type KnowledgeGraph, type ReviewDocument, type ReviewReference } from './model'
import { useKnowledgeReview } from './useKnowledgeReview'

vi.mock('./api', () => ({
  reviewApi: { get: vi.fn(), save: vi.fn(), geocode: vi.fn(), confirm: vi.fn(), importCallouts: vi.fn(), importAsset: vi.fn() },
  assetPreviewURL: (path: string) => path,
}))
vi.mock('@/lib/config-form-helpers', () => ({ isDesktopRuntime: () => true, pickFilePath: vi.fn() }))
const sessionState = vi.hoisted(() => ({ activeSessionId: 'session-1' }))
vi.mock('@/stores/sessionStore', () => ({ useSessionStore: (select: (value: { activeSessionId: string }) => unknown) => select(sessionState) }))
vi.mock('@/stores/chatStore', () => ({ useChatStore: { getState: () => ({ resolveSessionInterrupt: vi.fn() }) } }))

const reference: ReviewReference = { session_id: 'session-1', review_id: 'review-1', checkpoint_id: 'checkpoint-1' }
const workbookPath = '/Users/zw/Documents/红二十五军图文标注.xlsx'
const graph: KnowledgeGraph = {
  actors: [], events: [], routes: [], callouts: [],
  spatial_entities: [{ entity_id: 'he', name: '何家冲', lon: 114.1, lat: 31.5 }],
}
function makeDocument(): ReviewDocument {
  return { ...reference, revision: 3, status: 'awaiting_review', source_text: '从何家冲出发。', graph: structuredClone(graph) }
}
function imported(): CalloutImportResult {
  return {
    review: { ...makeDocument(), revision: 4, graph: { ...structuredClone(graph), callouts: [{ callout_id: 'xlsx-1', anchor_ref_type: 'spatial_entity', anchor_ref: 'he', title: '', body: '从何家冲出发。', template: 'image_top_text_bottom', leader_style: 'double' }] } },
    imported_count: 29, updated_count: 0, image_count: 27, new_place_count: 12, warnings: ['板桥镇没有图片，已使用纯文字。'],
  }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}
beforeEach(() => {
  vi.clearAllMocks()
  sessionState.activeSessionId = 'session-1'
  vi.mocked(reviewApi.get).mockResolvedValue(makeDocument())
  vi.mocked(reviewApi.importCallouts).mockResolvedValue(imported())
  vi.mocked(pickFilePath).mockResolvedValue(workbookPath)
})
afterEach(cleanup)

describe('Excel callout import', () => {
  it('submits current unsaved edits with the revision, then displays the saved result and images', async () => {
    const { result } = renderHook(() => useKnowledgeReview())
    await act(async () => { await result.current.show(reference) })
    act(() => result.current.setDraft((current) => current ? { ...current, map_name: '我的修改' } : current))
    const draft = result.current.draft
    await act(async () => { await result.current.importCallouts() })
    expect(reviewApi.importCallouts).toHaveBeenCalledWith(reference, 3, workbookPath, draft, expect.any(AbortSignal))
    expect(result.current.document?.revision).toBe(4)
    expect(result.current.draft?.callouts).toHaveLength(1)
    expect(result.current.dirty).toBe(false)
    expect(result.current.notice).toContain('新增 29 条')
    expect(result.current.notice).toContain('27 张图片')
    expect(result.current.importWarnings).toEqual(imported().warnings)
    expect(reviewApi.confirm).not.toHaveBeenCalled()
  })

  it('locks mutations and closing through the file picker and import without a second approval', async () => {
    const picker = deferred<string | null>()
    const request = deferred<CalloutImportResult>()
    vi.mocked(pickFilePath).mockReturnValue(picker.promise)
    vi.mocked(reviewApi.importCallouts).mockReturnValue(request.promise)
    const { result } = renderHook(() => useKnowledgeReview())
    await act(async () => { await result.current.show(reference) })
    let pending!: Promise<void>
    act(() => { pending = result.current.importCallouts() })
    expect(result.current.importing).toBe(true)
    act(() => { result.current.setDraft(null); result.current.close() })
    expect(result.current.draft).not.toBeNull()
    expect(result.current.open).toBe(true)
    await act(async () => { picker.resolve(workbookPath) })
    await act(async () => { await result.current.save(); await result.current.confirm(); await result.current.importCallouts() })
    expect(reviewApi.save).not.toHaveBeenCalled()
    expect(reviewApi.confirm).not.toHaveBeenCalled()
    expect(reviewApi.importCallouts).toHaveBeenCalledTimes(1)
    await act(async () => { request.resolve(imported()); await pending })
    expect(result.current.importing).toBe(false)
  })

  it('preserves local edits on a conflict and permits retry', async () => {
    vi.mocked(reviewApi.importCallouts).mockRejectedValue(new ApiError('版本不一致', 409))
    const { result } = renderHook(() => useKnowledgeReview())
    await act(async () => { await result.current.show(reference) })
    act(() => result.current.setDraft((current) => current ? { ...current, map_name: '未保存修改' } : current))
    await act(async () => { await result.current.importCallouts() })
    expect(result.current.draft?.map_name).toBe('未保存修改')
    expect(result.current.document?.revision).toBe(3)
    expect(result.current.error).toContain('草稿版本已变化')
    expect(result.current.importing).toBe(false)
  })

  it('does not start an old import after switching sessions while choosing a file', async () => {
    const picker = deferred<string | null>()
    vi.mocked(pickFilePath).mockReturnValue(picker.promise)
    const { result, rerender } = renderHook(() => useKnowledgeReview())
    await act(async () => { await result.current.show(reference) })
    let pending!: Promise<void>
    act(() => { pending = result.current.importCallouts() })
    sessionState.activeSessionId = 'session-2'
    rerender()
    await act(async () => { picker.resolve(workbookPath); await pending })
    expect(reviewApi.importCallouts).not.toHaveBeenCalled()
    expect(result.current.importing).toBe(false)
    expect(result.current.error).toBe('')
  })

  it('aborts an in-flight import on session change and ignores its stale response', async () => {
    const request = deferred<CalloutImportResult>()
    vi.mocked(reviewApi.importCallouts).mockReturnValue(request.promise)
    const { result, rerender } = renderHook(() => useKnowledgeReview())
    await act(async () => { await result.current.show(reference) })
    let pending!: Promise<void>
    await act(async () => { pending = result.current.importCallouts() })
    const signal = vi.mocked(reviewApi.importCallouts).mock.calls[0][4]
    sessionState.activeSessionId = 'session-2'
    rerender()
    expect(signal?.aborted).toBe(true)
    const nextRef = { ...reference, session_id: 'session-2', review_id: 'review-2' }
    vi.mocked(reviewApi.get).mockResolvedValue({ ...makeDocument(), ...nextRef, revision: 9 })
    await act(async () => { await result.current.show(nextRef) })
    await act(async () => { request.resolve(imported()); await pending })
    expect(result.current.document?.review_id).toBe('review-2')
    expect(result.current.document?.revision).toBe(9)
    expect(result.current.importing).toBe(false)
    expect(result.current.notice).toBe('')
  })

  it('aborts the request when the review controller is unmounted', async () => {
    const request = deferred<CalloutImportResult>()
    vi.mocked(reviewApi.importCallouts).mockReturnValue(request.promise)
    const { result, unmount } = renderHook(() => useKnowledgeReview())
    await act(async () => { await result.current.show(reference) })
    let pending!: Promise<void>
    await act(async () => { pending = result.current.importCallouts() })
    const signal = vi.mocked(reviewApi.importCallouts).mock.calls[0][4]
    unmount()
    expect(signal?.aborted).toBe(true)
    await act(async () => { request.resolve(imported()); await pending })
  })

  it('cancels cleanly and explains unsupported old .xls files', async () => {
    const { result } = renderHook(() => useKnowledgeReview())
    await act(async () => { await result.current.show(reference) })
    vi.mocked(pickFilePath).mockResolvedValueOnce(null)
    await act(async () => { await result.current.importCallouts() })
    expect(result.current.error).toBe('')
    vi.mocked(pickFilePath).mockResolvedValueOnce('/tmp/notes.xls')
    await act(async () => { await result.current.importCallouts() })
    expect(result.current.error).toContain('另存为 .xlsx')
    expect(reviewApi.importCallouts).not.toHaveBeenCalled()
    expect(result.current.importing).toBe(false)
  })

  it('offers import in an empty callout list', async () => {
    const { result } = renderHook(() => useKnowledgeReview())
    await act(async () => { await result.current.show(reference) })
    await act(async () => { render(<KnowledgeReviewDialog controller={result.current} />) })
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: '制图表达' })) })
    expect(screen.getByRole('button', { name: '从 Excel 导入' })).toBeEnabled()
  })
})

describe('callout presentation defaults', () => {
  it('starts without a title in text mode and switches to top-image layout on adding an image', async () => {
    const item = createItem('callouts')
    expect(item).toMatchObject({ title: '', template: 'text_only', leader_style: 'double', image_enabled: false })
    vi.mocked(pickFilePath).mockResolvedValue('/tmp/portrait.jpg')
    vi.mocked(reviewApi.importAsset).mockResolvedValue({ image_path: '/project/image.jpg', preview_url: '/api/image.jpg' })
    const onChange = vi.fn()
    render(<CalloutEditor item={item} graph={graph} onChange={onChange} disabled={false} reference={reference} document={makeDocument()} onBusyChange={vi.fn()} onAssetImported={vi.fn()} />)
    expect(screen.queryByText('注记标题')).not.toBeInTheDocument()
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: '选择本地图片' })) })
    expect(onChange).toHaveBeenCalledWith({ image_path: '/project/image.jpg', image_enabled: true, template: 'image_top_text_bottom' })
  })

  it('uses the anchor name for navigation, hides legacy titles, and lets a portrait fill the text width', () => {
    const item = { callout_id: 'legacy', anchor_ref_type: 'spatial_entity', anchor_ref: 'he', title: '旧标题不显示', body: '正文', image_enabled: true, image_path: '/project/image.jpg', template: 'image_top_text_bottom' }
    expect(itemOptions({ ...graph, callouts: [item] }, 'callouts')).toEqual([['legacy', '何家冲']])
    expect(issueLabel({ path: 'callouts[0].body', message: '正文' }, { ...graph, callouts: [item] })).toContain('何家冲')
    expect(referencesTo({ ...graph, callouts: [item] }, 'spatial_entities', 'he')).toEqual(['注记「何家冲」'])
    const onChange = vi.fn()
    render(<CalloutEditor item={item} graph={graph} onChange={onChange} disabled={false} reference={reference} document={{ ...makeDocument(), assets: [{ image_path: '/project/image.jpg', preview_url: '/api/image.jpg' }] }} onBusyChange={vi.fn()} onAssetImported={vi.fn()} />)
    const preview = within(screen.getByRole('region', { name: '图文注记效果示意' }))
    expect(preview.queryByText('旧标题不显示')).not.toBeInTheDocument()
    expect(preview.getByRole('img')).toHaveClass('w-full', 'h-auto')
    expect(preview.getByRole('img')).not.toHaveClass('max-h-40')
    fireEvent.click(screen.getByRole('button', { name: '移除图片引用' }))
    expect(onChange).toHaveBeenCalledWith({ image_path: '', image_enabled: false, template: 'text_only' })
  })

  it('uses top-image layout after adding an image to older data without a template', async () => {
    const item = { callout_id: 'old', body: '正文' }
    vi.mocked(pickFilePath).mockResolvedValue('/tmp/portrait.jpg')
    vi.mocked(reviewApi.importAsset).mockResolvedValue({ image_path: '/project/image.jpg', preview_url: '/api/image.jpg' })
    const onChange = vi.fn()
    render(<CalloutEditor item={item} graph={graph} onChange={onChange} disabled={false} reference={reference} document={makeDocument()} onBusyChange={vi.fn()} onAssetImported={vi.fn()} />)
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: '选择本地图片' })) })
    expect(onChange).toHaveBeenCalledWith({ image_path: '/project/image.jpg', image_enabled: true, template: 'image_top_text_bottom' })
  })
})
