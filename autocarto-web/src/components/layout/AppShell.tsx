'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import { usePathname } from 'next/navigation'
import TopBar from '@/components/layout/TopBar'
import InstancePanel from '@/components/layout/InstancePanel'
import SessionPanel from '@/components/layout/SessionPanel'
import { isWorkspacePath } from '@/lib/workspaceRoutes'
import { useInstanceStore } from '@/stores/instanceStore'
import { useUIStore } from '@/stores/uiStore'

const SESSION_PREVIEW_HOLD_MS = 320

function clearTimer(ref: { current: ReturnType<typeof setTimeout> | null }) {
  if (!ref.current) {
    return
  }
  clearTimeout(ref.current)
  ref.current = null
}

export default function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname()
  const sessionPanelCollapsed = useUIStore((s) => s.sessionPanelCollapsed)
  const projectConnected = useInstanceStore((s) => s.projectConnected)
  const inWorkspacePage = isWorkspacePath(pathname)

  const [hoveredInstanceId, setHoveredInstanceId] = useState<string | null>(null)
  const previewHoldTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const previewPanelHoverRef = useRef(false)

  const clearPreviewHoldTimer = useCallback(() => {
    clearTimer(previewHoldTimerRef)
  }, [])

  const schedulePreviewClear = useCallback((delay = SESSION_PREVIEW_HOLD_MS) => {
    clearPreviewHoldTimer()
    previewHoldTimerRef.current = setTimeout(() => {
      if (!previewPanelHoverRef.current) {
        setHoveredInstanceId(null)
      }
      previewHoldTimerRef.current = null
    }, delay)
  }, [clearPreviewHoldTimer])

  const handleInstanceHover = useCallback((instanceId: string | null) => {
    if (instanceId) {
      clearPreviewHoldTimer()
      setHoveredInstanceId(instanceId)
      return
    }

    if (!sessionPanelCollapsed) {
      clearPreviewHoldTimer()
      setHoveredInstanceId(null)
      return
    }

    if (previewPanelHoverRef.current) {
      return
    }

    schedulePreviewClear()
  }, [clearPreviewHoldTimer, schedulePreviewClear, sessionPanelCollapsed])

  const handleSessionPreviewInteractionChange = useCallback((active: boolean) => {
    previewPanelHoverRef.current = active
    if (active) {
      clearPreviewHoldTimer()
      return
    }

    if (sessionPanelCollapsed && hoveredInstanceId) {
      schedulePreviewClear(220)
    }
  }, [clearPreviewHoldTimer, hoveredInstanceId, schedulePreviewClear, sessionPanelCollapsed])

  useEffect(() => {
    if (!sessionPanelCollapsed) {
      clearPreviewHoldTimer()
      previewPanelHoverRef.current = false
      setHoveredInstanceId(null)
    }
  }, [clearPreviewHoldTimer, sessionPanelCollapsed])

  useEffect(() => {
    return () => {
      clearPreviewHoldTimer()
    }
  }, [clearPreviewHoldTimer])

  return (
    <div className="flex h-screen w-screen flex-col overflow-hidden bg-[#fafafa] dark:bg-zinc-950">
      <TopBar />
      <div className="flex min-h-0 flex-1 overflow-hidden">
        {!inWorkspacePage && (
          <InstancePanel
            sessionPanelCollapsed={sessionPanelCollapsed}
            onInstanceHover={handleInstanceHover}
          />
        )}
        {!inWorkspacePage && projectConnected && (
          <SessionPanel
            hoveredInstanceId={hoveredInstanceId}
            onPreviewInteractionChange={handleSessionPreviewInteractionChange}
          />
        )}
        <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
          <main className="flex min-h-0 flex-1 flex-col overflow-hidden">{children}</main>
        </div>
      </div>
    </div>
  )
}
