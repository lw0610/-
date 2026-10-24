import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'
import { StepPreview } from './PreviewPanels'
import type { FlowPlan } from './types'

it('shows saved steps directly without compilation progress or failure controls', () => {
  const plan: FlowPlan = {
    api_version: 'flow-plan/v1', flow_id: 'demo', name: '示例', scheme: 'default',
    inputs: [], resources: [], plan_status: 'enriching',
    enrichment: { done: 0, total: 2, failed: [{ step_id: 'two', title: '导出成果', reason: 'invalid_json' }] },
    steps: [
      { id: 'one', index: 1, title: '读取数据', role_label: '数据角色', kind: 'agent', instructions: '读取用户选择的数据。', enrichment: 'pending', on_failure: 'ask' },
      { id: 'two', index: 2, title: '导出成果', role_label: '制图角色', kind: 'agent', instructions: '导出为 PDF。', enrichment: 'failed', condition: { text: '用户确认后' }, expected_outputs: [{ label: '地图', type: 'file', required: true, format: 'pdf' }], on_failure: 'stop' },
    ],
  }
  render(<StepPreview plan={plan} capabilities={[]} />)
  expect(screen.getByText('1. 读取数据')).toBeInTheDocument()
  expect(screen.getByText('2. 导出成果')).toBeInTheDocument()
  expect(screen.getByText('条件：用户确认后')).toBeInTheDocument()
  expect(screen.getByText('产出：地图 · file / pdf · 必需')).toBeInTheDocument()
  expect(screen.getByText('失败时：停止流程')).toBeInTheDocument()
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
  expect(screen.queryByText(/未完善|完善中|模型返回格式|重新编译/)).not.toBeInTheDocument()
})
