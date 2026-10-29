import type { DataCandidate, DataFindResult, DataFindRow, PlanData } from './types'

export interface PlanSource {
  candidate: DataCandidate
  route: 'direct' | 'explore'
}

// Keep selection semantics aligned with dataservice.ValidateChoice: exploration
// is eligible only when the query has no available deterministic candidate.
export function planSources(result: DataFindResult, row: DataFindRow): PlanSource[] {
  const direct = (row.candidates || []).map(candidate => ({ candidate, route: 'direct' as const }))
  if (direct.some(source => source.candidate.available)) return direct
  return [...direct, ...(result.fallback || []).map(candidate => ({ candidate, route: 'explore' as const }))]
}

export function selectedPlanSource(result: DataFindResult, row: DataFindRow, choice?: PlanData['choice']): PlanSource | undefined {
  const id = typeof choice === 'object' && choice ? choice[row.row] || choice['*'] : choice
  const automatic = !id || id === 'auto' || id === 'recommended'
  const options = planSources(result, row)
  if (!automatic) return options.find(source => source.candidate.available && source.candidate.id === id)
  return options.find(source => source.route === 'direct' && source.candidate.available && source.candidate.id === row.recommended)
    || options.find(source => source.route === 'explore' && source.candidate.available)
}

export function planSourceName(source: PlanSource): string {
  return source.route === 'explore' ? `${source.candidate.name} · 探索获取` : source.candidate.name
}
