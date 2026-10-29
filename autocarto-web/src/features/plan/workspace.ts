import type { TodoItem, WorkflowBlock } from '@/types/message'
import type { FlowRunState, PlanSnapshot } from './types'

const time = (value?: string) => Date.parse(value || '') || 0

export function workspaceRuns(runs: FlowRunState[], plans: PlanSnapshot[]) {
  const byId = new Map(plans.flatMap(plan => plan.run ? [[plan.run.flow_run_id, plan.run] as const] : []))
  for (const run of runs) byId.set(run.flow_run_id, run)
  return [...byId.values()].sort((a, b) => time(a.started_at) - time(b.started_at))
}

export function activeExecutionBlockId(run: FlowRunState, blocks: WorkflowBlock[]) {
  return blocks.findLast(block => block.entries.some(entry => entry.data?.flow_run_id === run.flow_run_id))?.id || `flow-run-${run.flow_run_id}`
}

export function executionTasks(runs: FlowRunState[], plans: PlanSnapshot[], fallback: TodoItem[]): TodoItem[] {
  const latestPlan = plans.slice().sort((a, b) => time(a.created_at || a.updated_at) - time(b.created_at || b.updated_at)).at(-1)
  const latestRun = runs.at(-1)
  if (latestPlan && (!latestRun || !latestPlan.run && time(latestPlan.created_at || latestPlan.updated_at) > time(latestRun.started_at))) {
    return latestPlan.plan.steps.map(step => ({ content: step.title, status: 'todo' }))
  }
  return latestRun?.steps.length ? latestRun.steps.map(step => ({
    content: step.title,
    status: step.status,
  })) : fallback
}
