import { afterEach, describe, expect, it, vi } from 'vitest'
import type { SessionMessageRecord, SessionRecord } from '@/lib/api'
import type { Message } from '@/types/message'
import {
  allMessageTypes,
  buildSessionMessageDedupKey,
  normalizeWorkflowToolStatus,
  reconcileWorkflowBlocksWithTaskListUpdate,
  shouldPreserveTaskListUpdateCompletion,
  estimateContextStats,
  isKnownMessageType,
  knownMessageTypeSet,
  mergeSessionMessages,
  roleToMessageType,
  toChatMessages,
  toContextMessages,
  toMessagesFromCompressedContext,
  toSessionMessage,
  toStringList,
  toTimestamp,
  toWorkflowBlocks,
} from '@/lib/chat-message-utils'
import {
  formatInterruptDecisionMessage,
  sanitizeInterruptPayload,
} from '@/lib/interrupt-utils'

afterEach(() => {
  vi.restoreAllMocks()
})

describe('chat-message-utils', () => {
  it('暴露完整消息类型集合并支持类型推断', () => {
    expect(allMessageTypes).toEqual([
      'user',
      'assistant',
      'status',
      'thinking',
      'tool',
      'agent',
      'error',
      'decision',
      'interrupt',
      'context',
      'phase_transition',
    ])
    expect(knownMessageTypeSet.has('assistant')).toBe(true)
    expect(isKnownMessageType('tool')).toBe(true)
    expect(isKnownMessageType('unknown')).toBe(false)
    expect(roleToMessageType('system', 'tool')).toBe('tool')
    expect(roleToMessageType('USER')).toBe('user')
    expect(roleToMessageType('assistant')).toBe('assistant')
    expect(roleToMessageType('other')).toBe('status')
  })

  it('toTimestamp 能解析时间并在无效输入时回退到当前时间', () => {
    vi.spyOn(Date, 'now').mockReturnValue(1700000000123)

    expect(toTimestamp('2024-01-02T03:04:05.000Z')).toBe(new Date('2024-01-02T03:04:05.000Z').getTime())
    expect(toTimestamp('bad-timestamp')).toBe(1700000000123)
    expect(toTimestamp(undefined)).toBe(1700000000123)
  })

  it('toChatMessages 会把 SessionRecord 转成消息对象并保留扩展字段', () => {
    vi.spyOn(Date, 'now').mockReturnValue(1700000000999)

    const session: SessionRecord = {
      session_id: 'sid-1',
      name: 'Test Session',
      created_at: '2024-01-01T00:00:00.000Z',
      updated_at: '2024-01-01T00:00:01.000Z',
      messages: [
        {
          role: 'assistant',
          content: 'hello',
          timestamp: '2024-01-02T03:04:05.000Z',
          msg_type: 'tool',
          agent: 'planner',
          agent_display_name: 'Planner',
          phase: 'analysis',
          workflow_block_id: 'wf-1',
          data: { ok: true },
        },
        {
          role: 'user',
          content: 'next',
          timestamp: 'invalid',
        },
      ],
    }

    expect(toChatMessages(session)).toEqual([
      {
        id: 'session-sid-1-0-2024-01-02T03:04:05.000Z',
        type: 'tool',
        content: 'hello',
        timestamp: new Date('2024-01-02T03:04:05.000Z').getTime(),
        agent: 'planner',
        agentDisplayName: 'Planner',
        phase: 'analysis',
        workflowBlockId: 'wf-1',
        data: { ok: true },
      },
      {
        id: 'session-sid-1-1-invalid',
        type: 'user',
        content: 'next',
        timestamp: 1700000000999,
      },
    ])
  })

  it('toSessionMessage 和 buildSessionMessageDedupKey 会生成稳定记录键', () => {
    const message: Message = {
      id: 'm-1',
      type: 'assistant',
      content: 'result',
      timestamp: Date.parse('2024-01-02T03:04:05.000Z'),
      agent: 'builder',
      agentDisplayName: 'Builder',
      phase: 'build',
      workflowBlockId: 'wf-2',
      data: { output: 'done' },
    }

    const record = toSessionMessage(message)
    expect(record).toEqual<SessionMessageRecord>({
      role: 'assistant',
      content: 'result',
      timestamp: '2024-01-02T03:04:05.000Z',
      msg_type: 'assistant',
      agent: 'builder',
      agent_display_name: 'Builder',
      phase: 'build',
      workflow_block_id: 'wf-2',
      data: { output: 'done' },
    })
    expect(buildSessionMessageDedupKey(record)).toBe('assistant::2024-01-02T03:04:05.000Z::result')
  })

  it('mergeSessionMessages 会去重并按时间排序', () => {
    const baseMessages: Message[] = [
      { id: 'm-1', type: 'assistant', content: 'same', timestamp: 2000 },
      { id: 'm-2', type: 'user', content: 'later', timestamp: 4000 },
    ]
    const cachedMessages: Message[] = [
      { id: 'm-3', type: 'assistant', content: 'same', timestamp: 2000 },
      { id: 'm-4', type: 'status', content: 'earlier', timestamp: 1000 },
    ]

    expect(mergeSessionMessages(baseMessages, cachedMessages)).toEqual([
      { id: 'm-4', type: 'status', content: 'earlier', timestamp: 1000 },
      { id: 'm-1', type: 'assistant', content: 'same', timestamp: 2000 },
      { id: 'm-2', type: 'user', content: 'later', timestamp: 4000 },
    ])
  })

  it('toContextMessages、toStringList 和 estimateContextStats 只保留上下文相关信息', () => {
    const messages: Message[] = [
      { id: 'm-1', type: 'user', content: 'hello', timestamp: 1 },
      { id: 'm-2', type: 'tool', content: 'skip', timestamp: 2 },
      { id: 'm-3', type: 'assistant', content: 'world!', timestamp: 3 },
      { id: 'm-4', type: 'status', content: 'ok', timestamp: 4 },
    ]

    expect(toContextMessages(messages)).toEqual([
      { role: 'user', content: 'hello' },
      { role: 'assistant', content: 'world!' },
      { role: 'system', content: 'ok' },
    ])
    expect(toStringList([' a ', '', null, 'b', '  '])).toEqual(['a', 'b'])
    expect(estimateContextStats(messages)).toEqual({
      messageCount: 3,
      totalChars: 13,
      estimatedTokens: 4,
    })
  })

  it('toMessagesFromCompressedContext 会把压缩上下文转换为消息列表', () => {
    vi.spyOn(Date, 'now').mockReturnValue(5000)

    expect(
      toMessagesFromCompressedContext([
        { role: 'user', content: 'hello' },
        { role: 'assistant', content: 'world' },
        null,
      ]),
    ).toEqual([
      { id: 'compressed-5000-0', type: 'user', content: 'hello', timestamp: 5000 },
      { id: 'compressed-5000-1', type: 'assistant', content: 'world', timestamp: 5001 },
    ])
  })

  it('toWorkflowBlocks 会规范化 block、entry 和 toolCard 字段', () => {
    vi.spyOn(Date, 'now').mockReturnValue(7000)

    expect(
      toWorkflowBlocks([
        {
          id: 'wf-1',
          agent: 'planner',
          agentDisplayName: 'Planner',
          summary: '处理中',
          status: 'running',
          startedAt: 100,
          updatedAt: 200,
          finishedAt: 300,
          entries: [
            {
              id: 'entry-1',
              type: 'tool',
              content: '调用工具',
              phase: 'analysis',
              timestamp: 150,
              data: { important: true },
              toolCard: {
                id: 'tool-1',
                tool: 'buffer',
                status: 'failed',
                args: { distance: 10 },
                result: 'error',
                statusSource: 'runtime',
                success: false,
                startedAt: 120,
                finishedAt: 180,
              },
            },
            {
              content: 'fallback',
              type: 'unknown',
            },
          ],
        },
      ]),
    ).toEqual([
      {
        id: 'wf-1',
        agent: 'planner',
        agentDisplayName: 'Planner',
        summary: '处理中',
        status: 'running',
        startedAt: 100,
        updatedAt: 200,
        finishedAt: 300,
        entries: [
          {
            id: 'entry-1',
            type: 'tool',
            content: '调用工具',
            phase: 'analysis',
            timestamp: 150,
            data: { important: true },
            toolCard: {
              id: 'tool-1',
              tool: 'buffer',
              status: 'failed',
              args: { distance: 10 },
              result: 'error',
              statusSource: 'runtime',
              success: false,
              startedAt: 120,
              finishedAt: 180,
            },
          },
          {
            id: 'entry-0-1-7000',
            type: 'status',
            content: 'fallback',
            phase: undefined,
            timestamp: 7000,
            data: undefined,
            toolCard: undefined,
          },
        ],
      },
    ])
  })

  it('toWorkflowBlocks 会把 reload 路径中的终态别名统一映射为稳定状态', () => {
    vi.spyOn(Date, 'now').mockReturnValue(7100)

    expect(
      toWorkflowBlocks([
        {
          id: 'wf-2',
          agent: 'builder',
          summary: '执行完成',
          status: 'done',
          startedAt: 100,
          updatedAt: 200,
          entries: [
            {
              id: 'entry-2',
              type: 'tool',
              content: '工具返回',
              timestamp: 150,
              toolCard: {
                id: 'tool-2',
                tool: 'buffer',
                status: 'success',
                startedAt: 120,
                finishedAt: 180,
              },
            },
            {
              id: 'entry-3',
              type: 'tool',
              content: '工具失败',
              timestamp: 160,
              toolCard: {
                id: 'tool-3',
                tool: 'clip',
                status: 'blocked',
                startedAt: 130,
                finishedAt: 190,
              },
            },
          ],
        },
      ]),
    ).toEqual([
      expect.objectContaining({
        id: 'wf-2',
        status: 'completed',
        entries: [
          expect.objectContaining({
            id: 'entry-2',
            toolCard: expect.objectContaining({
              id: 'tool-2',
              status: 'completed',
            }),
          }),
          expect.objectContaining({
            id: 'entry-3',
            toolCard: expect.objectContaining({
              id: 'tool-3',
              status: 'failed',
            }),
          }),
        ],
      }),
    ])
  })

  it('normalizeWorkflowToolStatus 会在缺少显式状态时回退到 success 字段', () => {
    expect(normalizeWorkflowToolStatus(undefined, true)).toBe('completed')
    expect(normalizeWorkflowToolStatus(undefined, false)).toBe('failed')
    expect(normalizeWorkflowToolStatus('processing', false)).toBe('running')
  })

  it('toWorkflowBlocks 会在 toolCard 缺少 status 时根据 success 推断终态', () => {
    vi.spyOn(Date, 'now').mockReturnValue(7200)

    expect(
      toWorkflowBlocks([
        {
          id: 'wf-3',
          agent: 'builder',
          summary: '处理工具结果',
          status: 'completed',
          startedAt: 100,
          updatedAt: 200,
          entries: [
            {
              id: 'entry-4',
              type: 'tool',
              content: '写入任务列表',
              timestamp: 150,
              toolCard: {
                id: 'tool-4',
                tool: 'write_todos',
                success: true,
                startedAt: 120,
                finishedAt: 180,
              },
            },
            {
              id: 'entry-5',
              type: 'tool',
              content: '工具执行失败',
              timestamp: 160,
              toolCard: {
                id: 'tool-5',
                tool: 'buffer',
                success: false,
                startedAt: 130,
                finishedAt: 190,
              },
            },
          ],
        },
      ]),
    ).toEqual([
      expect.objectContaining({
        id: 'wf-3',
        entries: [
          expect.objectContaining({
            id: 'entry-4',
            toolCard: expect.objectContaining({
              id: 'tool-4',
              status: 'completed',
              success: true,
            }),
          }),
          expect.objectContaining({
            id: 'entry-5',
            toolCard: expect.objectContaining({
              id: 'tool-5',
              status: 'failed',
              success: false,
            }),
          }),
        ],
      }),
    ])
  })

  it('reconcileWorkflowBlocksWithTaskListUpdate 会把 write_todos 的运行中卡片收敛为完成态', () => {
    expect(
      reconcileWorkflowBlocksWithTaskListUpdate([
        {
          id: 'wf-4',
          agent: 'planner',
          agentDisplayName: 'Planner',
          summary: '写入任务列表',
          status: 'running',
          startedAt: 100,
          updatedAt: 200,
          entries: [
            {
              id: 'entry-6',
              type: 'tool',
              content: '调用工具: write_todos',
              phase: 'tool_call',
              timestamp: 150,
              toolCard: {
                id: 'tool-6',
                tool: 'write_todos',
                status: 'running',
                startedAt: 120,
              },
            },
            {
              id: 'entry-7',
              type: 'tool',
              content: '调用工具: buffer',
              phase: 'tool_call',
              timestamp: 160,
              toolCard: {
                id: 'tool-7',
                tool: 'buffer',
                status: 'running',
                startedAt: 130,
              },
            },
          ],
        },
      ], 5000),
    ).toEqual([
      {
        id: 'wf-4',
        agent: 'planner',
        agentDisplayName: 'Planner',
        summary: '写入任务列表',
        status: 'running',
        startedAt: 100,
        updatedAt: 5000,
        entries: [
          {
            id: 'entry-6',
            type: 'tool',
            content: '调用工具: write_todos',
            phase: 'tool_result',
            timestamp: 5000,
            toolCard: {
              id: 'tool-6',
              tool: 'write_todos',
              status: 'completed',
              statusSource: 'task_list_update',
              success: true,
              startedAt: 120,
              finishedAt: 5000,
            },
          },
          {
            id: 'entry-7',
            type: 'tool',
            content: '调用工具: buffer',
            phase: 'tool_call',
            timestamp: 160,
            toolCard: {
              id: 'tool-7',
              tool: 'buffer',
              status: 'running',
              startedAt: 130,
            },
          },
        ],
      },
    ])
  })

  it('shouldPreserveTaskListUpdateCompletion 会阻止 write_todos 的未知终态把已完成卡片重新打回运行中', () => {
    expect(
      shouldPreserveTaskListUpdateCompletion(
        {
          id: 'existing-tool',
          tool: 'write_todos',
          status: 'completed',
          statusSource: 'task_list_update',
          success: true,
          startedAt: 100,
          finishedAt: 200,
        },
        {
          id: 'incoming-tool',
          tool: 'write_todos',
          status: 'running',
          statusSource: 'unknown',
          startedAt: 100,
        },
      ),
    ).toBe(true)

    expect(
      shouldPreserveTaskListUpdateCompletion(
        {
          id: 'existing-tool',
          tool: 'buffer',
          status: 'completed',
          statusSource: 'task_list_update',
          success: true,
          startedAt: 100,
          finishedAt: 200,
        },
        {
          id: 'incoming-tool',
          tool: 'buffer',
          status: 'running',
          statusSource: 'unknown',
          startedAt: 100,
        },
      ),
    ).toBe(false)
  })
})

describe('interrupt-utils', () => {
  it('sanitizeInterruptPayload 会移除自定义输入占位值', () => {
    expect(
      sanitizeInterruptPayload({
        option: '__custom_input__',
        options: ['保留', '__custom_input__', '  '],
        decisions: {
          choose: {
            selected: ['A', '__custom_input__'],
            note: 'memo',
          },
        },
      }),
    ).toEqual({
      options: ['保留'],
      decisions: {
        choose: {
          selected: ['A'],
          note: 'memo',
        },
      },
    })
  })

  it('formatInterruptDecisionMessage 会格式化多决策摘要', () => {
    expect(
      formatInterruptDecisionMessage('cp-1', {
        decisions: {
          route: {
            selected: ['A', '__custom_input__'],
            text: '手动输入',
            note: '优先',
            confirmed: true,
          },
        },
      }),
    ).toBe('决策结果（cp-1）\n- Q1（route）: 选择: A；确认: 是；自定义: 手动输入；补充: 优先')
  })

  it('formatInterruptDecisionMessage 在没有结构化负载时返回默认文案', () => {
    expect(formatInterruptDecisionMessage('', undefined)).toBe(
      '决策结果（pending checkpoint）\n- 已确认并继续执行。',
    )
  })
})
