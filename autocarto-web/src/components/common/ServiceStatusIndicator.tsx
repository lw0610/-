'use client'

import { useEffect, useMemo, useRef, useState, useCallback } from 'react'
import { createPortal } from 'react-dom'
import { ServicesIcon } from '@/components/common/AppIcon'
import { buildTopBarServiceStatusItems, getAggregateStatus } from '@/lib/serviceStatus'
import { useConnectionStore } from '@/stores/connectionStore'
import { useRuntimeSnapshotStore } from '@/stores/runtimeSnapshotStore'

interface ServiceStatusIndicatorProps {
  projectConnected: boolean
  projectStatusText?: string
}

export default function ServiceStatusIndicator({
  projectConnected,
  projectStatusText = '',
}: ServiceStatusIndicatorProps) {
  const websocketConnected = useConnectionStore((s) => s.connected)
  const runtimeSnapshot = useRuntimeSnapshotStore((s) => s.snapshot)
  const [open, setOpen] = useState(false)
  const [popoverStyle, setPopoverStyle] = useState<React.CSSProperties>({})
  const buttonRef = useRef<HTMLButtonElement>(null)
  const popoverRef = useRef<HTMLDivElement>(null)
  const closeTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const cancelCloseTimer = () => {
    if (!closeTimerRef.current) {
      return
    }
    clearTimeout(closeTimerRef.current)
    closeTimerRef.current = null
  }

  const scheduleClose = (delay = 120) => {
    cancelCloseTimer()
    closeTimerRef.current = setTimeout(() => {
      setOpen(false)
      closeTimerRef.current = null
    }, delay)
  }

  const updatePosition = useCallback(() => {
    if (open && buttonRef.current) {
      const rect = buttonRef.current.getBoundingClientRect()
      // Default to right-aligned with the button
      let left = rect.right - 320 // 320 is the width (w-80 = 20rem = 320px)
      
      // Ensure it doesn't go off-screen to the left
      if (left < 10) {
        left = 10
      }
      
      setPopoverStyle({
        top: `${rect.bottom + 8}px`,
        left: `${left}px`,
      })
    }
  }, [open])

  useEffect(() => {
    if (open) {
      updatePosition()
      window.addEventListener('scroll', updatePosition, true)
      window.addEventListener('resize', updatePosition)
    }
    return () => {
      window.removeEventListener('scroll', updatePosition, true)
      window.removeEventListener('resize', updatePosition)
    }
  }, [open, updatePosition])

  const items = useMemo(() => buildTopBarServiceStatusItems({
    snapshot: runtimeSnapshot,
    websocketConnected,
    projectConnected,
    projectStatusText,
  }), [projectConnected, projectStatusText, runtimeSnapshot, websocketConnected])

  const aggregate = useMemo(() => getAggregateStatus(items), [items])
  const runtimeItems = useMemo(() => items.filter((item) => item.kind === 'service'), [items])
  const connectionItems = useMemo(() => items.filter((item) => item.kind === 'connection'), [items])

  useEffect(() => () => {
    cancelCloseTimer()
  }, [])

  const dotColorClass = useMemo(() => {
    if (aggregate.tone === 'danger') return 'bg-red-500'
    if (aggregate.tone === 'warning') return 'bg-amber-500'
    if (aggregate.tone === 'success') return 'bg-emerald-500'
    return 'bg-zinc-400'
  }, [aggregate.tone])

  return (
    <div
      className="relative"
      onMouseEnter={() => {
        cancelCloseTimer()
        setOpen(true)
      }}
      onMouseLeave={() => {
        scheduleClose()
      }}
      onFocusCapture={() => {
        cancelCloseTimer()
        setOpen(true)
      }}
      onBlurCapture={(event) => {
        const nextTarget = event.relatedTarget as Node | null
        if (nextTarget && 
            (event.currentTarget.contains(nextTarget) || 
             popoverRef.current?.contains(nextTarget))) {
          return
        }
        scheduleClose(0)
      }}
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          cancelCloseTimer()
          setOpen(false)
        }
      }}
    >
      <button
        ref={buttonRef}
        type="button"
        title="服务连接状态"
        aria-label="服务连接状态"
        className="flex h-7 w-7 items-center justify-center rounded-sm border border-zinc-200 bg-white transition-colors hover:bg-zinc-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2 focus-visible:ring-offset-zinc-50 dark:border-zinc-800 dark:bg-zinc-900 dark:hover:bg-zinc-800 dark:focus-visible:ring-offset-zinc-950"
      >
        <span className={`h-2.5 w-2.5 rounded-full ${dotColorClass}`} />
      </button>

      {open && typeof document !== 'undefined' && createPortal(
        <div
          ref={popoverRef}
          style={popoverStyle}
          className="fixed z-[100] w-80 rounded border border-zinc-200/80 bg-white/95 backdrop-blur shadow-lg transition-opacity dark:border-zinc-800 dark:bg-zinc-950/95"
          onMouseEnter={() => cancelCloseTimer()}
          onMouseLeave={() => scheduleClose()}
        >
          <div className="flex items-center justify-between border-b border-zinc-200 px-3 py-2 dark:border-zinc-800">
            <div className="flex items-center gap-2 text-xs font-medium text-zinc-700 dark:text-zinc-100">
              <ServicesIcon className="h-3.5 w-3.5" />
              <span>服务连接状态</span>
            </div>
            <span className={`rounded px-1.5 py-0.5 text-[10px] ${aggregate.badgeClassName}`}>{aggregate.label}</span>
          </div>

          <div className="space-y-3 p-2">
            {[
              { key: 'runtime', label: '运行时服务', entries: runtimeItems },
              { key: 'connections', label: '工作区连接', entries: connectionItems },
            ].map((group) => group.entries.length > 0 && (
              <div key={group.key} className="space-y-2">
                <div className="px-1 text-[11px] font-medium text-zinc-500 dark:text-zinc-400">{group.label}</div>
                {group.entries.map((item) => (
                  <div key={item.id} className="rounded-sm border border-zinc-200/80 bg-zinc-50/70 p-2 dark:border-zinc-800 dark:bg-zinc-900/70">
                    <div className="flex items-center justify-between gap-2">
                      <div className="flex min-w-0 items-center gap-2">
                        <span className={`h-2 w-2 shrink-0 rounded-full ${item.dotClassName}`} />
                        <span className="truncate text-xs font-medium text-zinc-700 dark:text-zinc-100">{item.label}</span>
                      </div>
                      <span className={`shrink-0 rounded px-1.5 py-0.5 text-[10px] ${item.badgeClassName}`}>{item.statusLabel}</span>
                    </div>
                    {item.meta && (
                      <div className="mt-1 text-[11px] text-zinc-500 dark:text-zinc-400">{item.meta}</div>
                    )}
                    {item.lastError && (
                      <div className="mt-1 line-clamp-2 text-[11px] text-red-600 dark:text-red-300">{item.lastError}</div>
                    )}
                  </div>
                ))}
              </div>
            ))}
          </div>
        </div>,
        document.body
      )}
    </div>
  )
}
