import type { SessionRuntimeStateRecord } from '@/stores/chatStore'

export type SessionRuntimeStatus = 'running' | 'interrupted' | 'failed' | 'completed' | 'cancelled' | 'unknown'
export type SessionRuntimeTone = 'success' | 'warning' | 'danger' | 'neutral'

export interface SessionRuntimeStatusMeta {
  status: SessionRuntimeStatus
  label: string
  tone: SessionRuntimeTone
  dotClassName: string
  active: boolean
  order: number
  emptyPreview: string
}

interface SessionRuntimeSessionLike {
  session_id: string
  name?: string
  updated_at?: string
  last_message_preview?: string
}

export interface BackgroundTaskItem {
  sessionId: string
  title: string
  preview: string
  interruptCount: number
  runtime: SessionRuntimeStateRecord
}

interface BuildBackgroundTaskItemsInput {
  runningSessionIds?: string[]
  sessionPendingInterrupts?: Record<string, unknown[] | undefined>
  sessionRuntimeStates?: Record<string, Partial<SessionRuntimeStateRecord> | null | undefined>
  sessions?: SessionRuntimeSessionLike[]
  limit?: number
}

const STATUS_META: Record<SessionRuntimeStatus, SessionRuntimeStatusMeta> = {
  running: {
    status: 'running',
    label: '运行中',
    tone: 'success',
    dotClassName: 'bg-emerald-500',
    active: true,
    order: 0,
    emptyPreview: '任务正在后台执行中',
  },
  interrupted: {
    status: 'interrupted',
    label: '等待确认',
    tone: 'warning',
    dotClassName: 'bg-amber-500',
    active: true,
    order: 1,
    emptyPreview: '任务正在等待确认后继续',
  },
  failed: {
    status: 'failed',
    label: '失败',
    tone: 'danger',
    dotClassName: 'bg-red-500',
    active: false,
    order: 2,
    emptyPreview: '会话运行失败',
  },
  completed: {
    status: 'completed',
    label: '已完成',
    tone: 'neutral',
    dotClassName: 'bg-zinc-400 dark:bg-zinc-500',
    active: false,
    order: 3,
    emptyPreview: '会话运行已完成',
  },
  cancelled: {
    status: 'cancelled',
    label: '已取消',
    tone: 'neutral',
    dotClassName: 'bg-zinc-400 dark:bg-zinc-500',
    active: false,
    order: 4,
    emptyPreview: '会话运行已取消',
  },
  unknown: {
    status: 'unknown',
    label: '未知',
    tone: 'neutral',
    dotClassName: 'bg-zinc-400 dark:bg-zinc-500',
    active: false,
    order: 5,
    emptyPreview: '暂无运行态信息',
  },
}

function normalizeRuntimeStatus(raw?: string | null): SessionRuntimeStatus {
  const value = String(raw || '').trim().toLowerCase()
  if (value === 'running' || value === 'interrupted' || value === 'failed' || value === 'completed' || value === 'cancelled') {
    return value
  }
  return 'unknown'
}

function parseUpdatedAt(value?: string | null) {
  const time = new Date(String(value || '')).getTime()
  return Number.isFinite(time) ? time : 0
}

function normalizeRuntimeRecord(
  sessionId: string,
  runtime: Partial<SessionRuntimeStateRecord> | null | undefined,
  fallbackStatus?: string,
  fallbackUpdatedAt?: string,
): SessionRuntimeStateRecord {
  return {
    session_id: sessionId,
    run_id: String(runtime?.run_id || '').trim() || undefined,
    status: String(runtime?.status || fallbackStatus || '').trim().toLowerCase() || undefined,
    execution_mode: String(runtime?.execution_mode || '').trim() || undefined,
    phase: String(runtime?.phase || '').trim() || undefined,
    bound_instance_id: String(runtime?.bound_instance_id || '').trim() || undefined,
    bound_project_path: String(runtime?.bound_project_path || '').trim() || undefined,
    checkpoint_id: String(runtime?.checkpoint_id || '').trim() || undefined,
    runtime_revision: String(runtime?.runtime_revision || '').trim() || undefined,
    active_agent_config: String(runtime?.active_agent_config || '').trim() || undefined,
    last_error: String(runtime?.last_error || '').trim() || undefined,
    failure_code: String(runtime?.failure_code || '').trim() || undefined,
    failure_reason: String(runtime?.failure_reason || '').trim() || undefined,
    updated_at: String(runtime?.updated_at || fallbackUpdatedAt || '').trim() || undefined,
  }
}

export function getSessionRuntimeStatusMeta(status?: string | null): SessionRuntimeStatusMeta {
  return STATUS_META[normalizeRuntimeStatus(status)]
}

export function isSessionRuntimeActive(status?: string | null) {
  return getSessionRuntimeStatusMeta(status).active
}

export function compareSessionRuntimeRecords<T extends { status?: string; updated_at?: string }>(left: T, right: T) {
  const leftMeta = getSessionRuntimeStatusMeta(left.status)
  const rightMeta = getSessionRuntimeStatusMeta(right.status)
  if (leftMeta.order !== rightMeta.order) {
    return leftMeta.order - rightMeta.order
  }
  return parseUpdatedAt(right.updated_at) - parseUpdatedAt(left.updated_at)
}

export function buildSessionRuntimePreview(
  runtime: Partial<SessionRuntimeStateRecord> | null | undefined,
  fallbackPreview?: string | null,
) {
  const failureReason = String(runtime?.failure_reason || '').trim()
  if (failureReason) {
    return failureReason
  }
  const lastError = String(runtime?.last_error || '').trim()
  if (lastError) {
    return lastError
  }
  const preview = String(fallbackPreview || '').trim()
  if (preview) {
    return preview
  }
  return getSessionRuntimeStatusMeta(runtime?.status).emptyPreview
}

export function buildBackgroundTaskItems({
  runningSessionIds = [],
  sessionPendingInterrupts = {},
  sessionRuntimeStates = {},
  sessions = [],
  limit = 8,
}: BuildBackgroundTaskItemsInput): BackgroundTaskItem[] {
  const sessionsById = new Map(sessions.map((session) => [String(session.session_id || '').trim(), session]))
  const itemsById = new Map<string, BackgroundTaskItem>()

  const upsertItem = (sessionId: string, runtimeInput?: Partial<SessionRuntimeStateRecord> | null, fallbackStatus?: string) => {
    const normalizedSessionId = String(sessionId || '').trim()
    if (!normalizedSessionId) {
      return
    }
    const session = sessionsById.get(normalizedSessionId)
    const runtime = normalizeRuntimeRecord(
      normalizedSessionId,
      runtimeInput,
      fallbackStatus,
      String(session?.updated_at || '').trim() || undefined,
    )
    itemsById.set(normalizedSessionId, {
      sessionId: normalizedSessionId,
      title: String(session?.name || '').trim() || `会话 ${normalizedSessionId.slice(0, 8)}`,
      preview: buildSessionRuntimePreview(runtime, session?.last_message_preview),
      interruptCount: Array.isArray(sessionPendingInterrupts[normalizedSessionId])
        ? sessionPendingInterrupts[normalizedSessionId]!.length
        : 0,
      runtime,
    })
  }

  for (const [sessionId, runtime] of Object.entries(sessionRuntimeStates)) {
    upsertItem(sessionId, runtime)
  }

  for (const sessionId of runningSessionIds) {
    if (!itemsById.has(sessionId)) {
      upsertItem(sessionId, null, 'running')
    }
  }

  return Array.from(itemsById.values())
    .sort((left, right) => compareSessionRuntimeRecords(left.runtime, right.runtime))
    .slice(0, Math.max(limit, 0))
}
