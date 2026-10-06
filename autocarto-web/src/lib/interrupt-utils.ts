import { toStringList } from '@/lib/chat-message-utils'
import { asRecord } from '@/lib/session-utils'

export function sanitizeInterruptPayload(payload?: unknown) {
  const customValue = '__custom_input__'
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) {
    return payload
  }

  const record = payload as Record<string, unknown>
  const nextPayload: Record<string, unknown> = {
    ...record,
  }

  if (typeof record.option === 'string' && record.option.trim() === customValue) {
    delete nextPayload.option
  }

  if (Array.isArray(record.options)) {
    const options = toStringList(record.options).filter((value) => value !== customValue)
    if (options.length > 0) {
      nextPayload.options = options
    } else {
      delete nextPayload.options
    }
  }

  const decisionMap = asRecord(record.decisions)
  const decisionEntries = Object.entries(decisionMap)
  if (decisionEntries.length > 0) {
    const sanitizedDecisions: Record<string, unknown> = {}
    decisionEntries.forEach(([decisionId, raw]) => {
      const decisionRecord = asRecord(raw)
      const nextDecision: Record<string, unknown> = {
        ...decisionRecord,
      }
      const selected = toStringList(decisionRecord.selected).filter((value) => value !== customValue)
      if (selected.length > 0) {
        nextDecision.selected = selected
      } else {
        delete nextDecision.selected
      }
      sanitizedDecisions[decisionId] = nextDecision
    })
    nextPayload.decisions = sanitizedDecisions
  }

  return nextPayload
}

export function formatInterruptDecisionMessage(checkpointId: string, payload?: unknown) {
  const header = `决策结果（${checkpointId || 'pending checkpoint'}）`
  const customValue = '__custom_input__'
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) {
    return `${header}\n- 已确认并继续执行。`
  }

  const record = payload as Record<string, unknown>
  const lines: string[] = [header]

  const decisionMap = asRecord(record.decisions)
  const decisionEntries = Object.entries(decisionMap)
  if (decisionEntries.length > 0) {
    decisionEntries.forEach(([id, raw], idx) => {
      const item = asRecord(raw)
      const rawSelected = toStringList(item.selected)
      const hasCustomSelected = rawSelected.includes(customValue)
      const selected = rawSelected.filter((value) => value !== customValue)
      const text = String(item.text || '').trim()
      const note = String(item.note || '').trim()
      const confirmed = typeof item.confirmed === 'boolean' ? item.confirmed : null
      const parts: string[] = []

      if (selected.length > 0) {
        parts.push(`选择: ${selected.join('、')}`)
      }
      if (confirmed !== null) {
        parts.push(`确认: ${confirmed ? '是' : '否'}`)
      }
      if (text) {
        parts.push(hasCustomSelected ? `自定义: ${text}` : `输入: ${text}`)
      }
      if (note) {
        parts.push(`补充: ${note}`)
      }

      const detail = parts.length > 0 ? parts.join('；') : '已确认'
      lines.push(`- Q${idx + 1}（${id}）: ${detail}`)
    })
  } else {
    const option = String(record.option || '').trim()
    const options = toStringList(record.options)
    const input = String(record.input || '').trim()
    const text = String(record.text || '').trim()
    const confirmed = typeof record.confirmed === 'boolean' ? record.confirmed : null

    if (option) {
      lines.push(`- 选择: ${option}`)
    }
    if (options.length > 0) {
      lines.push(`- 选择: ${options.join('、')}`)
    }
    if (input) {
      lines.push(`- 输入: ${input}`)
    }
    if (text) {
      lines.push(`- 补充: ${text}`)
    }
    if (confirmed !== null) {
      lines.push(`- 确认: ${confirmed ? '是' : '否'}`)
    }
  }

  return lines.length > 1 ? lines.join('\n') : `${header}\n- 已确认并继续执行。`
}
