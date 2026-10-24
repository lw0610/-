import { expect, test, type APIRequestContext } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { mkdir, readFile, writeFile, stat } from 'node:fs/promises'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { DEFAULT_DESKTOP_RUNTIME_PORT, mockDesktopRuntime } from './helpers/desktopRuntime'

// Explicitly opt in: uses a running, test-owned production Go HTTP/WS server.
// The only browser shim supplies the desktop runtime URL; no HTTP/WS is mocked.
const base = `http://127.0.0.1:${DEFAULT_DESKTOP_RUNTIME_PORT}`
async function api<T>(request: APIRequestContext, method: 'GET'|'POST'|'PUT'|'DELETE', path: string, data?: unknown) {
  const response = await request.fetch(`${base}/api${path}`, { method, data, headers: { 'X-AutoCarto-Contract-Version': 'v1' } })
  expect(response.ok(), `${method} ${path}: ${await response.text()}`).toBeTruthy()
  const envelope = await response.json()
  expect(envelope.success).toBe(true)
  return envelope.data as T
}

test('A3 real HTTP and disk: import, repair, edit, resources, export, tools and v2 scheme', async ({ page, request }) => {
  test.skip(process.env.AUTOCARTO_REFACTOR_UI_LIVE !== '1', 'requires an isolated production Go backend')
  const work = join(tmpdir(), `enio2-t53-browser-${Date.now()}`)
  const source = join(work, 'received-flow'), dataDir = join(work, 'actual-data')
  await mkdir(join(source, 'resources/data'), { recursive: true }); await mkdir(dataDir, { recursive: true })
  await writeFile(join(source, 'resources/data/核验清单.csv'), '名称,路径,类型,必需\n核验点,points.geojson,矢量,是\n')
  await writeFile(join(dataDir, 'points.geojson'), JSON.stringify({ type: 'FeatureCollection', features: [{ type: 'Feature', properties: { name: '核验点' }, geometry: { type: 'Point', coordinates: [110, 30] } }] }))
  const resourceLabel = `核验清单-${Date.now()}`
  const markdown = `---
编号: ui-acceptance-import
名称: 界面导入验收
说明: 真实HTTP、磁盘资源绑定与人工确认
版本: 1.0.0
---

## 适用场景
只用于隔离验收。

## 需要准备
- 原文（必填）：本条对话原文
- 纸张（默认 A4 横向；可选值：A3 横向、A4 横向）
- 质量审查（是/否，默认否）
- 可选数据（可选）：可选文件夹
- 可选标题（可选）：可选说明

## 使用的资源
- 数据清单「${resourceLabel}」：resources/data/核验清单.csv

## 步骤
### 第1步 核验输入
- 负责：用户
- 做法：请确认「原文」与「${resourceLabel}」对应。
- 完成标准：「输入确认」（确认结果）
- 失败时：询问用户

### 第2步 确认交付
- 负责：用户
- 做法：确认「输入确认」后完成。
- 完成标准：「交付确认」（确认结果）

## 必须遵守
- 不调用外部模型，不虚构成果。

## 交付
交付「输入确认」和「交付确认」。
`
  await writeFile(join(source, 'FLOW.md'), markdown)
  for (const id of ['ui-acceptance-import','ui-authored']) {
    const removed = await request.delete(`${base}/api/flows/${id}`)
    expect([200,404]).toContain(removed.status())
  }
  const oldDocument = await request.delete(`${base}/api/resources/doc/${encodeURIComponent('界面验收说明')}`)
  expect([200,404]).toContain(oldDocument.status())
  await api(request, 'POST', '/agent-configs/default/activate')
  await mockDesktopRuntime(page)
  page.on('dialog', dialog => dialog.type() === 'beforeunload' ? dialog.accept() : dialog.dismiss())
  const errors: string[] = []; page.on('pageerror', error => errors.push(error.message))
  let finished: any
  const frames: Array<{ direction: string; message: any }> = []
  let taskSocket: object | undefined
  const taskSocketCloses: string[] = []
  page.on('websocket', socket => {
    socket.on('close', () => { if (socket === taskSocket) taskSocketCloses.push(socket.url()) })
    const capture = (direction: string, payload: string | Buffer) => {
      try {
        const message = JSON.parse(String(payload))
        if (direction === 'sent' && message.type === 'task') taskSocket = socket
        if (['task','error','task_ack','flow_event','interrupt_request','interrupt_response'].includes(message.type)) {
          frames.push({ direction, message })
          writeFileSync('/tmp/enio2-t53-ui-frames.json', JSON.stringify(frames, null, 2))
        }
        if (message.type === 'flow_event' && message.data?.event === 'flow.finished') finished = message.data
      } catch {}
    }
    socket.on('framereceived', frame => capture('received', frame.payload))
    socket.on('framesent', frame => capture('sent', frame.payload))
  })
  const responses: string[] = []; page.on('response', response => { if (response.url().startsWith(base+'/api/')) responses.push(`${response.status()} ${response.request().method()} ${new URL(response.url()).pathname}`) })
  await page.goto('/flows?scheme=default')
  await expect(page.getByRole('button', { name: /通用数据成图/ })).toBeVisible()

  // B: import a real directory, repair a missing relative data file via the UI.
  await page.getByRole('button', { name: '导入', exact: true }).click()
  const importDialog = page.getByRole('dialog', { name: '导入流程包' })
  await importDialog.getByLabel('文件夹、ZIP 或 FLOW.md 路径').fill(source)
  await importDialog.getByRole('button', { name: '确定', exact: true }).click()
  await expect(importDialog).not.toBeVisible()
  await expect(page.getByRole('heading', { name: /错误/ })).toBeVisible()
  await page.screenshot({ path: '/tmp/enio2-t53-import-diagnostics.png', fullPage: true })
  await page.getByRole('button', { name: '选择文件夹', exact: true }).first().click()
  const binding = page.getByRole('dialog', { name: '绑定数据基准目录' })
  await binding.getByLabel('数据所在文件夹').fill(dataDir)
  await binding.getByRole('button', { name: '确定', exact: true }).click()
  await expect(binding).not.toBeVisible()
  await expect(page.getByRole('heading', { name: /错误/ })).toHaveCount(0)
  const imported = await api<any>(request, 'GET', '/flows/ui-acceptance-import?scheme=default')
  expect(imported.plan.resources[0].entries[0]).toMatchObject({ name: '核验点', exists: true })
  expect(imported.plan.resources[0].entries[0].path).toBe(join(dataDir, 'points.geojson'))
  expect(imported.plan.steps.map((step: {kind:string}) => step.kind)).toEqual(['confirm','confirm'])
  await page.getByRole('button', { name: '步骤预览', exact: true }).click()
  await expect(page.getByRole('heading', { name: '1. 核验输入', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '资源', exact: true }).click()
  await expect(page.getByText(join(dataDir, 'points.geojson'), { exact: false })).toBeVisible()
  await page.screenshot({ path: '/tmp/enio2-t53-resource-binding.png', fullPage: true })

  // C: create from the real template, edit, parse, use capability handbook.
  await page.getByRole('button', { name: '新建流程', exact: true }).click()
  const create = page.getByRole('dialog', { name: '从模板新建流程' })
  await create.getByLabel('流程编号（小写字母、数字、连字符）').fill('ui-authored')
  await create.getByLabel('流程名称', { exact: true }).fill('界面新写流程')
  await create.getByRole('button', { name: '确定', exact: true }).click()
  await expect(create).not.toBeVisible()
  await expect(page.getByRole('heading', { name: '界面新写流程', exact: true })).toBeVisible()
  await expect(page.getByRole('textbox', { name: '流程内容' })).toHaveValue(/编号: ui-authored/)
  const authored = markdown.replace('ui-acceptance-import','ui-authored').replace('界面导入验收','界面新写流程').replace(`## 使用的资源\n- 数据清单「${resourceLabel}」：resources/data/核验清单.csv`,'## 使用的资源').replace(`与「${resourceLabel}」对应`,'准确')
  const editor = page.getByRole('textbox', { name: '流程内容' })
  await expect(editor).not.toHaveAttribute('readonly', '')
  await editor.fill(authored)
  await expect(editor).toHaveValue(authored)
  await expect(page.getByRole('button', { name: '保存', exact: true })).toBeEnabled()
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('已保存')
  await expect.poll(async () => (await api<any>(request, 'GET', '/flows/ui-authored?scheme=default')).flow_md).toContain('### 第2步 确认交付')
  await page.getByRole('button', { name: '步骤预览', exact: true }).click()
  await expect(page.getByRole('heading', { name: '1. 核验输入', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '能力手册', exact: true }).click()
  await page.getByRole('textbox', { name: '搜索能力' }).fill('排版')
  await expect(page.getByText('按版式一次排版', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '输入', exact: true }).click()
  await expect(page.locator('body')).toContainText('对话原文')
  await page.screenshot({ path: '/tmp/enio2-t53-authored-inputs.png', fullPage: true })
  await page.getByRole('button', { name: '导出', exact: true }).click()
  const exportDialog = page.getByRole('dialog', { name: '导出流程包' }), target = join(work, 'authored.zip')
  await exportDialog.getByLabel('导出 ZIP 的完整路径').fill(target)
  await exportDialog.getByRole('button', { name: '确定', exact: true }).click()
  await expect(exportDialog).not.toBeVisible()
  await expect.poll(async () => (await stat(target).catch(() => ({ size: 0 }))).size).toBeGreaterThan(0)
  expect((await readFile(target)).subarray(0,2).toString()).toBe('PK')

  // Public resource import and preview also use the real filesystem/API.
  const document = join(work, '界面验收说明.md')
  await writeFile(document, '# 真实资源预览\n这段文本来自磁盘文件。\n')
  await page.getByRole('button', { name: '公共资源', exact: true }).click()
  await page.getByRole('combobox', { name: /^资源类型/ }).selectOption('doc')
  await page.getByRole('button', { name: '导入公共资源', exact: true }).click()
  const resourceDialog = page.getByRole('dialog', { name: '导入公共资源', exact: true })
  await resourceDialog.getByLabel('资源文件或文件夹路径').fill(document)
  await resourceDialog.getByRole('button', { name: '确定', exact: true }).click()
  await expect(page.getByText('这段文本来自磁盘文件。', { exact: false })).toBeVisible()
  await page.screenshot({ path: '/tmp/enio2-t53-public-resource.png', fullPage: true })

  await page.goto('/settings/tools')
  await page.getByRole('button', { name: '能力手册', exact: true }).click()
  await page.getByPlaceholder('搜索工具...').fill('排版')
  await expect(page.getByText('按版式一次排版', { exact: true })).toBeVisible()
  await page.screenshot({ path: '/tmp/enio2-t53-tool-handbook.png', fullPage: true })

  // D: normal v2 profile save retains existing roles and enables only intended flows.
  await page.goto('/settings/models-agents')
  await page.getByRole('button', { name: '智能体方案', exact: true }).click()
  await page.getByPlaceholder('搜索方案...').fill('default')
  await page.getByRole('button', { name: '编辑', exact: true }).first().click()
  const scheme = page.getByRole('dialog', { name: '编辑智能体方案 · default', exact: true })
  await scheme.getByRole('group', { name: /角色 数据工程专家/ }).getByRole('button', { name: '编辑', exact: true }).click()
  const role = page.getByRole('dialog', { name: /^编辑角色 数据工程专家/ })
  await expect(role.getByLabel('角色类型')).toHaveValue('agent')
  await expect(role.getByRole('checkbox', { name: /所有角色自动拥有/ }).first()).toBeDisabled()
  await expect(role.getByText('隐藏执行器工具', { exact: true })).toHaveCount(0)
  await role.getByRole('textbox', { name: '搜索工具包' }).fill('几何')
  const geometry = role.getByRole('checkbox', { name: /^几何处理与叠加/ })
  const hadGeometry = await geometry.isChecked()
  await geometry.setChecked(!hadGeometry)
  await role.getByRole('button', { name: '保存', exact: true }).click()
  await scheme.getByRole('checkbox', { name: /界面新写流程/ }).check()
  await scheme.getByRole('checkbox', { name: /界面导入验收/ }).check()
  const schemeSaved = page.waitForResponse(response => response.url() === `${base}/api/agent-configs/default` && response.request().method() === 'PUT')
  await scheme.getByRole('button', { name: '保存当前方案', exact: true }).click()
  expect((await schemeSaved).ok()).toBe(true)
  await expect(scheme.getByRole('button', { name: '保存当前方案', exact: true })).toBeDisabled()
  await expect.poll(async () => (await api<any>(request, 'GET', '/agent-configs/default/data')).flows?.enabled).toContain('ui-authored')
  const saved = await api<any>(request, 'GET', '/agent-configs/default/data')
  expect(saved.version).toBe('2.0'); expect(saved.flows.enabled).toContain('ui-authored')
  expect(saved.runtime_agents.find((entry:{name:string})=>entry.name==='data_engineer').agent.tool_packages.includes('vector-geometry')).toBe(!hadGeometry)
  expect(saved.runtime_agents.every((entry:{agent:object})=>!('skill_packages' in entry.agent))).toBe(true)
  await page.screenshot({ path: '/tmp/enio2-t53-scheme.png', fullPage: true })
  await scheme.getByRole('link', { name: '界面新写流程', exact: true }).click()
  await expect(page).toHaveURL(/\/flows\?flow=ui-authored&scheme=default$/)
  await expect(page.getByRole('textbox', { name: '流程内容' })).toHaveValue(/界面新写流程/)
  // Opening the saved flow refreshes its plan after the scheme changes.
  await page.getByRole('button', { name: '步骤预览', exact: true }).click()
  await expect(page.getByRole('heading', { name: '1. 核验输入', exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: '试运行', exact: true })).toHaveAttribute('aria-disabled', 'false')
  await page.getByRole('link', { name: '试运行', exact: true }).click()
  await expect(page).toHaveURL(/\/chat\?flow=ui-authored$/)
  await expect(page.getByRole('combobox', { name: '选择流程' })).toHaveValue('ui-authored')
  // A: inspect an actual builtin form, then execute a two-confirmation flow
  // through production task/interrupt/flow events. No model or map output is faked.
  const selector = page.getByRole('combobox', { name: '选择流程' })
  await expect(selector).toBeEnabled()
  await selector.selectOption('marching-route')
  await expect(page.getByRole('combobox', { name: '纸张' })).toHaveValue('A1 横向')
  await expect(page.getByRole('checkbox', { name: '质量审查' })).not.toBeChecked()
  await expect(page.getByRole('textbox', { name: '路线数据集', exact: true })).toHaveValue('')
  await expect(page.getByRole('textbox', { name: '地图标题', exact: true })).toHaveValue('')
  await page.screenshot({ path: '/tmp/enio2-t53-builtin-form.png', fullPage: true })
  await selector.selectOption('ui-authored')
  await expect(page.getByRole('combobox', { name: '纸张' })).toHaveValue('A4 横向')
  await expect(page.getByRole('textbox', { name: '原文（必填）', exact: true })).toHaveCount(0)
  await expect(page.getByRole('textbox', { name: '可选数据', exact: true })).toHaveValue('')
  await expect(page.getByRole('textbox', { name: '可选标题', exact: true })).toHaveValue('')
  await page.screenshot({ path: '/tmp/enio2-t53-chat-form.png', fullPage: true })
  await page.getByRole('textbox', { name: '消息输入' }).fill('真实隔离验收原文，保留在本条消息。')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect.poll(() => frames.find(frame => frame.direction === 'sent' && frame.message.type === 'task')?.message.idempotency_key).toMatch(/^task-.+/)
  const task = frames.find(frame => frame.direction === 'sent' && frame.message.type === 'task')!.message
  expect(Object.keys(task.data.flow.inputs).sort()).toEqual(['input_1','input_2','input_3'])
  expect(task.data.flow.inputs).toMatchObject({ input_1: { $ref: 'current_user_message' }, input_2: 'A4 横向', input_3: false })
  const progress = page.getByRole('region', { name: '流程进度 界面新写流程', exact: true })
  await expect(progress).toBeVisible()
  await expect(progress.getByRole('listitem', { name: '第 1 步 核验输入', exact: true })).toContainText('等待确认')
  await page.screenshot({ path: '/tmp/enio2-t53-live-waiting.png', fullPage: true })
  await progress.getByRole('button', { name: '确认并继续', exact: true }).first().click()
  await expect(progress.getByRole('listitem', { name: '第 2 步 确认交付', exact: true })).toContainText('等待确认')
  await progress.getByRole('button', { name: '确认并继续', exact: true }).last().click()
  await expect(progress.getByText('已完成 · 2/2', { exact: true })).toBeVisible()
  await expect.poll(() => finished?.payload.status).toBe('completed')
  const runPath = finished.payload.run_json_path
  const run = JSON.parse(await readFile(runPath, 'utf8'))
  expect(run.status).toBe('completed'); expect(run.steps).toHaveLength(2)
  expect(run.steps.every((step: { status: string }) => step.status === 'completed')).toBe(true)
  expect(run.event_seq).toBeGreaterThan(0)
  await expect(page.getByTestId('backend-status-banner')).toHaveCount(0)
  expect(taskSocketCloses).toEqual([])
  await page.screenshot({ path: '/tmp/enio2-t53-live-completed.png', fullPage: true })
  await writeFile('/tmp/enio2-t53-ui-run.json', JSON.stringify(run, null, 2))

  expect(errors).toEqual([])
  await writeFile('/tmp/enio2-t53-ui-http-evidence.json',JSON.stringify({work, imported, saved, responses, pageerrors:errors, task_socket_closes:taskSocketCloses},null,2))
})
