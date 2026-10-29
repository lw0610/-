import type { Message, TodoItem, WorkflowBlock, WorkflowEntry } from '@/types/message'
import { normalizeWorkflowToolStatus } from '@/lib/chat-message-utils'
import type { FlowRunState, FlowStepState, PlanSnapshot } from './types'

const roleNames: Record<string, string> = {
  orchestrator: '主智能体', data_engineer: '数据工程智能体', cartographer: '制图智能体',
  quality_inspector: '质量审查智能体', system_data_acquisition: '数据获取',
}
const time = (value?: string) => Date.parse(value || '') || 0

export function workspaceRuns(runs: FlowRunState[], plans: PlanSnapshot[]) {
  const byId = new Map(plans.flatMap(plan => plan.run ? [[plan.run.flow_run_id, plan.run] as const] : []))
  for (const run of runs) byId.set(run.flow_run_id, run)
  return [...byId.values()].sort((a, b) => time(a.started_at) - time(b.started_at))
}

export function executionBlockId(run: FlowRunState, step: FlowStepState, attemptIndex = 0) {
  return `execution-${run.flow_run_id}-${step.id}-${step.attempts[attemptIndex]?.run_id || attemptIndex}`
}

export function activeExecutionBlockId(run: FlowRunState) {
  const step = run.steps.find(item => ['running', 'waiting_user', 'failed', 'interrupted'].includes(item.status))
    || run.steps.slice().reverse().find(item => item.attempts.length) || run.steps[0]
  return step ? executionBlockId(run, step, Math.max(0, step.attempts.length - 1)) : `flow-run-${run.flow_run_id}`
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
    owner: roleNames[step.role] || step.role,
  })) : fallback
}

function toEntry(message: Message): WorkflowEntry | null {
  if (message.workflowBlockId || message.type === 'user' || message.type === 'interrupt') return null
  const data = message.data || {}, phase = message.phase
  const tool = String(data.tool || '')
  const isTool = phase === 'tool_call' || phase === 'tool_result' || message.type === 'tool'
  if (!message.content && !isTool) return null
  const entry: WorkflowEntry = {
    id: message.id, type: isTool ? 'tool' : message.type === 'thinking' ? 'thinking' : message.type === 'decision' ? 'decision' : 'status',
    content: message.content, phase, timestamp: message.timestamp, data,
  }
  if (isTool) entry.toolCard = {
    id: String(data.call_id || message.id), callId: String(data.call_id || '') || undefined, tool,
    status: phase === 'tool_call' ? 'running' : normalizeWorkflowToolStatus(data.status, data.success),
    args: data.tool_args && typeof data.tool_args === 'object' ? data.tool_args as Record<string, unknown> : undefined,
    result: phase === 'tool_result' ? String(data.result ?? message.content) : undefined,
    statusSource: typeof data.status_source === 'string' ? data.status_source : undefined,
    success: typeof data.success === 'boolean' ? data.success : undefined,
    startedAt: message.timestamp, finishedAt: phase === 'tool_result' ? message.timestamp : undefined,
  }
  return entry
}

function attemptIndex(step: FlowStepState, entry: WorkflowEntry) {
  const explicit = step.attempts.findIndex(attempt => attempt.run_id === entry.data?.run_id)
  if (explicit >= 0) return explicit
  const timestamp = entry.toolCard?.startedAt || entry.timestamp
  const index = step.attempts.findLastIndex(attempt => time(attempt.started_at) <= timestamp)
  return Math.max(0, index)
}

function mergeEntry(previous: WorkflowEntry, incoming: WorkflowEntry): WorkflowEntry {
  const oldTool = previous.toolCard, newTool = incoming.toolCard
  if (!oldTool || !newTool) return incoming.timestamp >= previous.timestamp ? { ...incoming, id: previous.id } : previous
  // A late/replayed call must not undo its result; call IDs, never tool names,
  // identify one invocation. Distinct retries remain separate records.
  const terminal = previous.phase === 'tool_result' && incoming.phase !== 'tool_result' ? previous : incoming
  return {
    ...terminal, id: previous.id, timestamp: Math.max(previous.timestamp, incoming.timestamp),
    data: { ...previous.data, ...incoming.data, ...terminal.data },
    toolCard: { ...oldTool, ...newTool, ...terminal.toolCard,
      id: oldTool.id, args: oldTool.args || newTool.args,
      startedAt: Math.min(oldTool.startedAt, newTool.startedAt),
    },
  }
}

/** One read model for live events and restored history; neither source is rewritten. */
export function executionWorkflowBlocks(runs: FlowRunState[], messages: Message[], persisted: WorkflowBlock[]): WorkflowBlock[] {
  const runById = new Map(runs.map(run => [run.flow_run_id, run]))
  const groups = new Map<string, { block: WorkflowBlock; entries: Map<string, WorkflowEntry> }>()
  const ordinary: WorkflowBlock[] = []
  const occurrences = new Map<string, number>()
  const callGroups = new Map<string, string>()
  const historicalCalls = new Set<string>()

  for (const run of runs) for (const step of run.steps) {
    const attempts = step.attempts.length ? step.attempts : [undefined]
    attempts.forEach((attempt, index) => {
      if (!attempt && ['pending', 'todo'].includes(step.status)) return
      const latest = index === attempts.length - 1
      const status = latest ? step.status : attempt?.verification?.ok === false ? 'failed' : attempt?.receipt?.status || 'completed'
      const startedAt = time(attempt?.started_at) || time(run.started_at)
      const id = executionBlockId(run, step, index)
      const entries = new Map<string, WorkflowEntry>()
      const summary = `${step.index}. ${step.title}${attempts.length > 1 ? ` · 第 ${index + 1} 次尝试` : ''}${status === 'waiting_user' ? ' · 等待确认' : ''}`
      groups.set(id, { block: { id, agent: step.role, agentDisplayName: roleNames[step.role] || step.role || '智能体', summary,
        status: ['failed', 'cancelled', 'interrupted'].includes(status) ? 'error' : ['running', 'waiting_user'].includes(status) ? 'running' : 'completed',
        startedAt, updatedAt: time(attempt?.finished_at) || startedAt, finishedAt: time(attempt?.finished_at) || undefined, entries: [],
      }, entries })
      const note = (key: string, content: string) => { if (content) entries.set(key, { id: `${id}-${key}`, type: 'status', content, timestamp: time(attempt?.finished_at) || startedAt }) }
      note('receipt', attempt?.receipt?.summary || '')
      const outputs = attempt?.receipt?.outputs || (latest ? step.outputs : {})
      if (Object.keys(outputs).length) note('outputs', `步骤产出\n${JSON.stringify(outputs, null, 2)}`)
      if (latest) note('failure', step.last_failure || '')
      note('verification', attempt?.verification?.errors?.join('\n') || '')
      if (step.status === 'skipped') note('condition', step.condition?.reason || '此步骤已跳过')
      if (latest && step === (run.steps.find(item => ['running', 'waiting_user', 'failed', 'interrupted'].includes(item.status)) || run.steps.at(-1))) {
        if (run.source_notes?.length) note('sources', `数据来源说明\n${[...new Set(run.source_notes)].join('\n')}`)
        if (run.result.issues.length) note('issues', `待处理事项\n${[...new Set(run.result.issues)].join('\n')}`)
        const verification = run.final_verification as { ok?: boolean; errors?: string[] } | undefined
        if (verification) note('final-verification', verification.ok ? '完成检查已通过' : `完成检查发现问题\n${verification.errors?.join('\n') || ''}`)
        if (run.run_json_path) note('run-record', `运行记录\n${run.run_json_path}`)
      }
    })
  }

  const add = (entry: WorkflowEntry, source: 'persisted' | 'live', agent?: string, displayName?: string, sourceBlock?: WorkflowBlock) => {
    const run = runById.get(String(entry.data?.flow_run_id || ''))
    const step = run?.steps.find(step => step.id === entry.data?.step_id)
    if (!run || !step) return false
    const callId = entry.toolCard?.callId || String(entry.data?.call_id || '')
    const callKey = callId ? `${run.flow_run_id}:${step.id}:${callId}` : ''
    const attemptId = String(entry.data?.run_id || '')
    if (source === 'live' && callKey && historicalCalls.has(callKey)) return true
    // Old sessions did not retain run_id on process entries, and replanning
    // sometimes removed old attempts. Keep those original blocks intact;
    // assigning a pre-snapshot event to attempt 0 would invent a false history.
    const firstStart = time(step.attempts[0]?.started_at)
    if (source === 'persisted' && !attemptId && firstStart && (entry.toolCard?.startedAt || entry.timestamp) < firstStart) {
      if (callKey) historicalCalls.add(callKey)
      return false
    }
    const missingAttempt = attemptId && !step.attempts.some(attempt => attempt.run_id === attemptId)
    const id = (callKey && callGroups.get(callKey)) || (missingAttempt
      ? `execution-${run.flow_run_id}-${step.id}-${attemptId}`
      : executionBlockId(run, step, attemptIndex(step, entry)))
    let group = groups.get(id)
    if (!group) {
      const block: WorkflowBlock = { id, agent: agent || step.role, agentDisplayName: displayName || roleNames[step.role] || step.role || '智能体',
        summary: `${step.index}. ${step.title}${missingAttempt ? ' · 历史尝试' : ''}`, status: sourceBlock?.status || (step.status === 'running' ? 'running' : step.status === 'failed' ? 'error' : 'completed'),
        startedAt: entry.timestamp, updatedAt: entry.timestamp, entries: [] }
      group = { block, entries: new Map() }; groups.set(id, group)
    }
    if (callKey) callGroups.set(callKey, id)
    if (displayName) group.block.agentDisplayName = roleNames[displayName] || displayName
    if (agent) group.block.agent = agent
    let key = callId ? `call:${callId}` : entry.phase === 'progress' && entry.data?.request_key ? `progress:${entry.data.request_key}` : ''
    if (!key) {
      const semantic = `${entry.type}:${entry.phase || ''}:${entry.content}`
      const counterKey = `${source}:${id}:${semantic}`
      const count = occurrences.get(counterKey) || 0
      occurrences.set(counterKey, count + 1)
      key = `${semantic}:${count}`
    }
    const previous = group.entries.get(key)
    group.entries.set(key, previous ? mergeEntry(previous, entry) : entry)
    group.block.updatedAt = Math.max(group.block.updatedAt, entry.timestamp)
    return true
  }

  // Restore entries first: their original timestamps and call IDs anchor live
  // replays to the same attempt, even if the client received them much later.
  for (const block of persisted) {
    const remaining = block.entries.filter(entry => !add(entry, 'persisted', block.agent, block.agentDisplayName, block))
    if (remaining.length === block.entries.length) ordinary.push(block)
    else if (remaining.length) ordinary.push({ ...block, entries: remaining })
  }
  for (const message of messages) {
    const entry = toEntry(message)
    if (entry) add(entry, 'live', message.agent, message.agentDisplayName)
  }
  return [...ordinary, ...[...groups.values()].map(({ block, entries }) => ({ ...block,
    entries: [...entries.values()].sort((a, b) => (a.toolCard?.startedAt || a.timestamp) - (b.toolCard?.startedAt || b.timestamp)),
  }))].sort((a, b) => a.startedAt - b.startedAt)
}
