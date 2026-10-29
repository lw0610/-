import { describe, expect, it } from 'vitest'
import type { Message, WorkflowBlock } from '@/types/message'
import type { FlowRunState } from './types'
import { executionTasks, executionWorkflowBlocks } from './workspace'

const start = '2026-10-28T00:00:00Z'
const timestamp = Date.parse(start)
const run: FlowRunState = {
  api_version: 'flow-run/v1', flow_run_id: 'flow', session_id: 'session', flow_id: '', status: 'failed', started_at: start,
  steps: [{ id: 'prepare', index: 1, title: '整理数据', role: 'data_engineer', status: 'failed', outputs: {},
    attempts: [{ run_id: 'first', started_at: start, finished_at: '2026-10-28T00:01:00Z', receipt: { status: 'failed', summary: '', outputs: {}, issues: [] } },
      { run_id: 'second', started_at: '2026-10-28T00:02:00Z' }] }],
  result: { summary: '', deliverables: {}, issues: [] },
}
const call: Message = { id: 'live-call', type: 'tool', phase: 'tool_call', content: '调用工具', timestamp,
  data: { flow_run_id: 'flow', step_id: 'prepare', run_id: 'first', call_id: 'call1', tool: 'get_layers_info', tool_args: { layer: '人口' } } }
const restored: WorkflowBlock = { id: 'old-block', agent: 'data_engineer', agentDisplayName: '数据工程智能体', summary: '旧摘要', status: 'error', startedAt: timestamp, updatedAt: timestamp + 1000,
  entries: [{ id: 'stored-result', type: 'tool', phase: 'tool_result', content: '真实回执', timestamp: timestamp + 1000,
    data: { ...call.data, result: '真实回执', status: 'completed', success: true },
    toolCard: { id: 'stored-tool', callId: 'call1', tool: 'get_layers_info', status: 'completed', result: '真实回执', startedAt: timestamp, finishedAt: timestamp + 1000 } }] }

describe('right workspace execution projection', () => {
  it('merges live and restored invocations without hiding a distinct retry or rewriting ordinary tasks', () => {
    const ordinary: WorkflowBlock = { ...restored, id: 'ordinary', entries: [] }
    const replay = { ...call, id: 'late-replayed-call', timestamp: timestamp + 130000 }
    const retry = { ...call, id: 'retry', timestamp: timestamp + 130001, data: { ...call.data, run_id: 'second', call_id: 'call2' } }
    const blocks = executionWorkflowBlocks([run], [call, replay, retry], [ordinary, restored])
    expect(blocks.find(block => block.id === 'ordinary')).toBe(ordinary)
    const first = blocks.find(block => block.id === 'execution-flow-prepare-first')!
    expect(first.entries.filter(entry => entry.toolCard)).toHaveLength(1)
    expect(first.entries[0].toolCard).toMatchObject({ status: 'completed', args: { layer: '人口' }, result: '真实回执' })
    expect(blocks.find(block => block.id === 'execution-flow-prepare-second')?.entries[0].toolCard?.callId).toBe('call2')
    expect(executionTasks([run], [], [])).toEqual([{ content: '整理数据', status: 'failed', owner: '数据工程智能体' }])
    expect(restored.entries[0].toolCard?.args).toBeUndefined()
    // Older backend snapshots could discard v1 attempts during replanning.
    // Their recorded call IDs still belong to v1, never to the remaining v2.
    const replanned = { ...run, steps: [{ ...run.steps[0], attempts: [run.steps[0].attempts[1]] }] }
    const historical = executionWorkflowBlocks([replanned], [replay, retry], [restored])
    expect(historical.find(block => block.id === 'execution-flow-prepare-first')?.summary).toContain('历史尝试')
    expect(historical.find(block => block.id === 'execution-flow-prepare-second')?.entries.filter(entry => entry.toolCard)).toHaveLength(1)
    const noAttemptId = { ...restored, entries: restored.entries.map(entry => ({ ...entry, data: { ...entry.data, run_id: undefined } })) }
    const oldHistory = executionWorkflowBlocks([replanned], [replay, retry], [noAttemptId])
    expect(oldHistory.find(block => block.id === 'old-block')).toBe(noAttemptId)
    expect(oldHistory.flatMap(block => block.entries).filter(entry => entry.toolCard?.callId === 'call1')).toHaveLength(1)
    expect(oldHistory.find(block => block.id === 'execution-flow-prepare-second')?.entries.filter(entry => entry.toolCard)).toHaveLength(1)
  })
  it('shows live tool call and result in the same existing process card before history is restored', () => {
    const result: Message = { ...call, id: 'live-result', phase: 'tool_result', content: '返回值', timestamp: timestamp + 1000, data: { ...call.data, status: 'completed', success: true, result: '返回值' } }
    const blocks = executionWorkflowBlocks([run], [call, result], [])
    const first = blocks.find(block => block.id === 'execution-flow-prepare-first')!
    expect(first.entries).toHaveLength(1)
    expect(first.entries[0].toolCard).toMatchObject({ status: 'completed', args: { layer: '人口' }, result: '返回值' })
    const ordinary = [restored]
    expect(executionWorkflowBlocks([], [], ordinary)).toEqual(ordinary)
    expect(executionTasks([], [], [{ content: '简单任务' }])).toEqual([{ content: '简单任务' }])
  })
})
