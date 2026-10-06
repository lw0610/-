export function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object') {
    return {}
  }
  return value as Record<string, unknown>
}

export function asBool(value: unknown, fallback: boolean): boolean {
  return typeof value === 'boolean' ? value : fallback
}

export function asNumber(value: unknown, fallback: number): number {
  if (typeof value === 'number' && Number.isFinite(value)) {
    return value
  }
  return fallback
}

export function asString(value: unknown, fallback: string): string {
  return typeof value === 'string' ? value : fallback
}

export function clampInt(value: number, min = 1, max = Number.MAX_SAFE_INTEGER): number {
  const rounded = Math.round(value)
  return Math.min(max, Math.max(min, rounded))
}

export function normalizePathInput(value: string): string {
  return value.trim()
}
