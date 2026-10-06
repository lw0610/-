'use client'

import React from 'react'
import StatusBadge from '@/components/settings/StatusBadge'
import { cn } from '@/lib/utils'
import { getSessionRuntimeStatusMeta } from '@/lib/sessionRuntimeDisplay'
import type { SessionRuntimeStateRecord } from '@/stores/chatStore'

interface SessionRuntimeSummaryProps {
  runtime: Partial<SessionRuntimeStateRecord> | null | undefined
  connectionHint?: 'reattaching' | 'replay_required'
  showSessionId?: boolean
  showProjectPath?: boolean
  showFailureReason?: boolean
  badgeSize?: 'xs' | 'sm'
  className?: string
  textClassName?: string
  chipClassName?: string
  sessionIdClassName?: string
}

export default function SessionRuntimeSummary({
  runtime,
  showSessionId = false,
  showProjectPath = true,
  showFailureReason = true,
  connectionHint,
  badgeSize = 'sm',
  className,
  textClassName,
  chipClassName,
  sessionIdClassName,
}: SessionRuntimeSummaryProps) {
  if (!runtime && !connectionHint) {
    return null
  }

  const meta = runtime ? getSessionRuntimeStatusMeta(runtime.status) : null
  const sessionId = String(runtime?.session_id || '').trim()
  const boundInstanceId = String(runtime?.bound_instance_id || '').trim()
  const boundProjectPath = String(runtime?.bound_project_path || '').trim()
  const failureReason = String(runtime?.failure_reason || '').trim()

  const pillClassName = cn(
    'inline-flex max-w-full items-center rounded-sm border border-zinc-200 bg-white px-2 py-0.5 dark:border-zinc-700 dark:bg-zinc-950',
    badgeSize === 'xs' ? 'text-[10px]' : 'text-[11px]',
    textClassName,
    chipClassName,
  )

  return (
    <div className={cn('flex flex-wrap items-center gap-2', textClassName, className)}>
      {showSessionId && sessionId && (
        <span className={cn('font-medium text-zinc-900 dark:text-zinc-100', sessionIdClassName)}>
          {sessionId}
        </span>
      )}
      {connectionHint === 'reattaching' && (
        <StatusBadge tone="warning" size={badgeSize}>重连中</StatusBadge>
      )}
      {connectionHint === 'replay_required' && (
        <StatusBadge tone="danger" size={badgeSize}>需重同步</StatusBadge>
      )}
      {meta && <StatusBadge tone={meta.tone} size={badgeSize}>{meta.label}</StatusBadge>}
      {boundInstanceId && <span className={pillClassName}>实例 {boundInstanceId}</span>}
      {showProjectPath && boundProjectPath && (
        <span className={cn(pillClassName, 'max-w-[40rem] truncate')}>
          工程 {boundProjectPath}
        </span>
      )}
      {showFailureReason && failureReason && (
        <span className={cn('text-red-600 dark:text-red-300', badgeSize === 'xs' ? 'text-[10px]' : 'text-[11px]')}>
          原因：{failureReason}
        </span>
      )}
    </div>
  )
}
