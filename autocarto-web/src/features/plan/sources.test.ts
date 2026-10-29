import { expect, it } from 'vitest'
import { planSources, selectedPlanSource } from './sources'
import type { DataFindResult, PlanData } from './types'

const result = (): DataFindResult => ({ rows: [{ row: 'population', category: 'population', candidates: null }], fallback: [
  { id: 'disabled', kind: 'web_search', name: '不可用搜索', available: false, reasons: [] },
  { id: 'public-search', kind: 'web_search', name: '公开来源搜索', available: true, reasons: ['执行时搜索并核验'] },
] })

it('resolves default, recommended, row and wildcard choices to genuine exploration candidates', () => {
  const query = result()
  const choices: PlanData['choice'][] = [undefined, 'auto', 'recommended', 'public-search', { population: 'public-search' }, { '*': 'recommended' }]
  for (const choice of choices) {
    expect(selectedPlanSource(query, query.rows[0], choice)).toMatchObject({ route: 'explore', candidate: { id: 'public-search' } })
  }
  expect(selectedPlanSource(query, query.rows[0], 'invented-id')).toBeUndefined()
  expect(selectedPlanSource(query, query.rows[0], 'disabled')).toBeUndefined()
})

it('keeps available deterministic candidates ahead of and separate from exploration', () => {
  const query = result()
  query.rows[0].candidates = [{ id: 'file', kind: 'user', name: '已提供人口表', path: '/data/population.csv', available: true, reasons: [] }]
  query.rows[0].recommended = 'file'
  expect(selectedPlanSource(query, query.rows[0], 'auto')).toMatchObject({ route: 'direct', candidate: { id: 'file' } })
  expect(planSources(query, query.rows[0])).toHaveLength(1)
  expect(selectedPlanSource(query, query.rows[0], 'public-search')).toBeUndefined()
})
