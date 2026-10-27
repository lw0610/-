import type {
  AcquisitionChoice,
  AcquisitionItem,
  FlowInput,
} from '@/features/flows/types'
export type SourceMode = 'user' | 'auto' | 'fill' | 'none'
export function acquisitionChoice(value: unknown): AcquisitionChoice {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? (value as AcquisitionChoice)
    : {}
}
export function sourceMode(value: unknown): SourceMode {
  const choice = acquisitionChoice(value)
  return choice.$fill
    ? 'fill'
    : choice.$acquire
      ? 'auto'
      : value === null
        ? 'none'
        : 'user'
}
export function sourcePath(value: unknown) {
  return typeof value === 'string' ? value : acquisitionChoice(value).path || ''
}
export function changeSourceMode(value: unknown, mode: SourceMode): unknown {
  const previous = acquisitionChoice(value)
  if (mode === 'none') return null
  if (mode === 'user') return sourcePath(value)
  const choices = {
    ...(previous.sources ? { sources: previous.sources } : {}),
    ...(previous.files ? { files: previous.files } : {}),
    ...(previous.source ? { source: previous.source } : {}),
  }
  return mode === 'auto'
    ? { $acquire: true, ...choices }
    : { $fill: true, path: sourcePath(value), ...choices }
}
export function updateSourcePath(value: unknown, path: string) {
  return sourceMode(value) === 'fill'
    ? { ...acquisitionChoice(value), path }
    : path
}
export function changeItemChoice(
  value: unknown,
  item: AcquisitionItem,
  kind: 'source' | 'file',
  selection: string,
): AcquisitionChoice | string {
  // A single-file input uses its ordinary path mode when the user supplies it.
  // Per-entry files belong only to a folder/list acquisition choice.
  if (kind === 'file' && item.input_type !== 'folder' && !item.resource) return selection
  const next = { ...acquisitionChoice(value) }
  if (!next.$acquire && !next.$fill) next.$acquire = true
  if (kind === 'file') {
    const files = { ...next.files }
    if (selection) files[item.entry || item.key] = selection
    else delete files[item.entry || item.key]
    next.files = files
  } else if (item.input_type === 'folder' || item.resource) {
    const files = { ...next.files }
    delete files[item.entry || item.key]
    next.files = files
    const sources = { ...next.sources }
    if (selection) sources[item.entry || item.key] = selection
    else delete sources[item.entry || item.key]
    next.sources = sources
  } else {
    const files = { ...next.files }
    delete files[item.entry || item.key]
    next.files = files
    if (selection) next.source = selection
    else delete next.source
  }
  return next
}
export function canAcquire(input: FlowInput, policy?: string) {
  return (
    input.acquirable === true &&
    policy !== 'deny' &&
    Boolean(input.acquire_available)
  )
}
export function acquisitionDependencies(
  items: AcquisitionItem[],
  values: Record<string, unknown>,
) {
  const labels = new Set<string>()
  for (const item of items) {
    if (!['auto', 'fill'].includes(sourceMode(values[item.input]))) continue
    const text = [
      item.extent_expression,
      item.scale_expression,
      ...Object.values(item.params || {}),
    ].join(' ')
    for (const match of text.matchAll(/「([^」]+)」/g)) labels.add(match[1])
  }
  return labels
}
