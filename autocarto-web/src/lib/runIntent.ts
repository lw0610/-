import type { ExecutionMode, RunIntent } from '@/types/message'

function normalizeValue(value: unknown) {
  return String(value || '').trim().toLowerCase()
}

export function isExecutionMode(value: unknown): value is ExecutionMode {
  const mode = normalizeValue(value)
  return mode === 'plan' || mode === 'build'
}

export function parseExecutionMode(value: unknown): ExecutionMode | null {
  const mode = normalizeValue(value)
  if (mode === 'plan' || mode === 'build') {
    return mode
  }
  return null
}

export function isRunIntent(value: unknown): value is RunIntent {
  const intent = normalizeValue(value)
  return intent === 'plan_only' || intent === 'execute'
}

export function parseRunIntent(value: unknown): RunIntent | null {
  const intent = normalizeValue(value)
  if (intent === 'plan_only' || intent === 'execute') {
    return intent
  }
  return null
}

export function executionModeToRunIntent(mode: ExecutionMode): RunIntent {
  return mode === 'plan' ? 'plan_only' : 'execute'
}

export function runIntentToExecutionMode(intent: unknown): ExecutionMode | null {
  const normalized = normalizeValue(intent)
  if (normalized === 'plan_only' || normalized === 'plan') {
    return 'plan'
  }
  if (normalized === 'execute' || normalized === 'build') {
    return 'build'
  }
  return null
}

export function resolveExecutionMode(mode: unknown, intent: unknown): ExecutionMode | null {
  const normalizedMode = normalizeValue(mode)
  if (normalizedMode) {
    return parseExecutionMode(normalizedMode)
  }
  return runIntentToExecutionMode(intent)
}

export function getExecutionModeValidationError(mode: unknown, intent: unknown): string | null {
  const normalizedMode = normalizeValue(mode)
  if (normalizedMode && !parseExecutionMode(normalizedMode)) {
    return `execution_mode=${normalizedMode}`
  }

  const normalizedIntent = normalizeValue(intent)
  if (!normalizedMode && normalizedIntent && !parseRunIntent(normalizedIntent)) {
    return `run_intent=${normalizedIntent}`
  }

  return null
}
