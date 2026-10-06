import { SessionRecord } from '@/lib/api'

interface SessionFetchRetryOptions {
  retryCount?: number
  initialDelayMs?: number
}

const DEFAULT_RETRY_COUNT = 2
const DEFAULT_RETRY_DELAY_MS = 250

function parseApiErrorStatus(message: string) {
  const matched = message.match(/api error\s+(\d{3})/i)
  if (!matched) {
    return 0
  }
  return Number(matched[1] || 0)
}

function shouldRetrySessionFetch(error: unknown) {
  if (!(error instanceof Error)) {
    return true
  }
  const message = error.message.toLowerCase()
  if (!message) {
    return true
  }

  const status = parseApiErrorStatus(message)
  if (status >= 500 || status === 408 || status === 429) {
    return true
  }

  return (
    message.includes('failed to fetch')
    || message.includes('networkerror')
    || message.includes('network request')
    || message.includes('timeout')
    || message.includes('abort')
    || message.includes('econnrefused')
    || message.includes('socket')
  )
}

async function fetchSessionsWithRetry(
  fetcher: (projectPath?: string) => Promise<SessionRecord[]>,
  projectPath: string | undefined,
  options?: SessionFetchRetryOptions,
) {
  const retryCount = Number.isFinite(options?.retryCount) ? Math.max(0, Math.round(options?.retryCount || 0)) : DEFAULT_RETRY_COUNT
  const initialDelayMs = Number.isFinite(options?.initialDelayMs)
    ? Math.max(50, Math.round(options?.initialDelayMs || 0))
    : DEFAULT_RETRY_DELAY_MS

  let delayMs = initialDelayMs
  let lastError: unknown
  for (let attempt = 0; attempt <= retryCount; attempt += 1) {
    try {
      return await fetcher(projectPath)
    } catch (error) {
      lastError = error
      if (attempt >= retryCount || !shouldRetrySessionFetch(error)) {
        throw error
      }
      await new Promise((resolve) => setTimeout(resolve, delayMs))
      delayMs = Math.min(delayMs * 2, 2000)
    }
  }

  throw lastError instanceof Error ? lastError : new Error('会话列表加载失败')
}

export interface SessionSnapshot {
  snapshot_id: string
  created_at: string
  reason?: string
  messages_count?: number
  token_count?: number
  messages_backup?: unknown[]
  tool_calls_backup?: unknown[]
}

export function parseSnapshots(raw: unknown): SessionSnapshot[] {
  if (!Array.isArray(raw)) {
    return []
  }
  return raw
    .filter((item) => item && typeof item === 'object')
    .map((item) => item as SessionSnapshot)
    .filter((item) => Boolean(item.snapshot_id))
    .sort((a, b) => new Date(b.created_at || 0).getTime() - new Date(a.created_at || 0).getTime())
}

export function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object') {
    return {}
  }
  return value as Record<string, unknown>
}

export function asBool(value: unknown, fallback: boolean) {
  if (typeof value === 'boolean') {
    return value
  }
  return fallback
}

export function asInt(value: unknown, fallback: number) {
  if (typeof value === 'number' && Number.isFinite(value)) {
    return Math.max(1, Math.round(value))
  }
  return fallback
}

export function sanitizeFileName(input: string) {
  const raw = input.trim()
  if (!raw) {
    return 'session'
  }
  return raw.replace(/[\\/:*?"<>|]/g, '_')
}

export async function loadSessionsForProject(
  fetcher: (projectPath?: string) => Promise<SessionRecord[]>,
  projectPath: string,
  options?: SessionFetchRetryOptions,
) {
	const normalizedProjectPath = projectPath.trim()
	if (!normalizedProjectPath) {
		return []
	}
	return fetchSessionsWithRetry(fetcher, normalizedProjectPath, options)
}
