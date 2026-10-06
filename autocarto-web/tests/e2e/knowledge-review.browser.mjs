// Isolated browser check. Run after `npm run build` with:
// node tests/e2e/knowledge-review.browser.mjs
// No existing Playwright config, real backend, QGIS process or user data is used.
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { readFile, mkdtemp } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../out')
const csp = JSON.parse(await readFile(path.resolve(root, '../src-tauri/tauri.conf.json'), 'utf8')).app.security.csp
const screenshotDir = await mkdtemp(path.join(tmpdir(), 'knowledge-review-ui-'))
const server = createServer(async (request, response) => {
  try {
    let pathname = decodeURIComponent(new URL(request.url, 'http://localhost').pathname)
    if (pathname === '/') pathname = '/index.html'
    if (!path.extname(pathname)) pathname += '.html'
    const file = path.resolve(root, `.${pathname}`)
    if (!file.startsWith(`${root}${path.sep}`)) throw new Error('Invalid path')
    const mime = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.json': 'application/json', '.svg': 'image/svg+xml' }
    response.writeHead(200, { 'Content-Type': mime[path.extname(file)] || 'application/octet-stream', 'Content-Security-Policy': csp })
    response.end(await readFile(file))
  } catch { response.writeHead(404); response.end('Not found') }
})
await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve))
const baseURL = `http://127.0.0.1:${server.address().port}`
const browser = await chromium.launch({ headless: true, channel: 'chrome' })
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } })
  const errors = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('dialog', (dialog) => dialog.accept())
  await page.addInitScript(({ baseURL }) => {
    window.__TAURI_INTERNALS__ = { invoke: async (command) => {
      if (command === 'get_backend_runtime') return { port: Number(new URL(baseURL).port), backend_url: baseURL, ws_url: baseURL.replace('http:', 'ws:') + '/ws' }
      if (command === 'pick_file_path') return '/tmp/mock-review-image.png'
      throw new Error(`Unexpected desktop command: ${command}`)
    } }
    window.__TAURI__ = {}
    class MockWebSocket {
      static CONNECTING = 0; static OPEN = 1; static CLOSING = 2; static CLOSED = 3
      readyState = 1; onopen = null; onmessage = null; onclose = null; onerror = null
      constructor(url) { this.url = url; window.__reviewSocket = this; setTimeout(() => this.onopen?.(new Event('open')), 0) }
      send(raw) { const data = JSON.parse(raw); if (data.type === 'interrupt_response') window.__unexpectedWSApproval = true }
      close() { this.readyState = 3 }
      addEventListener() {} removeEventListener() {}
    }
    window.WebSocket = MockWebSocket
  }, { baseURL })
  const info = { type: 'knowledge_review', session_id: 'review-session', review_id: 'review-1', revision: 1, status: 'awaiting_review', summary: { routes: 1, events: 1, spatial_entities: 3, actors: 1, callouts: 1 } }
  const interrupt = { checkpoint_id: 'review-checkpoint', interrupt_contexts: [{ id: 'review-interrupt', is_root_cause: true, info }] }
  let review = {
    ...info, source_text: '甲部队由甲地出发，经乙地前往丙地。途中在乙地发生战斗。', errors: [],
    graph: {
      task: { task_id: 'task-1', title: '行军路线示例', map_type: 'marching_route', input_crs: 'EPSG:4326' },
      actors: [{ actor_id: 'actor-1', name: '甲部队', aliases: ['先遣队'], evidence_refs: ['evidence-1'] }],
      spatial_entities: ['甲地', '乙地', '丙地'].map((name, index) => ({ entity_id: `place-${index + 1}`, name, entity_kind: 'place', factual: true, map_role: 'route_node', coordinate_source: 'manual', lon: 110 + index, lat: 30 + index, evidence_refs: ['evidence-1'] })),
      events: [{ event_id: 'event-1', name: '乙地战斗', event_type: 'battle', location_ref: 'place-2', actor_refs: ['actor-1'], time_text: '次日', description: '途中遭遇战斗。', evidence_refs: ['evidence-1'] }],
      routes: [{ route_id: 'route-1', name: '主力行军路线', topology: 'standard', side: 'friendly', evidence_refs: ['evidence-1'], paths: [{ path_id: 'path-1', role: 'main', direction: 'forward', nodes: [1, 2, 3].map((n) => ({ ref_type: 'spatial_entity', ref_id: `place-${n}` })) }] }],
      callouts: [{ callout_id: 'callout-1', anchor_ref_type: 'event', anchor_ref: 'event-1', title: '乙地战斗', body: '正文为纯文本，不执行 <script>alert(1)</script>。', template: 'image_top_text_bottom', image_enabled: false, image_path: '', width_mm: 42, leader_style: 'double' }],
      evidence: [{ evidence_id: 'evidence-1', quote: '甲部队由甲地出发，经乙地前往丙地。' }], unknown_future_field: { retained: true },
    },
  }
  const session = {
    session_id: info.session_id, name: '知识核对隔离测试', project_path: '/tmp/knowledge-review-fixture.qgz', execution_mode: 'build', run_intent: 'execute',
    created_at: '2026-09-19T00:00:00Z', updated_at: '2026-09-19T00:00:00Z',
    messages: [{ role: 'assistant', msg_type: 'interrupt', content: '请核对知识抽取结果', timestamp: '2026-09-19T00:00:00Z', data: interrupt }],
  }
  const instance = { id: 'fixture-instance', project_path: session.project_path, status: 'running', services: {}, pid: 1, qgis_version: '3.40', last_heartbeat: new Date().toISOString() }
  let saves = 0, confirms = 0
  const responseHeaders = { 'access-control-allow-origin': '*', 'access-control-allow-headers': '*', 'access-control-allow-methods': 'GET, PUT, POST, PATCH, OPTIONS' }
  await page.route('**/api/**', async (route) => {
    const url = new URL(route.request().url()), method = route.request().method()
    if (method === 'OPTIONS') { await route.fulfill({ status: 204, headers: responseHeaders }); return }
    let data = {}, status = 200
    if (url.pathname.includes('/knowledge-reviews/')) {
      if (url.pathname.endsWith('/assets')) {
        data = { asset_id: 'fixture.png', image_path: 'project_assets/fixture.png', preview_url: '/api/sessions/review-session/knowledge-reviews/review-1/assets/fixture.png' }
        review = { ...review, assets: [data] }
        await route.fulfill({ headers: responseHeaders, json: { success: true, data } }); return
      } else if (url.pathname.endsWith('/assets/fixture.png')) {
        await route.fulfill({ headers: responseHeaders, contentType: 'image/png', body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6l0sAAAAASUVORK5CYII=', 'base64') }); return
      } else if (url.pathname.endsWith('/confirm')) {
        confirms++
        if (confirms === 1) { await route.fulfill({ status: 422, headers: responseHeaders, json: { success: false, error: '模拟校验失败', data: { errors: [{ path: 'routes', message: '请检查路线节点' }] } } }); return }
        review = { ...review, status: 'compiled' }
      } else if (method === 'PUT') {
        saves++
        const body = route.request().postDataJSON()
        assert.equal(body.base_revision, review.revision)
        review = { ...review, graph: body.graph, revision: review.revision + 1 }
      }
      data = review
    } else if (url.pathname === '/api/runtime/snapshot' || url.pathname === '/api/runtime/services') {
      data = { services: {}, instances: { items: [instance], active_id: instance.id, instance_count: 1, running_count: 1 }, stale: false }
    } else if (url.pathname === '/api/sessions') data = { sessions: [session] }
    else if (url.pathname.startsWith('/api/sessions/')) data = session
    else if (url.pathname === '/api/capabilities') data = { qgis_available: true, instance_count: 1 }
    else if (url.pathname === '/api/llm/providers') data = { active: '', providers: [] }
    else if (url.pathname === '/api/agent-configs') data = { configs: [] }
    else if (url.pathname === '/api/instances') data = { instances: [instance], active_id: instance.id }
    await route.fulfill({ status, headers: responseHeaders, json: { success: true, data } })
  })
  // Different loopback origins exercise the desktop CSP allowance for backend images.
  await page.goto(`${baseURL.replace('127.0.0.1', 'localhost')}/chat.html`, { waitUntil: 'networkidle' })
  await page.getByRole('button', { name: /知识核对隔离测试/ }).first().click()
  await page.getByRole('button', { name: '查看并修改', exact: true }).waitFor()
  await page.evaluate((interrupt) => window.__reviewSocket.onmessage?.({ data: JSON.stringify({ type: 'interrupt_request', session_id: 'review-session', content: '核对后继续', data: interrupt }) }), interrupt)
  assert.equal(await page.getByLabel('知识抽取结果卡片').count(), 1)
  await page.screenshot({ path: path.join(screenshotDir, '01-card.png'), fullPage: true })
  await page.getByRole('button', { name: '查看并修改', exact: true }).click()
  await page.getByLabel('路线名称').fill('人工核对后的路线')
  await page.screenshot({ path: path.join(screenshotDir, '02-routes.png'), fullPage: true })
  for (const [label, file] of [['事件', '03-events'], ['地点', '04-places'], ['部队', '05-actors'], ['图文注记', '06-callouts']]) {
    await page.getByRole('button', { name: new RegExp(`^${label} \\(`) }).click()
    if (label === '图文注记') {
      await page.getByRole('button', { name: '选择本地图片', exact: true }).click()
      await page.getByRole('img', { name: '乙地战斗' }).waitFor()
      await page.waitForFunction(() => Array.from(document.images).some((image) => image.alt === '乙地战斗' && image.naturalWidth > 0))
    }
    await page.screenshot({ path: path.join(screenshotDir, `${file}.png`), fullPage: true })
  }
  await page.getByRole('button', { name: /^部队 \(/ }).click()
  await page.getByRole('button', { name: '删除条目', exact: true }).click()
  await page.getByRole('alert').filter({ hasText: '不能删除' }).waitFor()
  await page.getByRole('button', { name: '保存修改', exact: true }).click()
  await page.getByRole('status').filter({ hasText: '修改已保存' }).waitFor()
  assert.equal(saves, 1); assert.equal(confirms, 0)
  assert.equal(review.graph.routes[0].name, '人工核对后的路线')
  assert.equal(review.graph.callouts[0].image_path, 'project_assets/fixture.png')
  assert.deepEqual(review.graph.unknown_future_field, { retained: true })
  await page.getByRole('button', { name: '取消', exact: true }).click()
  await page.getByRole('button', { name: '开始制图', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '开始制图', exact: true }).click()
  await page.getByRole('alert').filter({ hasText: '模拟校验失败' }).waitFor()
  assert.equal(await page.getByRole('dialog').count(), 1)
  await page.getByRole('dialog').getByRole('button', { name: '开始制图', exact: true }).click()
  await page.getByRole('dialog').waitFor({ state: 'hidden' })
  assert.equal(await page.getByLabel('知识抽取结果卡片').count(), 1)
  assert.equal(await page.evaluate(() => Boolean(window.__unexpectedWSApproval)), false)
  await page.getByRole('button', { name: '查看结果', exact: true }).click()
  await page.getByRole('button', { name: '保存修改', exact: true }).waitFor()
  assert.equal(await page.getByRole('button', { name: '保存修改', exact: true }).isDisabled(), true)
  await page.screenshot({ path: path.join(screenshotDir, '07-confirmed-readonly.png'), fullPage: true })
  assert.deepEqual(errors, [])
  console.log(JSON.stringify({ ok: true, saves, confirms, screenshotDir }))
} finally {
  await browser.close()
  await new Promise((resolve) => server.close(resolve))
}
