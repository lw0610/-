import { expect, test, type Page } from '@playwright/test'
import { mockDesktopRuntime } from './helpers/desktopRuntime'
type Harness = Window & { __sent: Record<string, unknown>[]; __opened: string[]; __flowSocket: { onmessage?: (event: { data: string }) => void } }
const emit = (page: Page, message: Record<string, unknown>) => page.evaluate(message => (window as unknown as Harness).__flowSocket.onmessage?.({ data: JSON.stringify(message) }), message)

// Browser HTTP/WS contract playback only. No real Go server, model, QGIS,
// user runtime directory, native file opening or flow execution is used.
test('specified flow, grouped progress, HITL, snapshot continuation and Plan preview', async ({ page }) => {
  await mockDesktopRuntime(page, { port: 50132 })
  await page.addInitScript(() => {
    const runtime = window as unknown as Harness & { __TAURI_INTERNALS__: { invoke: (command: string, args?: { path?: string }) => Promise<unknown> } }
    runtime.__sent = []; runtime.__opened = []
    const previous = runtime.__TAURI_INTERNALS__.invoke
    runtime.__TAURI_INTERNALS__.invoke = async (command, args) => {
      if (command === 'open_local_path') { runtime.__opened.push(args?.path || ''); return null }
      return previous(command, args)
    }
    class Socket {
      static OPEN = 1; static CONNECTING = 0; readyState = 1
      onopen: ((event: Event) => void) | null = null
      onmessage?: (event: { data: string }) => void
      constructor() { runtime.__flowSocket = this; setTimeout(() => this.onopen?.(new Event('open')), 0) }
      send(raw: string) { runtime.__sent.push(JSON.parse(raw)) }
      close() { this.readyState = 3 }
      addEventListener() {} removeEventListener() {}
    }
    Object.defineProperty(window, 'WebSocket', { configurable: true, value: Socket })
  })
  const plan = { plan_status: 'enriching', enrichment: { done: 0, total: 2 }, api_version: 'flow-plan/v1', flow_id: 'demo', name: '数据检查与导出', scheme: 'default', inputs: [
    { key: 'source', label: '原文不应显示', type: 'conversation_text', required: true },
    { key: 'paper', label: '纸张', type: 'choice', required: true, default: 'A1', options: ['A0', 'A2'] },
    { key: 'folder', label: '数据文件夹', type: 'folder', required: true, remember: true },
    { key: 'review', label: '质量审查', type: 'boolean', required: false, default: false },
  ], resources: [], steps: [{ id: 's1', index: 1, title: '检查数据', role_label: '数据角色', kind: 'agent', instructions: '检查用户数据。', on_failure: 'ask', expected_outputs: [] }, { id: 's2', index: 2, title: '导出成果', role_label: '输出角色', kind: 'agent', instructions: '导出真实成果。', on_failure: 'stop', expected_outputs: [{ label: '图件', type: 'file', required: true }] }] }
  const session: Record<string, unknown> = { session_id: 'flow-session', name: '流程隔离对话', project_path: '/tmp/flow-contract.qgz', execution_mode: 'build', run_intent: 'execute', created_at: '2026-10-21T00:00:00Z', updated_at: '2026-10-21T00:00:00Z', messages: [], flow_run: null }
  const instance = { id: 'fixture-instance', pid: 1, project_path: session.project_path, status: 'running', qgis_version: '3.40', services: {}, last_heartbeat: '2026-10-21T00:00:00Z' }
  const snapshot = { services: { agent: { status: 'running' } }, instances: { items: [instance], active_id: instance.id, instance_count: 1, running_count: 1 }, mode_readiness: { plan: { enabled: true, ready: true }, build: { enabled: true, ready: true } }, stale: false }
  const errors: string[] = []; page.on('pageerror', error => errors.push(error.message))
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname, method = route.request().method()
    if (method === 'OPTIONS') { await route.fulfill({ status: 204 }); return }
    let data: unknown = {}
    if (path === '/api/runtime/snapshot' || path === '/api/runtime/services') data = snapshot
    else if (path === '/api/health') data = { ok: true }
    else if (path === '/api/capabilities') data = { qgis_available: true, instance_count: 1 }
    else if (path === '/api/instances') data = { instances: [instance], active_id: instance.id }
    else if (path === '/api/sessions') data = method === 'POST' ? session : { sessions: [session] }
    else if (path.startsWith('/api/sessions/')) data = session
    else if (path === '/api/llm/providers') data = { active: '', providers: [] }
    else if (path === '/api/agent-configs') data = { configs: [{ config_name: 'default', is_active: true }] }
    else if (path === '/api/flows') data = [{ id: 'demo', name: plan.name, enabled: true, source: 'builtin', version: '1.0', diagnostics_summary: { errors: 0, warnings: 0 } }]
    else if (path === '/api/flows/demo/inputs/memory') data = { folder: '/remembered-data' }
    else if (path === '/api/flows/demo/inputs/check') data = route.request().postDataJSON().values.folder === '/missing' ? [{ label: '数据文件夹', level: 'error', code: 'FLOW_DATA_FILE_MISSING', message: '缺少 roads.shp' }] : []
    else if (path === '/api/flows/demo/compile-status') data = { job: { flow_id: 'demo', scheme: 'default', state: 'running', done: 0, total: 3 } }
    else if (path === '/api/flows/demo') data = { meta: { id: 'demo', name: plan.name, version: '1.0' }, plan, source: 'builtin', stale: false, diagnostics: [], flow_md: '', resources: [] }
    else if (path === '/api/tools/catalog') data = { items: [] }
    else if (path === '/api/skill-packages') data = { packages: [] }
    await route.fulfill({ json: { success: true, data }, headers: { 'Access-Control-Allow-Origin': '*' } })
  })
  await page.goto('/chat?flow=demo')
  await page.getByRole('button', { name: /流程隔离对话/ }).first().click()
  await expect(page.getByRole('combobox', { name: '纸张（必填）' })).toHaveValue('A1')
  await expect(page.getByText('原文不应显示', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('textbox', { name: '数据文件夹（必填）' })).toHaveValue('/remembered-data')
  await expect(page.getByText('已解析', { exact: true })).toBeVisible()
  await page.getByRole('textbox', { name: '数据文件夹（必填）' }).fill('/missing')
  await expect(page.getByText('缺少 roads.shp')).toBeVisible()
  await expect(page.getByRole('button', { name: '发送', exact: true })).toBeDisabled()
  await page.getByRole('textbox', { name: '数据文件夹（必填）' }).fill('/remembered-data')
  await page.getByRole('textbox', { name: '消息输入' }).fill('保留完整用户原文')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as unknown as Harness).__sent.filter(message => message.type === 'task').length)).toBe(1)
  const task = await page.evaluate(() => (window as unknown as Harness).__sent.find(message => message.type === 'task'))
  expect(task).toMatchObject({ type: 'task', content: '保留完整用户原文', session_id: 'flow-session', data: { flow: { id: 'demo', inputs: { source: { $ref: 'current_user_message' }, paper: 'A1', review: false, folder: '/remembered-data' } } } })
  const flow = (event: string, payload: Record<string, unknown>) => emit(page, { type: 'flow_event', session_id: 'flow-session', data: { event, session_id: 'flow-session', flow_run_id: 'run-1', payload } })
  await flow('flow.started', { flow_id: 'demo', name: plan.name, steps: plan.steps })
  await flow('flow.step_started', { step_id: 's1', run_id: 'step-1', attempt: 1 })
  await emit(page, { type: 'agent_workflow', content: '检查图层的真实结果', session_id: 'flow-session', phase: 'tool_result', data: { flow_run_id: 'run-1', step_id: 's1', tool: 'get_layers_info', result: '图层待修复' } })
  await flow('flow.step_failed', { step_id: 's1', attempt: 1, errors: ['输入需要补充'], next: 'ask' })
  await flow('flow.step_waiting_user', { step_id: 's1', kind: 'failure_choice' })
  await emit(page, { type: 'interrupt_request', session_id: 'flow-session', data: { checkpoint_id: 'choice-cp', flow_run_id: 'run-1', step_id: 's1', interrupt_contexts: [{ is_root_cause: true, info: { type: 'decision', title: '本步骤未完成', select_mode: 'single', required: true, default: 'retry', category: 'flow_confirmation', options: [{ label: '重试本步', value: 'retry' }, { label: '跳过本步', value: 'skip' }, { label: '终止流程', value: 'stop' }] } }] } })
  const card = page.getByRole('region', { name: `流程进度 ${plan.name}` })
  await expect(card.getByText('步骤过程（1）', { exact: true })).toBeVisible()
  await card.getByText('步骤过程（1）', { exact: true }).click()
  await expect(card.getByText('图层待修复', { exact: true })).toBeVisible()
  await page.screenshot({ path: '/tmp/enio2-t32-waiting.png', fullPage: true })
  await card.getByRole('radio', { name: '跳过本步', exact: true }).check()
  await card.getByPlaceholder('可选：补充确认信息').fill('保留已有产出')
  await card.getByRole('button', { name: '确认并继续', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as unknown as Harness).__sent.find(message => message.type === 'interrupt_response'))).toMatchObject({ data: { checkpoint_id: 'choice-cp', approved: true, user_data: { option: 'skip', text: '保留已有产出' } } })
  await flow('flow.step_skipped', { step_id: 's1', reason: '用户选择跳过' })
  await flow('flow.step_started', { step_id: 's2', run_id: 'step-2', attempt: 1 })
  await flow('flow.step_completed', { step_id: 's2', summary: '已导出图件', outputs: { 图件: 'outputs/map.pdf' }, verification: { ok: true, errors: [] } })
  await flow('flow.finished', { status: 'completed_with_issues', summary: '图件已导出', deliverables: { 图件: 'outputs/map.pdf' }, issues: ['第一步由用户跳过'], run_json_path: 'flows/run-1/run.json' })
  await expect(card.getByText('完成，存在问题 · 2/2', { exact: true })).toBeVisible()
  await card.getByRole('button', { name: 'outputs/map.pdf', exact: true }).last().click()
  await expect.poll(() => page.evaluate(() => (window as unknown as Harness).__opened)).toEqual(['outputs/map.pdf'])
  await page.screenshot({ path: '/tmp/enio2-t32-completed.png', fullPage: true })
  session.flow_run = { api_version: 'flow-run/v1', event_seq: 0, flow_run_id: 'run-restart', session_id: 'flow-session', flow_id: 'demo', status: 'interrupted', started_at: '2026-10-21T00:00:00Z', steps: plan.steps.map(step => ({ id: step.id, index: step.index, title: step.title, role: step.role_label, status: 'pending', attempts: [], outputs: {} })), result: { deliverables: {}, issues: [], summary: '' }, plan }
  await page.reload()
  await page.getByRole('button', { name: /流程隔离对话/ }).first().click()
  await page.getByRole('button', { name: '继续此流程', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as unknown as Harness).__sent.find(message => message.type === 'task'))).toMatchObject({ data: { flow: { id: 'demo', resume_run_id: 'run-restart' } } })
  const resumed = await page.evaluate(() => (window as unknown as Harness).__sent.find(message => message.type === 'task'))
  expect((resumed?.data as { flow: Record<string, unknown> }).flow).not.toHaveProperty('inputs')
  await emit(page, { type: 'mode_state', session_id: 'flow-session', data: { mode: 'plan', run_intent: 'plan_only' } })
  await emit(page, { type: 'agent_workflow', session_id: 'flow-session', phase: 'tool_result', data: { tool: 'flow_preview', result: JSON.stringify({ execution_mode: 'plan', flow_id: 'demo', name: plan.name, plan, diagnostics: [], input_check: { valid: false, missing: ['数据文件'] } }) } })
  await expect(page.getByRole('region', { name: `流程预览 ${plan.name}` })).toBeVisible()
  await expect(page.getByText('需要补充：数据文件', { exact: true })).toBeVisible()
  await page.screenshot({ path: '/tmp/enio2-t32-preview.png', fullPage: true })
  expect(errors).toEqual([])
})
