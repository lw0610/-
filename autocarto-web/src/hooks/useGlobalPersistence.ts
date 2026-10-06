'use client'

import { useCallback, useEffect, useMemo, useRef } from 'react'
import api, { SessionMessageRecord, SessionRecord } from '@/lib/api'
import { executionModeToRunIntent, resolveExecutionMode } from '@/lib/runIntent'
import { useChatStore } from '@/stores/chatStore'
import { useSessionStore } from '@/stores/sessionStore'
import { ExecutionMode, Message, WorkflowBlock } from '@/types/message'

const MAX_PERSIST_RETRIES = 3
const PERSIST_DEBOUNCE_MS = 500

const persistableMessageTypeSet = new Set<Message['type']>([
  'user',
  'assistant',
  'decision',
  'phase_transition',
  'error',
  'context',
  'status',
  'agent',
])

function normalizeSessionId(value: string | null | undefined) {
  return String(value || '').trim()
}

function buildSessionMessageDedupKey(message: SessionMessageRecord) {
  const type = String(message.msg_type || message.role || 'status')
  const content = String(message.content || '')
  const timestamp = typeof message.timestamp === 'string' ? message.timestamp : ''
  return `${type}::${timestamp}::${content}`
}


function toSessionMessage(msg: Message): SessionMessageRecord {
  const record: SessionMessageRecord = {
    role: msg.type === 'user' ? 'user' : msg.type === 'assistant' ? 'assistant' : 'system',
    content: msg.content,
    timestamp: new Date(msg.timestamp).toISOString(),
    msg_type: msg.type,
  }

  if (msg.agent) {
    record.agent = msg.agent
  }
  if (msg.agentDisplayName) {
    record.agent_display_name = msg.agentDisplayName
  }
  if (msg.phase) {
    record.phase = msg.phase
  }
  if (msg.workflowBlockId) {
    record.workflow_block_id = msg.workflowBlockId
  }
  if (msg.data && typeof msg.data === 'object' && !Array.isArray(msg.data)) {
    record.data = msg.data
  }

  return record
}

export interface SessionLoadedPayload {
  messages: Message[]
  persistedMessages?: Message[]
  workflowBlocks: WorkflowBlock[]
  executionMode?: ExecutionMode
  sessionRecord?: SessionRecord | null
}

export interface GlobalPersistenceController {
  markSessionLoaded: (sessionId: string, payload: SessionLoadedPayload) => void
  markSessionDeleted: (sessionId: string) => void
  clearSession: (sessionId: string) => void
  flushSession: (sessionId: string) => Promise<void>
  flushAll: () => Promise<void>
}

const noopAsync = async () => {}

const noopController: GlobalPersistenceController = {
  markSessionLoaded: () => {},
  markSessionDeleted: () => {},
  clearSession: () => {},
  flushSession: noopAsync,
  flushAll: noopAsync,
}

let sharedController: GlobalPersistenceController = noopController

export function getGlobalPersistenceController() {
  return sharedController
}

export function useGlobalPersistence(projectPath: string) {
  const sessionId = useChatStore((s) => s.sessionId)
  const sessionExecutionMode = useChatStore((s) => s.sessionExecutionMode)
  const sessionMessages = useChatStore((s) => s.sessionMessages)
  const upsertSession = useSessionStore((s) => s.upsertSession)

  const activeSessionIdRef = useRef(sessionId)
  const sessionExecutionModeRef = useRef(sessionExecutionMode)
  const projectPathRef = useRef(projectPath)

  const persistedMessageIdsRef = useRef<Record<string, Set<string>>>({})
  const pendingMessagesRef = useRef<Record<string, Map<string, Message>>>({})
  const persistTimersRef = useRef<Record<string, ReturnType<typeof setTimeout> | null>>({})
  const retryCountsRef = useRef<Record<string, number>>({})
  const sessionCacheRef = useRef<Record<string, SessionRecord>>({})
  const modeOverridesRef = useRef<Record<string, ExecutionMode | undefined>>({})
  const deletedSessionIdsRef = useRef<Set<string>>(new Set())

  useEffect(() => {
    activeSessionIdRef.current = sessionId
  }, [sessionId])

  useEffect(() => {
    sessionExecutionModeRef.current = sessionExecutionMode
  }, [sessionExecutionMode])

  useEffect(() => {
    projectPathRef.current = projectPath
  }, [projectPath])

  const clearSession = useCallback((sessionIdRaw: string) => {
    const sid = normalizeSessionId(sessionIdRaw)
    if (!sid) {
      return
    }

    const timer = persistTimersRef.current[sid]
    if (timer) {
      clearTimeout(timer)
    }

    delete persistTimersRef.current[sid]
    delete pendingMessagesRef.current[sid]
    delete persistedMessageIdsRef.current[sid]
    delete retryCountsRef.current[sid]
    delete sessionCacheRef.current[sid]
    delete modeOverridesRef.current[sid]
  }, [])

  const ensureSessionRecord = useCallback(async (
    sessionIdRaw: string,
    options?: { forceRefresh?: boolean },
  ) => {
    const sid = normalizeSessionId(sessionIdRaw)
    if (!sid) {
      return null
    }

    const forceRefresh = Boolean(options?.forceRefresh)
    const cached = sessionCacheRef.current[sid]
    if (cached && !forceRefresh) {
      return cached
    }

    const preferredPath = String(projectPathRef.current || '').trim()
    if (!preferredPath) {
      return null
    }
    try {
      const loaded = await api.getSession(sid, preferredPath)
      sessionCacheRef.current[sid] = loaded
      return loaded
    } catch (error) {
      void error
      return null
    }
  }, [])

  const persistSession = useCallback(async (sessionIdRaw: string) => {
    const sid = normalizeSessionId(sessionIdRaw)
    if (!sid) {
      return
    }

    if (deletedSessionIdsRef.current.has(sid)) {
      const revived = await ensureSessionRecord(sid)
      if (!revived) {
        return
      }
      deletedSessionIdsRef.current.delete(sid)
    }

    const pendingMap = pendingMessagesRef.current[sid]
    const pending = pendingMap ? Array.from(pendingMap.values()) : []
    if (!pending.length) {
      return
    }

    const session = await ensureSessionRecord(sid, { forceRefresh: true })
    if (!session) {
      retryCountsRef.current[sid] = (retryCountsRef.current[sid] || 0) + 1
      return
    }

    const currentMessages = Array.isArray(session.messages) ? session.messages : []
    const dedupKeys = new Set(currentMessages.map((item) => buildSessionMessageDedupKey(item)))
    const updatedMessages = [...currentMessages]
    let previewSource: Message | null = null

    for (const msg of pending) {
      const record = toSessionMessage(msg)
      const dedupKey = buildSessionMessageDedupKey(record)
      if (dedupKeys.has(dedupKey)) {
        continue
      }
      dedupKeys.add(dedupKey)
      updatedMessages.push(record)
      previewSource = msg
    }

    const nextMode = modeOverridesRef.current[sid]
      || sessionExecutionModeRef.current[sid]
      || resolveExecutionMode(session.execution_mode, session.run_intent)

    const updated: SessionRecord = {
      ...session,
      messages: updatedMessages,
      updated_at: new Date().toISOString(),
    }

    if (nextMode) {
      updated.execution_mode = nextMode
      updated.run_intent = executionModeToRunIntent(nextMode)
    }

    if (previewSource) {
      const preview = previewSource.content.length > 100
        ? `${previewSource.content.slice(0, 100)}...`
        : previewSource.content
      updated.last_message_preview = `[${previewSource.type}] ${preview}`
    }

    const saveProjectPath = String(projectPathRef.current || '').trim()
    if (!saveProjectPath) {
      return
    }
    await api.saveSession(sid, updated, saveProjectPath)
    sessionCacheRef.current[sid] = updated
    upsertSession(updated)

    if (pendingMap) {
      pendingMap.clear()
    }
    retryCountsRef.current[sid] = 0
  }, [ensureSessionRecord, upsertSession])

  const schedulePersist = useCallback((sessionIdRaw: string) => {
    const sid = normalizeSessionId(sessionIdRaw)
    if (!sid) {
      return
    }

    const pendingSize = pendingMessagesRef.current[sid]?.size || 0
    if (!pendingSize) {
      return
    }

    const existingTimer = persistTimersRef.current[sid]
    if (existingTimer) {
      clearTimeout(existingTimer)
    }

    persistTimersRef.current[sid] = setTimeout(() => {
      persistTimersRef.current[sid] = null
      void persistSession(sid).catch(() => {
        const nextRetry = (retryCountsRef.current[sid] || 0) + 1
        retryCountsRef.current[sid] = nextRetry
        if (nextRetry <= MAX_PERSIST_RETRIES) {
          schedulePersist(sid)
        }
      })
    }, PERSIST_DEBOUNCE_MS)
  }, [persistSession])

  useEffect(() => {
    const sessionEntries = Object.entries(sessionMessages)
    for (const [sidRaw, messages] of sessionEntries) {
      const sid = normalizeSessionId(sidRaw)
      if (!sid || !Array.isArray(messages)) {
        continue
      }

      const persistedIds = persistedMessageIdsRef.current[sid] || new Set<string>()
      persistedMessageIdsRef.current[sid] = persistedIds

      const pending = pendingMessagesRef.current[sid] || new Map<string, Message>()
      pendingMessagesRef.current[sid] = pending

      let hasNewPersistable = false
      for (const msg of messages) {
        if (persistedIds.has(msg.id)) {
          continue
        }
        persistedIds.add(msg.id)
        if (!persistableMessageTypeSet.has(msg.type)) {
          continue
        }
        pending.set(msg.id, msg)
        hasNewPersistable = true
      }

      if (hasNewPersistable) {
        retryCountsRef.current[sid] = 0
        schedulePersist(sid)
      }
    }
  }, [schedulePersist, sessionMessages])


  const markSessionLoaded = useCallback((sessionIdRaw: string, payload: SessionLoadedPayload) => {
    const sid = normalizeSessionId(sessionIdRaw)
    if (!sid) {
      return
    }

    deletedSessionIdsRef.current.delete(sid)

    const persistedMessages = payload.persistedMessages ?? payload.messages
    const persistedIds = new Set(persistedMessages.map((msg) => msg.id))
    persistedMessageIdsRef.current[sid] = persistedIds
    const pending = pendingMessagesRef.current[sid] || new Map<string, Message>()
    for (const messageID of Array.from(pending.keys())) {
      if (persistedIds.has(messageID)) {
        pending.delete(messageID)
      }
    }
    pendingMessagesRef.current[sid] = pending
    retryCountsRef.current[sid] = 0

    if (payload.executionMode) {
      modeOverridesRef.current[sid] = payload.executionMode
    }
    if (payload.sessionRecord) {
      sessionCacheRef.current[sid] = payload.sessionRecord
    }
  }, [])

  const markSessionDeleted = useCallback((sessionIdRaw: string) => {
    const sid = normalizeSessionId(sessionIdRaw)
    if (!sid) {
      return
    }
    deletedSessionIdsRef.current.add(sid)
    clearSession(sid)
  }, [clearSession])

  const flushSession = useCallback(async (sessionIdRaw: string) => {
    const sid = normalizeSessionId(sessionIdRaw)
    if (!sid) {
      return
    }
    const timer = persistTimersRef.current[sid]
    if (timer) {
      clearTimeout(timer)
      persistTimersRef.current[sid] = null
    }
    await persistSession(sid)
  }, [persistSession])

  const flushAll = useCallback(async () => {
    const sessionIdSet = new Set<string>()
    Object.keys(pendingMessagesRef.current).forEach((sid) => sessionIdSet.add(sid))

    const tasks: Array<Promise<void>> = []
    sessionIdSet.forEach((sid) => {
      tasks.push(flushSession(sid))
    })
    await Promise.allSettled(tasks)
  }, [flushSession])

  const controller = useMemo<GlobalPersistenceController>(() => ({
    markSessionLoaded,
    markSessionDeleted,
    clearSession,
    flushSession,
    flushAll,
  }), [clearSession, flushAll, flushSession, markSessionDeleted, markSessionLoaded])

  useEffect(() => {
    sharedController = controller
    return () => {
      if (sharedController === controller) {
        sharedController = noopController
      }
    }
  }, [controller])

  useEffect(() => {
    if (typeof window === 'undefined') {
      return
    }
    const onBeforeUnload = () => {
      void flushAll()
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => {
      window.removeEventListener('beforeunload', onBeforeUnload)
      void flushAll()
    }
  }, [flushAll])

  useEffect(() => {
    const currentSessionIds = new Set<string>(Object.keys(sessionMessages))

    for (const sid of Object.keys(sessionCacheRef.current)) {
      if (!currentSessionIds.has(sid)) {
        clearSession(sid)
      }
    }
  }, [clearSession, sessionMessages])
}
