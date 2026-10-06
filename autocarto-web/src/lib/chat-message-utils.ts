import type { SessionMessageRecord, SessionRecord } from '@/lib/api'
import type { Message, TodoItem, WorkflowBlock, WorkflowToolCard } from '@/types/message'

export const allMessageTypes: Message['type'][] = [
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
]

export const knownMessageTypeSet = new Set<Message['type']>(allMessageTypes)

export function isKnownMessageType(value: unknown): value is Message['type'] {
  return typeof value === 'string' && knownMessageTypeSet.has(value as Message['type'])
}

export function roleToMessageType(role: string, msgType?: unknown): Message['type'] {
  if (isKnownMessageType(msgType)) {
    return msgType
  }

  const normalizedRole = String(role || '').toLowerCase()
  if (normalizedRole === 'user') {
    return 'user'
  }
  if (normalizedRole === 'assistant') {
    return 'assistant'
  }
  return 'status'
}

export function toTimestamp(raw: unknown) {
  if (typeof raw !== 'string') {
    return Date.now()
  }
  const value = new Date(raw).getTime()
  return Number.isNaN(value) ? Date.now() : value
}

export function toChatMessages(session: SessionRecord): Message[] {
  const input = Array.isArray(session.messages) ? session.messages : []
  return input.map((item, idx) => {
    const rawData = item.data
    const data = rawData && typeof rawData === 'object' && !Array.isArray(rawData)
      ? (rawData as Record<string, unknown>)
      : undefined

    const message: Message = {
      id: `session-${session.session_id}-${idx}-${typeof item.timestamp === 'string' ? item.timestamp : 'no-ts'}`,
      type: roleToMessageType(String(item.role || 'status'), item.msg_type),
      content: String(item.content || ''),
      timestamp: toTimestamp(item.timestamp),
    }

    if (typeof item.agent === 'string' && item.agent) {
      message.agent = item.agent
    }
    if (typeof item.agent_display_name === 'string' && item.agent_display_name) {
      message.agentDisplayName = item.agent_display_name
    }
    if (typeof item.phase === 'string' && item.phase) {
      message.phase = item.phase
    }
    if (typeof item.workflow_block_id === 'string' && item.workflow_block_id) {
      message.workflowBlockId = item.workflow_block_id
    }
    if (data) {
      message.data = data
    }

    return message
  })
}

export function toSessionMessage(msg: Message): SessionMessageRecord {
  const record: SessionMessageRecord = {
    role: msg.type === 'user' ? 'user' : msg.type === 'assistant' ? 'assistant' : 'system',
    content: msg.content,
    timestamp: new Date(msg.timestamp).toISOString(),
    msg_type: msg.type,
  }

  if (msg.agent) {
    record.agent = msg.agent
  }
  if (msg.agentDisplayName) {
    record.agent_display_name = msg.agentDisplayName
  }
  if (msg.phase) {
    record.phase = msg.phase
  }
  if (msg.workflowBlockId) {
    record.workflow_block_id = msg.workflowBlockId
  }
  if (msg.data && typeof msg.data === 'object' && !Array.isArray(msg.data)) {
    record.data = msg.data
  }

  return record
}

export function buildSessionMessageDedupKey(message: SessionMessageRecord) {
  const type = String(message.msg_type || message.role || 'status')
  const content = String(message.content || '')
  const timestamp = typeof message.timestamp === 'string' ? message.timestamp : ''
  return `${type}::${timestamp}::${content}`
}

export function mergeSessionMessages(baseMessages: Message[], cachedMessages: Message[]) {
  if (cachedMessages.length === 0) {
    return baseMessages
  }
  const dedup = new Set(baseMessages.map((item) => buildSessionMessageDedupKey(toSessionMessage(item))))
  const merged = [...baseMessages]
  for (const item of cachedMessages) {
    const key = buildSessionMessageDedupKey(toSessionMessage(item))
    if (dedup.has(key)) {
      continue
    }
    dedup.add(key)
    merged.push(item)
  }
  return merged.sort((a, b) => a.timestamp - b.timestamp)
}

export function toTodoItems(raw: unknown): TodoItem[] {
  if (!Array.isArray(raw)) {
    return []
  }

  return raw
    .filter((item) => item && typeof item === 'object' && !Array.isArray(item))
    .map((item) => {
      const payload = item as Record<string, unknown>
      return {
        content: typeof payload.content === 'string'
          ? payload.content
          : String(payload.content || payload.title || payload.name || ''),
        status: typeof payload.status === 'string' ? payload.status : String(payload.status || ''),
        priority: typeof payload.priority === 'string' ? payload.priority : undefined,
        owner: typeof payload.owner === 'string' ? payload.owner : undefined,
      } satisfies TodoItem
    })
}

export function toContextMessages(messages: Message[]) {
  return messages
    .filter((msg) => msg.type === 'user' || msg.type === 'assistant' || msg.type === 'status')
    .map((msg) => ({
      role: msg.type === 'user' ? 'user' : msg.type === 'assistant' ? 'assistant' : 'system',
      content: msg.content,
    }))
}

export function toStringList(raw: unknown): string[] {
  if (!Array.isArray(raw)) {
    return []
  }
  return raw
    .map((item) => String(item || '').trim())
    .filter((item) => Boolean(item))
}

function numericValue(raw: unknown): number | null {
  if (typeof raw === 'number' && Number.isFinite(raw)) {
    return raw
  }
  if (typeof raw === 'string' && raw.trim()) {
    const parsed = Number(raw)
    if (Number.isFinite(parsed)) {
      return parsed
    }
  }
  return null
}

export function estimateContextStats(messages: Message[], rawContextStats?: Record<string, unknown> | null) {
  const contextMessages = toContextMessages(messages)
  let totalChars = 0
  for (const item of contextMessages) {
    totalChars += String(item.content || '').length
  }

  const rawMessageCount = numericValue(rawContextStats?.message_count)
  const rawTotalChars = numericValue(rawContextStats?.total_chars)
  const rawEstimatedTokens = numericValue(rawContextStats?.estimated_tokens)
    ?? numericValue(rawContextStats?.total_tokens)

  if (
    rawEstimatedTokens !== null
    && rawMessageCount !== null
    && rawMessageCount === contextMessages.length
  ) {
    return {
      messageCount: contextMessages.length,
      totalChars: rawTotalChars !== null ? Math.max(0, Math.round(rawTotalChars)) : totalChars,
      estimatedTokens: Math.max(0, Math.round(rawEstimatedTokens)),
    }
  }

  return {
    messageCount: contextMessages.length,
    totalChars,
    estimatedTokens: Math.max(0, Math.round(totalChars / 3)),
  }
}

export function toMessagesFromCompressedContext(raw: unknown): Message[] {
  if (!Array.isArray(raw)) {
    return []
  }
  return raw
    .filter((item) => item && typeof item === 'object')
    .map((item, idx) => {
      const payload = item as Record<string, unknown>
      const role = String(payload.role || 'system')
      const content = String(payload.content || '')
      return {
        id: `compressed-${Date.now()}-${idx}`,
        type: roleToMessageType(role),
        content,
        timestamp: Date.now() + idx,
      } satisfies Message
    })
}

export function normalizeWorkflowBlockStatus(raw: unknown): WorkflowBlock['status'] {
  const value = String(raw || '').trim().toLowerCase()
  if (value === 'running' || value === 'active' || value === 'in_progress' || value === 'processing') {
    return 'running'
  }
  if (value === 'error' || value === 'failed' || value === 'blocked' || value === 'cancelled' || value === 'canceled' || value === 'aborted') {
    return 'error'
  }
  if (value === 'completed' || value === 'done' || value === 'success' || value === 'succeeded' || value === 'skipped') {
    return 'completed'
  }
  return 'completed'
}

export function normalizeWorkflowToolStatus(raw: unknown, success?: unknown): WorkflowToolCard['status'] {
  const value = String(raw || '').trim().toLowerCase()
  if (value === 'completed' || value === 'done' || value === 'success' || value === 'succeeded') {
    return 'completed'
  }
  if (value === 'failed' || value === 'error' || value === 'blocked' || value === 'cancelled' || value === 'canceled' || value === 'aborted') {
    return 'failed'
  }
  if (value === 'running' || value === 'active' || value === 'in_progress' || value === 'processing') {
    return 'running'
  }
  if (typeof success === 'boolean') {
    return success ? 'completed' : 'failed'
  }
  return 'running'
}

export function shouldPreserveTaskListUpdateCompletion(
  existing: WorkflowToolCard | undefined,
  incoming: WorkflowToolCard | undefined,
): boolean {
  if (!existing || !incoming) {
    return false
  }
  if (existing.tool !== 'write_todos' || incoming.tool !== 'write_todos') {
    return false
  }
  return normalizeWorkflowToolStatus(existing.status, existing.success) === 'completed'
    && existing.statusSource === 'task_list_update'
    && normalizeWorkflowToolStatus(incoming.status, incoming.success) === 'running'
}

export function reconcileWorkflowBlocksWithTaskListUpdate(blocks: WorkflowBlock[], timestamp: number): WorkflowBlock[] {
  if (!Array.isArray(blocks)) {
    return []
  }
  if (blocks.length === 0) {
    return blocks
  }

  for (let blockIndex = blocks.length - 1; blockIndex >= 0; blockIndex -= 1) {
    const block = blocks[blockIndex]
    const entryIndex = block.entries.findLastIndex((entry) => {
      if (!entry.toolCard) {
        return false
      }
      return entry.toolCard.tool === 'write_todos'
        && normalizeWorkflowToolStatus(entry.toolCard.status, entry.toolCard.success) === 'running'
    })

    if (entryIndex < 0) {
      continue
    }

    const targetEntry = block.entries[entryIndex]
    const nextEntries = [...block.entries]
    nextEntries[entryIndex] = {
      ...targetEntry,
      phase: 'tool_result',
      timestamp,
      toolCard: {
        ...targetEntry.toolCard!,
        status: 'completed',
        statusSource: 'task_list_update',
        success: true,
        finishedAt: timestamp,
      },
    }

    const nextBlocks = [...blocks]
    nextBlocks[blockIndex] = {
      ...block,
      updatedAt: timestamp,
      entries: nextEntries,
    }
    return nextBlocks
  }

  return blocks
}

export function toWorkflowBlocks(raw: unknown): WorkflowBlock[] {
  if (!Array.isArray(raw)) {
    return []
  }

  return raw
    .filter((item) => item && typeof item === 'object')
    .map((item, idx) => {
      const payload = item as Record<string, unknown>
      const rawEntries = Array.isArray(payload.entries) ? payload.entries : []
      return {
        id: String(payload.id || `wf-session-${idx}-${Date.now()}`),
        agent: String(payload.agent || 'orchestrator'),
        agentDisplayName: String(payload.agentDisplayName || payload.agent || '智能体'),
        summary: String(payload.summary || '处理中'),
        status: normalizeWorkflowBlockStatus(payload.status || 'completed'),
        startedAt: Math.max(0, Number(payload.startedAt || Date.now())),
        updatedAt: Math.max(0, Number(payload.updatedAt || payload.startedAt || Date.now())),
        finishedAt: typeof payload.finishedAt === 'number' ? payload.finishedAt : undefined,
        entries: rawEntries
          .filter((entry) => entry && typeof entry === 'object')
          .map((entry, entryIdx) => {
            const entryPayload = entry as Record<string, unknown>
            const rawToolCard = entryPayload.toolCard
            const toolCard = rawToolCard && typeof rawToolCard === 'object'
              ? (() => {
                  const card = rawToolCard as Record<string, unknown>
                  const argsRaw = card.args
                  const args = argsRaw && typeof argsRaw === 'object' && !Array.isArray(argsRaw)
                    ? (argsRaw as Record<string, unknown>)
                    : undefined
                  return {
                    id: String(card.id || `tool-${idx}-${entryIdx}-${Date.now()}`),
                    tool: String(card.tool || '未知工具'),
                    status: normalizeWorkflowToolStatus(card.status, card.success),
                    args,
                    result: typeof card.result === 'string' ? card.result : undefined,
                    statusSource: typeof card.statusSource === 'string' ? card.statusSource : undefined,
                    success: typeof card.success === 'boolean' ? card.success : undefined,
                    startedAt: Math.max(0, Number(card.startedAt || Date.now())),
                    finishedAt: typeof card.finishedAt === 'number' ? card.finishedAt : undefined,
                  }
                })()
              : undefined

            const rawData = entryPayload.data
            const data = rawData && typeof rawData === 'object' && !Array.isArray(rawData)
              ? (rawData as Record<string, unknown>)
              : undefined

            return {
              id: String(entryPayload.id || `entry-${idx}-${entryIdx}-${Date.now()}`),
              type: (['thinking', 'tool', 'status', 'decision', 'note'].includes(String(entryPayload.type || 'status'))
                ? String(entryPayload.type)
                : 'status') as 'thinking' | 'tool' | 'status' | 'decision' | 'note',
              content: String(entryPayload.content || ''),
              phase: typeof entryPayload.phase === 'string' ? entryPayload.phase : undefined,
              timestamp: Math.max(0, Number(entryPayload.timestamp || Date.now())),
              data,
              toolCard,
            }
          }),
      } satisfies WorkflowBlock
    })
}
