import { describe, expect, it } from 'vitest'
import type { WorkflowBlock } from '@/types/message'
import type { FlowRunState } from './types'
import { executionTasks, activeExecutionBlockId } from './workspace'

const run: FlowRunState = {
  api_version: 'flow-run/v1', flow_run_id: 'flow', session_id: 'session', flow_id: '', status: 'running', started_at: '2026-10-28T00:00:00Z',
  steps: [{id: 'prepare', index: 1, title: '整理数据', role: 'data_engineer', status: 'running', outputs: {}, attempts: []}],
  result: {summary: '', deliverables: {}, issues: []},
}
const original: WorkflowBlock = {
  id: 'original-agent-block', agent: 'system_data_acquisition', agentDisplayName: '数据获取智能体', summary: '获取数据', status: 'running', startedAt: 1, updatedAt: 2,
  entries: [{id: 'thought', type: 'thinking', content: '准备数据', timestamp: 1, data: {flow_run_id: 'flow'}}],
}

describe('workspace task and original process links', () => {
  it('shows task titles/status without adding responsible agents', () => {
    expect(executionTasks([run], [], [])).toEqual([{content: '整理数据', status: 'running'}])
    expect(executionTasks([], [], [{content: '简单任务'}])).toEqual([{content: '简单任务'}])
  })
  it('opens the existing agent process even before a data preparation step exists', () => {
    expect(activeExecutionBlockId(run, [original])).toBe('original-agent-block')
    const later = {...original, id: 'later-cartographer', agent: 'cartographer'}
    const unrelated = {...original, id: 'other-flow', entries: [{...original.entries[0], data: {flow_run_id: 'another-flow'}}]}
    expect(activeExecutionBlockId(run, [original, later, unrelated])).toBe(later.id)
  })
})
