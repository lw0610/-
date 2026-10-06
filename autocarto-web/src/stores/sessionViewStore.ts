import { create } from 'zustand'
import type { SessionRecord } from '@/lib/api'
import type {
  InterruptRequestData,
  Message,
  TodoItem,
  WorkflowBlock,
} from '@/types/message'
import { generateId } from '@/lib/utils'

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return {}
  }
  return value as Record<string, unknown>
}

function normalizeSessionId(value: string | null | undefined) {
  return String(value || '').trim()
}

export interface SessionData {
  messages: Message[]
  workflowBlocks: WorkflowBlock[]
  tasks: TodoItem[]
  pendingInterrupts: InterruptRequestData[]
  isThinking: boolean
  isRunning: boolean
}

function emptySessionData(): SessionData {
  return {
    messages: [],
    workflowBlocks: [],
    tasks: [],
    pendingInterrupts: [],
    isThinking: false,
    isRunning: false,
  }
}

interface SessionViewState {
  sessions: SessionRecord[]
  sessionsReady: boolean
  sessionDataMap: Record<string, SessionData>

  hydrateSessions: (sessions: SessionRecord[]) => void
  upsertSession: (session: SessionRecord) => void
  removeSession: (id: string) => void

  hydrateSessionData: (sessionId: string, data: Partial<SessionData>) => void
  ensureSessionData: (sessionId: string) => void

  addSessionMessage: (sessionId: string, msg: Omit<Message, 'id' | 'timestamp'>) => Message
  setSessionMessages: (sessionId: string, messages: Message[]) => void
  setSessionThinking: (sessionId: string, thinking: boolean) => void
  setSessionRunning: (sessionId: string, running: boolean) => void
  setSessionTasks: (sessionId: string, tasks: TodoItem[]) => void
  setSessionWorkflowBlocks: (sessionId: string, blocks: WorkflowBlock[]) => void
  addSessionInterrupt: (sessionId: string, interrupt: InterruptRequestData) => void
  resolveSessionInterrupt: (sessionId: string, checkpointId: string) => void
  clearSessionInterrupts: (sessionId: string) => void
  clearSessionData: (sessionId: string) => void
  purgeSession: (sessionId: string) => void
}

export const useSessionViewStore = create<SessionViewState>((set, get) => ({
  sessions: [],
  sessionsReady: false,
  sessionDataMap: {},

  hydrateSessions: (sessions) => set({ sessions, sessionsReady: true }),

  upsertSession: (session) => set((state) => {
    const idx = state.sessions.findIndex((s) => s.session_id === session.session_id)
    if (idx >= 0) {
      const next = [...state.sessions]
      next[idx] = session
      return { sessions: next }
    }
    return { sessions: [...state.sessions, session] }
  }),

  removeSession: (id) => set((state) => {
    const nextMap = { ...state.sessionDataMap }
    delete nextMap[id]
    return {
      sessions: state.sessions.filter((s) => s.session_id !== id),
      sessionDataMap: nextMap,
    }
  }),

  hydrateSessionData: (sessionId, data) => set((state) => {
    const sid = normalizeSessionId(sessionId)
    if (!sid) return state
    const existing = state.sessionDataMap[sid] || emptySessionData()
    return {
      sessionDataMap: {
        ...state.sessionDataMap,
        [sid]: { ...existing, ...data },
      },
    }
  }),

  ensureSessionData: (sessionId) => {
    const sid = normalizeSessionId(sessionId)
    if (!sid) return
    const state = get()
    if (state.sessionDataMap[sid]) return
    set({
      sessionDataMap: {
        ...state.sessionDataMap,
        [sid]: emptySessionData(),
      },
    })
  },

  addSessionMessage: (sessionId, msg) => {
    const sid = normalizeSessionId(sessionId)
    const payload: Message = {
      ...msg,
      id: generateId(),
      timestamp: Date.now(),
    }

    if (!sid) return payload

    set((state) => {
      const data = state.sessionDataMap[sid] || emptySessionData()

      if (msg.type === 'interrupt') {
        const incoming = asRecord(msg.data)
        const incomingCheckpoint = String(incoming.checkpoint_id || '').trim()
        if (incomingCheckpoint) {
          const existed = data.messages.find((item) => {
            if (item.type !== 'interrupt') return false
            const p = asRecord(item.data)
            return String(p.checkpoint_id || '').trim() === incomingCheckpoint
          })
          if (existed) return state
        }
      }

      return {
        sessionDataMap: {
          ...state.sessionDataMap,
          [sid]: { ...data, messages: [...data.messages, payload] },
        },
      }
    })

    return payload
  },

  setSessionMessages: (sessionId, messages) => set((state) => {
    const sid = normalizeSessionId(sessionId)
    if (!sid) return state
    const data = state.sessionDataMap[sid] || emptySessionData()
    return {
      sessionDataMap: {
        ...state.sessionDataMap,
        [sid]: { ...data, messages },
      },
    }
  }),

  setSessionThinking: (sessionId, thinking) => set((state) => {
    const sid = normalizeSessionId(sessionId)
    if (!sid) return state
    const data = state.sessionDataMap[sid] || emptySessionData()
    return {
      sessionDataMap: {
        ...state.sessionDataMap,
        [sid]: { ...data, isThinking: thinking },
      },
    }
  }),

  setSessionRunning: (sessionId, running) => set((state) => {
    const sid = normalizeSessionId(sessionId)
    if (!sid) return state
    const data = state.sessionDataMap[sid] || emptySessionData()
    return {
      sessionDataMap: {
        ...state.sessionDataMap,
        [sid]: { ...data, isRunning: running },
      },
    }
  }),

  setSessionTasks: (sessionId, tasks) => set((state) => {
    const sid = normalizeSessionId(sessionId)
    if (!sid) return state
    const data = state.sessionDataMap[sid] || emptySessionData()
    return {
      sessionDataMap: {
        ...state.sessionDataMap,
        [sid]: { ...data, tasks },
      },
    }
  }),

  setSessionWorkflowBlocks: (sessionId, blocks) => set((state) => {
    const sid = normalizeSessionId(sessionId)
    if (!sid) return state
    const data = state.sessionDataMap[sid] || emptySessionData()
    return {
      sessionDataMap: {
        ...state.sessionDataMap,
        [sid]: { ...data, workflowBlocks: blocks },
      },
    }
  }),

  addSessionInterrupt: (sessionId, interrupt) => set((state) => {
    const sid = normalizeSessionId(sessionId)
    if (!sid) return state
    const data = state.sessionDataMap[sid] || emptySessionData()
    const checkpointId = String(interrupt.checkpoint_id || '').trim()
    if (checkpointId) {
      const exists = data.pendingInterrupts.some(
        (i) => String(i.checkpoint_id || '').trim() === checkpointId,
      )
      if (exists) return state
    }
    return {
      sessionDataMap: {
        ...state.sessionDataMap,
        [sid]: { ...data, pendingInterrupts: [...data.pendingInterrupts, interrupt] },
      },
    }
  }),

  resolveSessionInterrupt: (sessionId, checkpointId) => set((state) => {
    const sid = normalizeSessionId(sessionId)
    if (!sid) return state
    const data = state.sessionDataMap[sid]
    if (!data) return state
    return {
      sessionDataMap: {
        ...state.sessionDataMap,
        [sid]: {
          ...data,
          pendingInterrupts: data.pendingInterrupts.filter(
            (i) => String(i.checkpoint_id || '').trim() !== checkpointId,
          ),
        },
      },
    }
  }),

  clearSessionInterrupts: (sessionId) => set((state) => {
    const sid = normalizeSessionId(sessionId)
    if (!sid) return state
    const data = state.sessionDataMap[sid]
    if (!data) return state
    return {
      sessionDataMap: {
        ...state.sessionDataMap,
        [sid]: { ...data, pendingInterrupts: [] },
      },
    }
  }),

  clearSessionData: (sessionId) => set((state) => {
    const sid = normalizeSessionId(sessionId)
    if (!sid) return state
    const nextMap = { ...state.sessionDataMap }
    delete nextMap[sid]
    return { sessionDataMap: nextMap }
  }),

  purgeSession: (sessionId) => set((state) => {
    const sid = normalizeSessionId(sessionId)
    if (!sid) return state
    const nextMap = { ...state.sessionDataMap }
    delete nextMap[sid]
    return {
      sessions: state.sessions.filter((s) => s.session_id !== sid),
      sessionDataMap: nextMap,
    }
  }),
}))
