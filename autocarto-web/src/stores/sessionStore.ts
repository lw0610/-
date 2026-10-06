import { create } from 'zustand'
import { SessionRecord } from '@/lib/api'
import type { SessionAttachStatus } from '@/lib/sessionReattach'

interface SessionState {
  sessions: SessionRecord[]
  sessionsReady: boolean
  activeSessionId: string | null
  sessionMutationTick: number
  lastMutatedSessionId: string | null
  sessionAttachState: Record<string, SessionAttachStatus>
  setSessions: (sessions: SessionRecord[]) => void
  upsertSession: (session: SessionRecord) => void
  setActiveSession: (id: string | null) => void
  removeSession: (id: string) => void
  setSessionAttachStates: (state: Record<string, SessionAttachStatus>) => void
  setSessionAttachStatus: (id: string, status: SessionAttachStatus) => void
  clearSessionAttachState: (id: string) => void
  clearSessions: () => void
  notifySessionMutated: (id: string) => void
}

function sortSessions(list: SessionRecord[]) {
  return [...list].sort((a, b) => {
    const ta = new Date(a.updated_at || a.created_at || 0).getTime()
    const tb = new Date(b.updated_at || b.created_at || 0).getTime()
    return tb - ta
  })
}

export const useSessionStore = create<SessionState>((set) => ({
  sessions: [],
  sessionsReady: false,
  activeSessionId: null as string | null,
  sessionMutationTick: 0,
  lastMutatedSessionId: null,
  sessionAttachState: {},
  setSessions: (sessions) =>
    set((state) => {
      const sorted = sortSessions(sessions)
      const currentActive = state.activeSessionId

      let activeSessionId: string | null = null
      if (currentActive && sorted.some((s) => s.session_id === currentActive)) {
        activeSessionId = currentActive
      }

      const validSessionIds = new Set(sorted.map((item) => item.session_id))
      const nextAttachState = Object.fromEntries(
        Object.entries(state.sessionAttachState).filter(([sessionId]) => validSessionIds.has(sessionId)),
      ) as Record<string, SessionAttachStatus>
      return {
        sessions: sorted,
        sessionsReady: true,
        activeSessionId,
        sessionAttachState: nextAttachState,
      }
    }),
  upsertSession: (session) =>
    set((state) => {
      const next = [...state.sessions]
      const idx = next.findIndex((s) => s.session_id === session.session_id)
      if (idx >= 0) {
        next[idx] = session
      } else {
        next.push(session)
      }
      return {
        sessions: sortSessions(next),
        sessionsReady: true,
      }
    }),
  setSessionAttachStates: (sessionAttachState) =>
    set(() => ({
      sessionAttachState: { ...sessionAttachState },
    })),
  setSessionAttachStatus: (id, status) =>
    set((state) => {
      const sessionId = id.trim()
      if (!sessionId) {
        return state
      }
      return {
        sessionAttachState: {
          ...state.sessionAttachState,
          [sessionId]: status,
        },
      }
    }),
  clearSessionAttachState: (id) =>
    set((state) => {
      const sessionId = id.trim()
      if (!sessionId || !(sessionId in state.sessionAttachState)) {
        return state
      }
      const nextAttachState = { ...state.sessionAttachState }
      delete nextAttachState[sessionId]
      return {
        sessionAttachState: nextAttachState,
      }
    }),
  setActiveSession: (activeSessionId) => set({ activeSessionId }),
  removeSession: (id) =>
    set((state) => {
      const sessions = state.sessions.filter((s) => s.session_id !== id)
      const activeSessionId = state.activeSessionId === id ? null : state.activeSessionId
      const nextAttachState = { ...state.sessionAttachState }
      delete nextAttachState[id]
      return {
        sessions,
        activeSessionId,
        sessionAttachState: nextAttachState,
      }
    }),
  clearSessions: () => set({ sessions: [], activeSessionId: null, sessionAttachState: {} }),
  notifySessionMutated: (id) =>
    set((state) => ({
      sessionMutationTick: state.sessionMutationTick + 1,
      lastMutatedSessionId: id,
    })),
}))

export function getActiveSessionFromStore() {
  const { sessions, activeSessionId } = useSessionStore.getState()
  if (!activeSessionId) {
    return null
  }
  return sessions.find((s) => s.session_id === activeSessionId) ?? null
}
