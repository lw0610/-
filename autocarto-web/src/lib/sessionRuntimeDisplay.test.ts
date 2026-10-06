import { test, expect } from 'vitest'

import { buildBackgroundTaskItems } from './sessionRuntimeDisplay'

test('buildBackgroundTaskItems merges active and terminal runtimes with shared preview rules', () => {
  const items = buildBackgroundTaskItems({
    runningSessionIds: ['sess-running'],
    sessionPendingInterrupts: {
      'sess-running': [{ id: 'i-1' }, { id: 'i-2' }],
    },
    sessionRuntimeStates: {
      'sess-running': {
        session_id: 'sess-running',
        status: 'running',
        bound_instance_id: 'inst-a',
        updated_at: '2026-03-13T10:01:00Z',
      },
      'sess-failed': {
        session_id: 'sess-failed',
        status: 'failed',
        failure_reason: '绑定实例断开',
        updated_at: '2026-03-13T09:01:00Z',
      },
      'sess-completed': {
        session_id: 'sess-completed',
        status: 'completed',
        updated_at: '2026-03-13T08:01:00Z',
      },
    },
    sessions: [
      {
        session_id: 'sess-running',
        name: '运行会话',
        last_message_preview: '处理中',
        updated_at: '2026-03-13T10:00:00Z',
      },
      {
        session_id: 'sess-failed',
        name: '失败会话',
        last_message_preview: '旧摘要',
        updated_at: '2026-03-13T09:00:00Z',
      },
      {
        session_id: 'sess-completed',
        name: '完成会话',
        updated_at: '2026-03-13T08:00:00Z',
      },
    ],
  })

  expect(items.map((item) => item.sessionId)).toEqual(['sess-running', 'sess-failed', 'sess-completed'])
  expect(items[0]?.preview).toBe('处理中')
  expect(items[0]?.interruptCount).toBe(2)
  expect(items[0]?.runtime.bound_instance_id).toBe('inst-a')
  expect(items[1]?.preview).toBe('绑定实例断开')
  expect(items[2]?.preview).toBe('会话运行已完成')
})

test('buildBackgroundTaskItems falls back to running status when runtime snapshot is missing', () => {
  const items = buildBackgroundTaskItems({
    runningSessionIds: ['sess-missing'],
    sessionRuntimeStates: {},
    sessions: [
      {
        session_id: 'sess-missing',
        name: '缺失运行态',
        last_message_preview: '仍在执行',
        updated_at: '2026-03-13T11:00:00Z',
      },
    ],
  })

  expect(items.length).toBe(1)
  expect(items[0]?.sessionId).toBe('sess-missing')
  expect(items[0]?.runtime.status).toBe('running')
  expect(items[0]?.preview).toBe('仍在执行')
})
