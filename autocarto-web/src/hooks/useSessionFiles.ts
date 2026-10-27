'use client'
import { useCallback, useEffect, useRef, useState } from 'react'
import { create } from 'zustand'
import wsClient from '@/lib/websocket'
import { useUIStore } from '@/stores/uiStore'
import { fileApi, type SessionFile } from '@/components/workspace/files'
const empty: SessionFile[] = []
export const useSessionFilesStore = create<{
  items: Record<string, SessionFile[]>
  put: (id: string, items: SessionFile[]) => void
}>((set) => ({
  items: {},
  put: (id, items) =>
    set((state) => ({ items: { ...state.items, [id]: items } })),
}))
export function countNewFiles(items: SessionFile[], viewedAt: number) {
  return items.filter(
    (item) =>
      item.origin.type !== 'preview' &&
      new Date(item.registered_at).getTime() > viewedAt,
  ).length
}
export function useSessionFiles(sessionId: string, projectPath: string) {
  const [all, setAll] = useState(false),
    [loading, setLoading] = useState(false),
    [error, setError] = useState(''),
    [sessionDir, setSessionDir] = useState('')
  const items = useSessionFilesStore((state) => state.items[sessionId] || empty)
  const viewedAt = useUIStore((state) => state.filesViewedAt[sessionId] || 0)
  const request = useRef(0)
  const refresh = useCallback(async () => {
    const seq = ++request.current
    if (!sessionId || !projectPath) {
      setSessionDir('')
      setLoading(false)
      setError('')
      return
    }
    setLoading(true)
    setError('')
    try {
      const response = await fileApi.list(sessionId, projectPath, all)
      if (seq !== request.current) return
      useSessionFilesStore.getState().put(sessionId, response.items || [])
      setSessionDir(response.session_dir)
    } catch (error) {
      if (seq === request.current)
        setError(error instanceof Error ? error.message : String(error))
    } finally {
      if (seq === request.current) setLoading(false)
    }
  }, [sessionId, projectPath, all])
  useEffect(() => {
    const generation = request
    void refresh()
    return () => {
      generation.current++
    }
  }, [refresh])
  useEffect(() => {
    const unsubscribe = wsClient.onMessage((message) => {
      const data = message.data as Record<string, unknown> | undefined
      if (
        message.type === 'session_files_changed' &&
        (message.session_id || data?.session_id) === sessionId
      )
        void refresh()
    })
    return () => {
      unsubscribe()
    }
  }, [refresh, sessionId])
  useEffect(() => {
    const refreshOnFocus = () => void refresh()
    window.addEventListener('focus', refreshOnFocus)
    return () => window.removeEventListener('focus', refreshOnFocus)
  }, [refresh])
  return {
    items,
    all,
    setAll,
    loading,
    error,
    sessionDir,
    refresh,
    newCount: countNewFiles(items, viewedAt),
    viewedAt,
  }
}
