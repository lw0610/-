import { afterEach, describe, expect, it, vi } from 'vitest'
import api, { ApiClient } from '@/lib/api'
import { reviewApi } from './api'
import type { KnowledgeGraph } from './model'

afterEach(() => vi.restoreAllMocks())

describe('knowledge review geocode request', () => {
  it('imports Excel together with the current graph using an extended timeout', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({})
    const graph: KnowledgeGraph = { actors: [], events: [], spatial_entities: [], routes: [], callouts: [] }
    await reviewApi.importCallouts({ session_id: 'session/a', review_id: 'review/b', checkpoint_id: 'checkpoint' }, 7, '/tmp/notes.xlsx', graph)
    expect(post).toHaveBeenCalledWith('/api/sessions/session%2Fa/knowledge-reviews/review%2Fb/callouts/import', { base_revision: 7, source_path: '/tmp/notes.xlsx', graph }, { timeoutMs: 600_000 })
  })
  it('sends only the saved revision and allows time for batch geocoding', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({})
    await reviewApi.geocode({ session_id: 'session/a', review_id: 'review/b', checkpoint_id: 'checkpoint' }, 7)
    expect(post).toHaveBeenCalledWith('/api/sessions/session%2Fa/knowledge-reviews/review%2Fb/geocode', { base_revision: 7 }, { timeoutMs: 300_000 })
  })

  it('uses the longer timeout only for requests that explicitly ask for it', async () => {
    const timeout = vi.spyOn(AbortSignal, 'timeout')
    vi.spyOn(globalThis, 'fetch').mockImplementation(async () => new Response(JSON.stringify({ success: true, data: {} }), { status: 200 }))
    const client = new ApiClient('http://localhost:9000')
    await client.post('/api/review/geocode', { base_revision: 1 }, { timeoutMs: 300_000 })
    expect(timeout).toHaveBeenLastCalledWith(300_000)
    await client.post('/api/review/confirm', { base_revision: 1 })
    expect(timeout).toHaveBeenLastCalledWith(15_000)
  })
})
