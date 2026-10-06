'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import api, { SessionRecord } from '@/lib/api'
import { loadSessionsForProject } from '@/lib/session-utils'
import type { QGISInstance } from '@/types/instance'

interface UseSessionPreviewParams {
  hoverPreviewMode: boolean
  hoveredInstance: QGISInstance | null
  previewProjectPath: string
}

function sortSessions(list: SessionRecord[]) {
  return [...list].sort((a, b) => {
    const ta = new Date(a.updated_at || a.created_at || 0).getTime()
    const tb = new Date(b.updated_at || b.created_at || 0).getTime()
    return tb - ta
  })
}

export function useSessionPreview({
  hoverPreviewMode,
  hoveredInstance,
  previewProjectPath,
}: UseSessionPreviewParams) {
  const [hoverSessionsLoading, setHoverSessionsLoading] = useState(false)
  const [hoverSessions, setHoverSessions] = useState<SessionRecord[]>([])
  const hoverSessionCacheRef = useRef<Record<string, SessionRecord[]>>({})

  const invalidateHoverSessionCache = useCallback(() => {
    hoverSessionCacheRef.current = {}
  }, [])

  useEffect(() => {
    let cancelled = false

    const loadHoverSessions = async () => {
      if (!hoverPreviewMode || !hoveredInstance) {
        setHoverSessions([])
        setHoverSessionsLoading(false)
        return
      }

      const cacheKey = `${hoveredInstance.id}::${previewProjectPath}`
      const cached = hoverSessionCacheRef.current[cacheKey]
      if (cached) {
        setHoverSessions(cached)
        setHoverSessionsLoading(false)
        return
      }

      setHoverSessionsLoading(true)
      try { const list = await loadSessionsForProject((path) => api.getSessions(path), previewProjectPath)
              const sorted = sortSessions(list)
              if (!cancelled) {
                hoverSessionCacheRef.current[cacheKey] = sorted
                setHoverSessions(sorted)
              } } catch (error) { void error; if (!cancelled) {
              setHoverSessions([])
            } } finally {
        if (!cancelled) {
          setHoverSessionsLoading(false)
        }
      }
    }

    void loadHoverSessions()

    return () => {
      cancelled = true
    }
  }, [hoverPreviewMode, hoveredInstance, previewProjectPath])

  return {
    hoverSessions,
    hoverSessionsLoading,
    invalidateHoverSessionCache,
  }
}
