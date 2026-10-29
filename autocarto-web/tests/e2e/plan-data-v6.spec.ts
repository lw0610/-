import { expect, test, type Page } from '@playwright/test'
import { mockDesktopRuntime } from './helpers/desktopRuntime'

type Harness = Window & { __v6sent: Record<string, unknown>[]; __v6socket: { onmessage?: (event: { data: string }) => void } }
const emit = (page: Page, message: Record<string, unknown>) => page.evaluate(message => (window as unknown as Harness).__v6socket.onmessage?.({ data: JSON.stringify(message) }), message)

// Offline browser contract playback. Real local matching, plan validation and
// ADK model/tool boundaries are covered by the Go L30 tests; this does not claim
// a real model, network acquisition, QGIS execution or native file access.
test('v6 one message shows each data origin and style then becomes a completed result', async ({ page }) => {
  await mockDesktopRuntime(page, { port: 50132 })
  await page.addInitScript(() => {
    const runtime = window as unknown as Harness
    runtime.__v6sent = []
    class Socket {
      static OPEN = 1; static CONNECTING = 0; readyState = 1
      onopen: ((event: Event) => void) | null = null
      onmessage?: (event: { data: string }) => void
      constructor() { runtime.__v6socket = this; setTimeout(() => this.onopen?.(new Event('open')), 0) }
      send(raw: string) { runtime.__v6sent.push(JSON.parse(raw)) }
      close() { this.readyState = 3 }
      addEventListener() {} removeEventListener() {}
    }
    Object.defineProperty(window, 'WebSocket', { configurable: true, value: Socket })
  })
  const plan = {
    api_version: 'carto-plan/v1', plan_id: 'cp-v6-sources', version: 1, goal: '湖南河流与铁路专题图', facts: [{ name: '范围', value: '湖南省', from: '用户消息' }], assumptions: [],
    data: [
      { id: 'river', name: '河流', layer_name: '河流线', from: 'user', path: '/tmp/v6-fixture/河流.geojson', geometry: 'line', style: '河流线' },
      { id: 'boundary', name: '省域', layer_name: '湖南省域', from: 'project', layer: 'project-boundary-id', geometry: 'polygon' },
      { id: 'water', name: '河流面', layer_name: '河流面', from: 'user', path: '/tmp/v6-fixture/湖泊水库.gpkg|layername=water', geometry: 'polygon', find_id: 'df-water', choice: 'c-water' },
      { id: 'rail', name: '铁路', layer_name: '铁路线', from: 'acquire', geometry: 'line', find_id: 'df-rail', choice: 'c-osm' },
      { id: 'route', name: '路线数据集', from: 'step', step: 'extract' },
    ],
    steps: [
      { id: 'extract', title: '从史料抽取路线', capabilities: ['路线知识抽取'], instructions: '读取用户本次史料', outputs: [{ label: '路线数据集', type: 'dataset' }] },
      { id: 'fetch', title: '只获取缺少的铁路', kind: 'acquire', data: ['rail'], outputs: [] },
      { id: 'map', title: '制图并导出', capabilities: ['制图与导出'], instructions: '使用原文件和工程图层，铁路使用真实获取结果', uses: ['river', 'boundary', 'water', 'rail', 'route'], outputs: [{ label: '图件', type: 'file', format: 'pdf' }] },
    ],
    deliverables: [{ label: '图件', type: 'file', format: 'pdf' }], checks: ['图件存在'],
  }
  const find = (row: string, id: string, candidate: Record<string, unknown>) => ({ find_id: id, rows: [{ row, recommended: candidate.id, candidates: [candidate], excluded: [] }] })
  const snapshot: Record<string, unknown> = {
    session_id: 'v6-plan-session', plan_id: plan.plan_id, plan_version: 1, plan, status: 'awaiting_confirmation', needs_confirmation: true, execution_mode: 'build',
    confirmation_reasons: ['河流面：几何类型匹配，但名称关联较弱，请确认用途'],
    find_results: {
      'df-water': find('河流面', 'df-water', { id: 'c-water', name: '湖泊水库', kind: 'user', available: true, confidence: 'confirm', requires_confirmation: true, reasons: ['用户文件夹中的文件'], match_reason: '几何类型匹配，但名称关联较弱，请确认用途' }),
      'df-rail': find('铁路', 'df-rail', { id: 'c-osm', name: 'OpenStreetMap 在线查询', kind: 'online_query', available: true, reasons: ['覆盖所需范围', '适合所选比例尺'], network: true }),
    },
  }
  const session = { session_id: 'v6-plan-session', name: 'V6 来源验收', project_path: '/tmp/v6-fixture/project.qgz', execution_mode: 'build', created_at: '2026-10-28T00:00:00Z', updated_at: '2026-10-28T00:00:00Z', messages: [] }
  const instance = { id: 'v6-instance', pid: 1, project_path: session.project_path, status: 'running', qgis_version: '3.40', services: {}, last_heartbeat: '2026-10-28T00:00:00Z' }
  const runtime = { services: { agent: { status: 'running' } }, instances: { items: [instance], active_id: instance.id, instance_count: 1, running_count: 1 }, mode_readiness: { plan: { enabled: true, ready: true }, build: { enabled: true, ready: true } }, stale: false }
  const starts: unknown[] = [], errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (route.request().method() === 'OPTIONS') { await route.fulfill({ status: 204 }); return }
    if (path === '/api/resources/by-id/style-river/preview') {
      await route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="160" height="90"><path d="M0 70 Q60 10 160 40" stroke="#317ecb" fill="none" stroke-width="5"/></svg>', headers: { 'Access-Control-Allow-Origin': '*' } }); return
    }
    let data: unknown = {}
    if (path === '/api/runtime/snapshot' || path === '/api/runtime/services') data = runtime
    else if (path === '/api/health') data = { ok: true }
    else if (path === '/api/capabilities') data = { qgis_available: true, instance_count: 1 }
    else if (path === '/api/instances') data = { instances: [instance], active_id: instance.id }
    else if (path === '/api/sessions') data = route.request().method() === 'POST' ? session : { sessions: [session] }
    else if (path.endsWith('/plans/cp-v6-sources/start')) { starts.push(route.request().postDataJSON()); data = { ...snapshot, status: 'running' } }
    else if (path.endsWith('/plans')) data = { plans: [] }
    else if (path.startsWith('/api/sessions/')) data = session
    else if (path === '/api/resources') data = [{ id: 'style-river', name: '河流线', type: 'style', format: 'qml', geometry: 'line', preview_status: 'ready', digest: 'v6-fixture-style' }]
    else if (path === '/api/llm/providers') data = { active: '', providers: [] }
    else if (path === '/api/agent-configs') data = { configs: [{ config_name: 'default', is_active: true }] }
    else if (path === '/api/guides') data = []
    else if (path === '/api/tools/catalog') data = { items: [] }
    else if (path === '/api/skill-packages') data = { packages: [] }
    await route.fulfill({ json: { success: true, data }, headers: { 'Access-Control-Allow-Origin': '*' } })
  })
  await page.goto('/chat')
  await page.getByRole('button', { name: /V6 来源验收/ }).first().click()
  await page.getByRole('textbox', { name: '消息输入' }).fill('用我给的河流文件夹和工程里的湖南省域制作专题图，缺少的铁路你去获取。')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as unknown as Harness).__v6sent.filter(message => message.type === 'chat').length)).toBe(1)
  await emit(page, { type: 'plan_event', session_id: session.session_id, data: snapshot })
  const card = page.getByRole('region', { name: `制图计划 ${plan.goal}` })
  await expect(card.getByText('河流线 · 您提供：/tmp/v6-fixture/河流.geojson')).toBeVisible()
  await expect(card.getByText('湖南省域 · 工程图层：湖南省域')).toBeVisible()
  await expect(card.getByText(/路线数据集 · 由第 1 步产生/)).toBeVisible()
  await expect(card.getByText('铁路线 · 自动获取')).toBeVisible()
  await expect(card.getByText('OpenStreetMap 在线查询 · 覆盖所需范围；适合所选比例尺')).toBeVisible()
  await expect(card.getByText('请确认', { exact: true })).toBeVisible()
  await expect(card.getByText('样式「河流线」')).toBeVisible()
  await expect(card.getByRole('img', { name: '河流线预览' })).toBeVisible()
  await page.screenshot({ path: '../docs/refactor-v6/evidence/L31-plan-sources.png', fullPage: true })
  await card.getByRole('button', { name: '开始', exact: true }).click()
  await expect.poll(() => starts).toEqual([{ version: 1 }])
  const flow = (event: string, payload: Record<string, unknown>) => emit(page, { type: 'flow_event', session_id: session.session_id, data: { event, session_id: session.session_id, flow_run_id: 'v6-run', payload: { plan_id: plan.plan_id, plan_version: 1, ...payload } } })
  await flow('flow.started', { name: plan.goal, steps: plan.steps, carto_plan: plan })
  for (const step of plan.steps) {
    await flow('flow.step_started', { step_id: step.id, run_id: `attempt-${step.id}`, attempt: 1 })
    await flow('flow.step_completed', { step_id: step.id, outputs: step.id === 'map' ? { 图件: 'output/湖南专题图.pdf' } : {} })
  }
  await flow('flow.finished', { status: 'completed', summary: '原有数据直接使用，缺少的铁路已获取并导出图件', deliverables: { 图件: 'output/湖南专题图.pdf' }, issues: [] })
  const progress = page.getByRole('region', { name: `流程进度 ${plan.goal}` })
  await expect(progress.getByText('已完成 · 3/3', { exact: true })).toBeVisible()
  await expect(progress.getByRole('button', { name: 'output/湖南专题图.pdf', exact: true }).last()).toBeVisible()
  await expect(card).toHaveCount(0)
  expect(errors).toEqual([])
})
