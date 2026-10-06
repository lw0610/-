'use client'

import { MouseEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import api, { SessionRecord } from '@/lib/api'
import { loadSessionsForProject } from '@/lib/session-utils'
import type { QGISInstance } from '@/types/instance'

interface UseInstanceTooltipParams {
  instances: QGISInstance[]
  sessionPanelCollapsed: boolean
  onInstanceHover?: (instanceId: string | null) => void
}

export interface TooltipState {
  instanceId: string
  top: number
  left: number
}

export interface TooltipSummaryView {
  loading: boolean
  total: number
  recent: string[]
  error: boolean
}

interface SessionSummaryState extends TooltipSummaryView {
  instanceId: string | null
}

interface CachedSessionSummary {
  total: number
  recent: string[]
  fetchedAt: number
}

const SUMMARY_CACHE_TTL_MS = 15000

const emptyTooltipSummary: TooltipSummaryView = {
  loading: false,
  total: 0,
  recent: [],
  error: false,
}

const emptySummaryState: SessionSummaryState = {
  instanceId: null,
  ...emptyTooltipSummary,
}

function getProjectPath(instance: QGISInstance) {
  const value = typeof instance.project_path === 'string' ? instance.project_path.trim() : ''
  return value
}

function getSummaryCacheKey(instance: QGISInstance) {
  return `${instance.id}::${getProjectPath(instance)}`
}

function sessionLabel(session: SessionRecord) {
  const value = typeof session.name === 'string' ? session.name.trim() : ''
  if (value) {
    return value
  }
  return session.session_id
}

function summarizeSessions(list: SessionRecord[]) {
  const visible = list.filter((item) => !item.archived)
  return {
    total: visible.length,
    recent: visible.slice(0, 3).map(sessionLabel),
  }
}

export function useInstanceTooltip({
  instances,
  sessionPanelCollapsed,
  onInstanceHover,
}: UseInstanceTooltipParams) {
  const [tooltipState, setTooltipState] = useState<TooltipState | null>(null)
  const [summaryState, setSummaryState] = useState<SessionSummaryState>(emptySummaryState)
  const summaryCacheRef = useRef<Record<string, CachedSessionSummary>>({})
  const summaryRequestTokenRef = useRef(0)

  useEffect(() => {
    if (!sessionPanelCollapsed) {
      onInstanceHover?.(null)
    }
  }, [onInstanceHover, sessionPanelCollapsed])

  useEffect(() => {
    if (sessionPanelCollapsed) {
      setTooltipState(null)
    }
  }, [sessionPanelCollapsed])

  useEffect(() => {
    if (!tooltipState) {
      return
    }
    const exists = instances.some((item) => item.id === tooltipState.instanceId)
    if (!exists) {
      setTooltipState(null)
    }
  }, [instances, tooltipState])

  useEffect(() => {
    return () => {
      onInstanceHover?.(null)
    }
  }, [onInstanceHover])

  const loadInstanceSummary = useCallback(async (instance: QGISInstance) => {
    const cacheKey = getSummaryCacheKey(instance)
    const cache = summaryCacheRef.current[cacheKey]
    if (cache && Date.now() - cache.fetchedAt < SUMMARY_CACHE_TTL_MS) {
      setSummaryState({
        instanceId: instance.id,
        loading: false,
        total: cache.total,
        recent: cache.recent,
        error: false,
      })
      return
    }

    setSummaryState({
      instanceId: instance.id,
      loading: true,
      total: 0,
      recent: [],
      error: false,
    })

    const token = ++summaryRequestTokenRef.current
    try { const projectPath = getProjectPath(instance)
    		const list = await loadSessionsForProject((path) => api.getSessions(path), projectPath)
    const summary = summarizeSessions(list)
    summaryCacheRef.current[cacheKey] = {
      ...summary,
      fetchedAt: Date.now(),
    }
    
    if (summaryRequestTokenRef.current !== token) {
      return
    }
    setSummaryState({
      instanceId: instance.id,
      loading: false,
      total: summary.total,
      recent: summary.recent,
      error: false,
    }) } catch (error) { void error; if (summaryRequestTokenRef.current !== token) {
      return
    }
    setSummaryState({
      instanceId: instance.id,
      loading: false,
      total: 0,
      recent: [],
      error: true,
    }) }
  }, [])

  const handleInstanceMouseEnter = useCallback((instance: QGISInstance, event: MouseEvent<HTMLButtonElement>) => {
    if (sessionPanelCollapsed) {
      onInstanceHover?.(instance.id)
      return
    }

    const rect = event.currentTarget.getBoundingClientRect()
    setTooltipState({
      instanceId: instance.id,
      top: rect.top + rect.height / 2,
      left: rect.right + 10,
    })
    void loadInstanceSummary(instance)
  }, [loadInstanceSummary, onInstanceHover, sessionPanelCollapsed])

  const handleInstanceMouseLeave = useCallback((instanceId: string) => {
    if (sessionPanelCollapsed) {
      return
    }
    setTooltipState((prev) => {
      if (!prev || prev.instanceId !== instanceId) {
        return prev
      }
      return null
    })
  }, [sessionPanelCollapsed])

  const handlePanelMouseLeave = useCallback(() => {
    if (sessionPanelCollapsed) {
      onInstanceHover?.(null)
      return
    }
    setTooltipState(null)
  }, [onInstanceHover, sessionPanelCollapsed])

  const tooltipInstance = useMemo(() => {
    if (!tooltipState) {
      return null
    }
    return instances.find((item) => item.id === tooltipState.instanceId) || null
  }, [instances, tooltipState])

  const currentTooltipSummary = useMemo<TooltipSummaryView>(() => {
    if (!tooltipInstance) {
      return emptyTooltipSummary
    }
    if (summaryState.instanceId === tooltipInstance.id) {
      return {
        loading: summaryState.loading,
        total: summaryState.total,
        recent: summaryState.recent,
        error: summaryState.error,
      }
    }
    return {
      loading: true,
      total: 0,
      recent: [],
      error: false,
    }
  }, [summaryState, tooltipInstance])

  return {
    currentTooltipSummary,
    handleInstanceMouseEnter,
    handleInstanceMouseLeave,
    handlePanelMouseLeave,
    tooltipInstance,
    tooltipState,
  }
}
