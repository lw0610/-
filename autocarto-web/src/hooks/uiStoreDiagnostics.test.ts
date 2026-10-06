import { afterEach, describe, expect, it, vi } from 'vitest'

describe('uiStore diagnostics', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.resetModules()
  })

  it('reports localStorage read failure and falls back to default', async () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('storage blocked')
    })

    const { useUIStore } = await import('@/stores/uiStore')

    expect(useUIStore.getState().sessionPanelCollapsed).toBe(false)
    expect(warnSpy).toHaveBeenCalled()
  })

  it('reports localStorage write failure without breaking state update', async () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('quota exceeded')
    })

    const { useUIStore } = await import('@/stores/uiStore')
    useUIStore.getState().setSessionPanelCollapsed(true)

    expect(useUIStore.getState().sessionPanelCollapsed).toBe(true)
    expect(warnSpy).toHaveBeenCalled()
  })
})
