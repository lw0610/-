import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'
import PlanStatusBadge from './PlanStatusBadge'
import type { FlowPlan } from './types'

const plan: FlowPlan = {
  api_version: 'flow-plan/v1', flow_id: 'demo', name: '示例', scheme: 'default',
  inputs: [], resources: [], steps: [], plan_status: 'partial',
  enrichment: { done: 1, total: 1, failed: [{ step_id: 'one', title: '导出成果', reason: 'invalid_json' }] },
}

it('does not present historical model failures or a compile action as plan status', () => {
  render(<PlanStatusBadge plan={plan} />)
  expect(screen.getByText('已解析')).toBeInTheDocument()
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
  expect(screen.queryByText(/未完善|模型返回格式|重新编译/)).not.toBeInTheDocument()
})

it('reports actual flow errors from the saved detail', () => {
  const view = render(<PlanStatusBadge plan={plan} diagnostics={[{ level: 'error', code: 'ROLE_MISSING', message: '角色不存在' }]} />)
  expect(screen.getByText('需要修复')).toBeInTheDocument()
  view.rerender(<PlanStatusBadge plan={plan} diagnostics={[{ level: 'warning', code: 'RESOURCE_WARNING', message: '资源警告' }]} />)
  expect(screen.getByText('已解析')).toBeInTheDocument()
})
