import { expect, test } from '@playwright/test'
import { mockDesktopRuntime } from './helpers/desktopRuntime'
import type { FlowDetail } from '../../src/features/flows/types'
const initial: FlowDetail = {
  meta: {
    id: 'demo',
    name: '示例数据流程',
    description: '检查数据并输出成果',
    version: '1.0.0',
  },
  source: 'builtin',
  flow_md:
    '---\n编号: demo\n名称: 示例数据流程\n---\n## 步骤\n### 1. 检查数据\n- 负责：数据专家\n- 做法：检查用户数据\n- 完成标准：「结果」（文本）\n',
  resources: [],
  diagnostics: [],
  plan: {
    api_version: 'flow-plan/v1',
    flow_id: 'demo',
    name: '示例数据流程',
    scheme: 'default',
    inputs: [{ key: 'file', label: '数据文件', type: 'file', required: true }],
    resources: [],
    requirements: [{name:'数据文件',category:'roads',extent:'全国',scale:'自动',params:{},fields:{name:'name'},recommended_sources:[],excluded_sources:[],acquisition:'系统'}],
    steps: [
      {
        id: 's1',
        index: 1,
        title: '检查数据',
        role: 'data',
        role_label: '数据专家',
        kind: 'agent',
        instructions: '检查用户数据的字段和几何类型。',
        expected_outputs: [{ label: '结果', type: 'text', required: true }],
        suggested_tools: ['get_layers_info'],
        on_failure: 'stop',
      },
    ],
  },
}

test('flow center imports, copies, edits, previews and exports with the public contract', async ({
  page,
}) => {
  await mockDesktopRuntime(page, { port: 50131 })
  await page.addInitScript(() => {
    class Socket {
      static OPEN = 1
      readyState = 1
      onopen: ((event: Event) => void) | null = null
      constructor() {
        setTimeout(() => this.onopen?.(new Event('open')), 0)
      }
      send() {}
      close() {}
      addEventListener() {}
      removeEventListener() {}
    }
    Object.defineProperty(window, 'WebSocket', {
      value: Socket,
      configurable: true,
    })
  })
  const flows = new Map<string, FlowDetail>([
    ['demo', structuredClone(initial)],
  ])
  let exported = false
  let remembered = false
  const memory: Record<string, string> = {}
  let compiles = 0
  let roleMapping:Record<string,string> = {}
  await page.route('**/api/**', async (route) => {
    const request = route.request(),
      url = new URL(request.url()),
      path = decodeURIComponent(url.pathname)
    if (request.method() === 'OPTIONS') {
      await route.fulfill({ status: 204 })
      return
    }
    let data: unknown = {}
    if (path === '/api/agent-configs')
      data = { configs: [{ config_name: 'default', is_active: true }] }
    else if (path.endsWith('/agent-configs/default/data'))
      data = {
        meta: { default_execution_mode: 'build', plan_enabled: false },
        runtime_shared: {},
        runtime_agents: [
          {
            name: 'data',
            agent: { display_name: '数据专家', enabled: true, prompts: {} },
          },
        ],
        has_plan: false,
      }
    else if (
      path === '/api/runtime/snapshot' ||
      path === '/api/runtime/services'
    )
      data = {
        services: {},
        instances: { items: [], active_id: null },
            }
    else if (path === '/api/tools/catalog')
      data = {
        items: [
          {
            id: 'get_layers_info',
            display_name: '读取图层信息',
            description: '查看图层字段和几何类型',
            keywords: ['图层'],
            package_id: 'project-state',
          },
        ],
      }
    else if (path === '/api/flows/bindings/status') data = { orphaned_count: 0 }
    else if (path === '/api/flows') {
      if (request.method() === 'POST') {
        const body = request.postDataJSON()
        const created: FlowDetail = {
          ...structuredClone(initial),
          source: 'user',
          meta: { ...initial.meta, id: body.id, name: '新流程' },
          flow_md: body.flow_md,
        }
        flows.set(created.meta.id, created)
        data = created
      } else
        data = Array.from(flows.values()).map((flow) => ({
          ...flow.meta,
          source: flow.source,
          updated_at: '',
          enabled: true,
          diagnostics_summary: { errors: 0, warnings: 0, infos: 0 },
        }))
    } else if (path === '/api/flows/import') {
      const imported = {
        ...structuredClone(initial),
        source: 'user' as const,
        meta: { ...initial.meta, id: 'imported', name: '导入的流程' },
      }
      imported.plan!.resources = [{
        key: 'path:data/base.csv', label: '示例底图', type: 'data_list', owner: 'flow',
        location: 'resources/data/base.csv', root: '/flow/resources/data/base.csv', entry: '/flow/resources/data/base.csv',
        base_dir_from_input: '底图数据', entries: [],
      }]
      imported.plan!.inputs = [{ key: '底图数据', label: '底图数据', type: 'folder', required: true, remember: true }]
      imported.diagnostics = [
        {
          level: 'info',
          code: 'FLOW_DATA_FROM_INPUT',
          message: '请选择数据所在文件夹',
          fix: { action: 'choose_input_folder', resource_key: 'path:data/base.csv' },
        },
      ]
      flows.set('imported', imported)
      data = { flow: imported }
    } else if (path.startsWith('/api/flows/')) {
      const [, , , id, action] = path.split('/'),
        flow = flows.get(id)
      if (!flow) {
        await route.fulfill({
          status: 404,
          json: { success: false, error: '不存在' },
        })
        return
      }
      if(action==='roles') {
        if(request.method()==='PUT') {
          roleMapping=request.postDataJSON().mapping
          flow.plan!.steps[0].role_label=roleMapping['数据检查']==='custom'?'自定义数据角色':'数据专家'
          flow.plan!.steps[0].role_source=roleMapping['数据检查']?'mapping':'scheme_name'
        }
        data={flow_id:id,scheme:'default',role_fill:'auto',role_mapping:roleMapping,role_slots:[{name:'数据检查',packages:['project-state'],resolution:{source:'scheme_name',role_id:'data',role_label:flow.plan!.steps[0].role_label}}],scheme_roles:[{id:'custom',display_name:'自定义数据角色',kind:'agent',enabled:true,is_main:false,tool_packages:['project-state']}],flow_roles:[]}
      } else if (action === 'copy') {
        const body = request.postDataJSON()
        const copy = {
          ...structuredClone(flow),
          source: 'user' as const,
          meta: { ...flow.meta, id: body.new_id, name: body.new_name },
        }
        flows.set(copy.meta.id, copy)
        data = copy
      } else if (action === 'compile' || action === 'compile-status') {
        compiles++
        data = { job: null, plan: flow.plan, diagnostics: flow.diagnostics }
      } else if (action === 'inputs') {
        if (request.method() === 'PUT') {
          memory[path.split('/').at(-1)!] = request.postDataJSON().value
          remembered = memory['底图数据'] === '/tmp/map-data'
        }
        data = path.endsWith('/check') ? [] : memory
      } else if (action === 'export') {
        exported = request.postDataJSON().include_library_deps === true
        data = { zip_path: request.postDataJSON().target_path }
      } else {
        if (request.method() === 'PUT')
          flow.flow_md = request.postDataJSON().flow_md
        data = flow
      }
    } else if (path === '/api/resources') data = []
    else if (path === '/api/resources/types') data = [{ type: 'data_list', label: '数据清单', words: ['数据清单'], group: 'data', folder: false, entry_extensions: ['.csv'] }]
    await route.fulfill({
      json: { success: true, data },
      headers: { 'Access-Control-Allow-Origin': '*' },
    })
  })
  await page.goto('/flows')
  await page
    .getByRole('button', { name: /示例数据流程.*检查数据并输出成果/ })
    .click()
  await expect(page.getByRole('textbox', { name: '流程内容' })).toHaveAttribute(
    'readonly',
  )
  await expect(page.getByText('1. 检查数据', { exact: true })).toBeVisible()
  await page.getByRole('button',{name:'数据需求',exact:true}).click()
  await expect(page.getByText('字段要求：name：name')).toBeVisible()
  await page.getByRole('button',{name:'角色对应',exact:true}).click()
  await page.getByRole('combobox',{name:'数据检查对应'}).selectOption('custom')
  await page.getByRole('button',{name:'保存角色对应',exact:true}).click()
  await expect.poll(()=>roleMapping).toEqual({'数据检查':'custom'})
  await expect(page.getByText('角色对应已保存。',{exact:true})).toBeVisible()
  await page.getByRole('button',{name:'步骤预览',exact:true}).click()
  await expect(page.getByText('负责：自定义数据角色')).toBeVisible()
  await expect(page.getByText('方案角色（已指定）')).toBeVisible()
  await page.getByRole('button', { name: '复制为我的流程' }).click()
  await page.getByRole('textbox', { name: '新流程编号' }).fill('my-flow')
  await page.getByRole('textbox', { name: '新流程名称' }).fill('我的数据流程')
  await page.getByRole('button', { name: '确定', exact: true }).click()
  await expect(
    page.getByRole('textbox', { name: '流程内容' }),
  ).not.toHaveAttribute('readonly')
  await page
    .getByRole('textbox', { name: '流程内容' })
    .fill(initial.flow_md + '\n补充说明。')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('已保存')
  await expect(page.getByRole('button', { name: /体检/ })).toHaveCount(0)
  await page.getByRole('button', { name: '输入', exact: true }).click()
  await expect(page.getByText('数据文件', { exact: false })).toBeVisible()
  await page.getByRole('button', { name: '导出', exact: true }).click()
  await expect(page.getByRole('checkbox', { name: '附带依赖的模板与样式' })).toBeChecked()
  await page
    .getByRole('textbox', { name: '导出 ZIP 的完整路径' })
    .fill('/tmp/my-flow.zip')
  await page.getByRole('button', { name: '确定', exact: true }).click()
  await expect.poll(() => exported).toBe(true)
  await page.getByRole('button', { name: '导入', exact: true }).click()
  await page
    .getByRole('textbox', { name: '文件夹、ZIP 或 FLOW.md 路径' })
    .fill('/tmp/shared-flow.zip')
  await page.getByRole('button', { name: '确定', exact: true }).click()
  await expect(page.getByRole('heading', { name: '导入的流程' })).toBeVisible()
  await expect(
    page.getByRole('button', { name: '步骤预览', exact: true }),
  ).toHaveAttribute('aria-pressed', 'true')
  const beforeInput = compiles
  await page.getByRole('button', { name: '资源', exact: true }).click()
  await page.getByRole('button', { name: '选择输入文件夹', exact: true }).click()
  await page.getByRole('button', { name: '选择底图数据文件夹' }).click()
  await page.getByRole('textbox', { name: '路径', exact: true }).fill('/tmp/map-data')
  await page.getByRole('button', { name: '确定', exact: true }).click()
  await expect.poll(() => remembered).toBe(true)
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(compiles).toBe(beforeInput)
  expect(compiles).toBe(0)
  await expect(page.getByRole('button', { name: /体检/ })).toHaveCount(0)
  await page.screenshot({
    path: 'test-results/flow-center.png',
    fullPage: true,
  })
})

test('save directly updates the step preview without compile requests and keeps trial run beside save', async ({ page }) => {
  await mockDesktopRuntime(page, { port: 50131 })
  await page.addInitScript(() => {
    class Socket {
      static OPEN = 1
      readyState = 1
      onopen: ((event: Event) => void) | null = null
      constructor() { setTimeout(() => this.onopen?.(new Event('open')), 0) }
      send() {}
      close() {}
      addEventListener() {}
      removeEventListener() {}
    }
    Object.defineProperty(window, 'WebSocket', { value: Socket, configurable: true })
  })
  const flow: FlowDetail = { ...structuredClone(initial), source: 'user' }
  // Legacy metadata must not make the new UI display or start enhancement jobs.
  flow.plan!.plan_status = 'partial'
  flow.plan!.enrichment = { done: 2, total: 2, failed: [{ step_id: 's1', title: '检查数据', reason: 'invalid_json' }] }
  flow.plan!.steps[0].enrichment = 'failed'
  const draft = flow.flow_md.replace('检查数据', '核对空间数据').replace('检查用户数据', '核对输入数据的坐标系和几何类型')
  const compileRequests: string[] = []
  const saveRequests: unknown[] = []
  const unexpectedMutations: string[] = []
  await page.route('**/api/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (request.method() === 'OPTIONS') { await route.fulfill({ status: 204 }); return }
    if (/\/compile(?:-status)?$/.test(path)) {
      compileRequests.push(`${request.method()} ${path}`)
      await route.fulfill({ status: 410, json: { success: false, error: 'Flow compilation is no longer available' } })
      return
    }
    if (request.method() !== 'GET' && !(request.method() === 'PUT' && path === '/api/flows/demo')) {
      unexpectedMutations.push(`${request.method()} ${path}`)
      await route.fulfill({ status: 400, json: { success: false, error: 'Unexpected mutation in preview test' } })
      return
    }
    let data: unknown = {}
    if (path === '/api/agent-configs') data = { configs: [{ config_name: 'default', is_active: true }] }
    else if (path === '/api/agent-configs/default/data') data = { runtime_agents: [], meta: {}, runtime_shared: {} }
    else if (path === '/api/runtime/snapshot' || path === '/api/runtime/services') data = { services: {}, instances: { items: [], active_id: null } }
    else if (path === '/api/tools/catalog') data = { items: [] }
    else if (path === '/api/flows/bindings/status') data = { orphaned_count: 0 }
    else if (path === '/api/flows') data = [{ ...flow.meta, source: 'user', updated_at: '', enabled: true, diagnostics_summary: { errors: 0, warnings: 0, infos: 0 } }]
    else if (path === '/api/flows/demo') {
      if (request.method() === 'PUT') {
        const body = request.postDataJSON()
        saveRequests.push(body)
        flow.flow_md = body.flow_md
        flow.plan!.steps[0].title = '核对空间数据'
        flow.plan!.steps[0].instructions = '核对输入数据的坐标系和几何类型'
      }
      data = flow
    } else if (path === '/api/resources' || path === '/api/resources/types') data = []
    await route.fulfill({ json: { success: true, data }, headers: { 'Access-Control-Allow-Origin': '*' } })
  })
  await page.goto('/flows')
  await page.getByRole('button', { name: /示例数据流程.*检查数据并输出成果/ }).click()
  const editor = page.getByRole('textbox', { name: '流程内容' })
  const save = page.getByRole('button', { name: '保存', exact: true })
  const trialRun = page.getByRole('link', { name: '试运行', exact: true })
  const expectActionsInOneRow = async () => {
    const saveBox = await save.boundingBox()
    const trialBox = await trialRun.boundingBox()
    expect(saveBox).not.toBeNull()
    expect(trialBox).not.toBeNull()
    expect(Math.abs(saveBox!.y + saveBox!.height / 2 - trialBox!.y - trialBox!.height / 2)).toBeLessThanOrEqual(2)
  }
  await expect(page.getByRole('heading', { name: '1. 检查数据', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: /重新编译/ })).toHaveCount(0)
  await expect(page.getByLabel('重新编译状态', { exact: true })).toHaveCount(0)
  await expect(page.getByText(/未完善|正在完善|模型返回格式不正确/)).toHaveCount(0)
  await editor.fill(draft)
  await expect(page.getByRole('heading', { name: '1. 检查数据', exact: true })).toBeVisible()
  await expect(trialRun).toHaveAttribute('aria-disabled', 'true')
  await expectActionsInOneRow()

  // Saving from another tab must reveal the updated steps immediately.
  await page.getByRole('button', { name: '输入', exact: true }).click()
  await save.click()
  await expect(page.getByRole('status')).toContainText('已保存')
  await expect(page.getByRole('button', { name: '步骤预览', exact: true })).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByRole('heading', { name: '1. 核对空间数据', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: '1. 检查数据', exact: true })).toHaveCount(0)
  await expect(page.locator('summary').filter({ hasText: '核对输入数据的坐标系和几何类型' })).toBeVisible()
  await expect(editor).toHaveValue(draft)
  await expect(save).toBeDisabled()
  await expect(trialRun).toHaveAttribute('aria-disabled', 'false')
  await expectActionsInOneRow()
  await page.screenshot({ path: 'test-results/flow-save-preview.png', fullPage: true })

  await page.reload()
  await page.getByRole('button', { name: /示例数据流程.*检查数据并输出成果/ }).click()
  await expect(page.getByRole('heading', { name: '1. 核对空间数据', exact: true })).toBeVisible()
  expect(saveRequests).toEqual([{ flow_md: draft }])
  expect(compileRequests).toEqual([])
  expect(unexpectedMutations).toEqual([])
})

test('resource library keeps explicit overrides visible and shows affected flows before changing them', async ({ page }) => {
  await mockDesktopRuntime(page, { port: 50131 })
  const base = { id: 'layout.example', name: '四角版式', type: 'layout', origin: 'library_builtin', owner: 'library_builtin', version: '1.0.0', path: '/builtin/example.layout.yaml', root: '/builtin', entry: 'example.layout.yaml', size: 100 }
  const mine = { ...base, id: 'layout.u-a', name: '我的四角版式', origin: 'library_user', owner: 'library_user', overrides: base.id }
  const style = { ...base, id: 'style.roads', name: '道路线条', type: 'style', path: '/builtin/roads.qml', entry: 'roads.qml' }
  const styleSet = { ...base, id: 'style_set.basemap', name: '底图样式组合', type: 'style_set', path: '/builtin/basemap', entry: 'manifest.yaml' }
  let copied = false, replaced = false, removed = false
  let userItem: typeof mine | null = null
  const resolved = (item: typeof base) => ({ ...item, key: item.id, label: item.name, location: item.path, entry: item.path, digest: 'sha256:123456789abcdef', entries: [] })
  await page.route('**/api/**', async route => {
    const request = route.request(), url = new URL(request.url()), path = decodeURIComponent(url.pathname)
    if (request.method() === 'OPTIONS') { await route.fulfill({ status: 204 }); return }
    let data: unknown = {}
    if (path === '/api/agent-configs') data = { configs: [{ config_name: 'default', is_active: true }] }
    else if (path === '/api/agent-configs/default/data') data = { runtime_agents: [], meta: {}, runtime_shared: {} }
    else if (path === '/api/runtime/snapshot' || path === '/api/runtime/services') data = { services: {}, instances: { items: [], active_id: null }, stale: false }
    else if (path === '/api/tools/catalog') data = { items: [] }
    else if (path === '/api/flows') data = []
    else if (path === '/api/flows/bindings/status') data = { orphaned_count: 0 }
    else if (path === '/api/resources/types') data = [
      { type: 'layout', public: true, label: '版面', words: ['版面', '版式'], group: 'layouts', folder: false, entry_extensions: ['.qpt', '.layout.yaml'] },
      { type: 'style', public: true, label: '样式', words: ['样式'], group: 'styles', folder: false, entry_extensions: ['.qml'] },
      { type: 'style_set', public: true, label: '样式集', words: ['样式集'], group: 'styles', folder: true, entry_extensions: [] },
    ]
    else if (path === '/api/resources' && url.searchParams.get('scope') === 'retired') data = []
    else if (path === '/api/resources') data = [...(userItem ? [{ ...base, overridden_by: userItem.id }, userItem] : [base]), style, styleSet]
    else if (path === `/api/resources/${base.id}/copy`) {
      copied = request.postDataJSON().override === true
      userItem = { ...mine, name: request.postDataJSON().name }
      data = resolved(userItem)
    } else if (path.endsWith('/references')) data = [{ flow_id: 'demo', flow_name: '示例数据流程', source: 'builtin', via: 'lock' }]
    else if (path === `/api/resources/${mine.id}/content`) {
      replaced = request.postDataJSON().source_path === '/tmp/replacement.qpt'
      userItem = { ...userItem!, version: '1.0.1' }
      data = resolved(userItem)
    } else if (path === `/api/resources/${mine.id}` && request.method() === 'DELETE') {
      removed = url.searchParams.get('force') === 'true'
      userItem = null
      data = { deleted: true }
    } else if (path.startsWith('/api/resources/by-id/')) data = resolved([base, style, styleSet, userItem].find(item => item && path.endsWith(item.id))!)
    await route.fulfill({ json: { success: true, data }, headers: { 'Access-Control-Allow-Origin': '*' } })
  })
  await page.goto('/flows')
  await page.getByRole('button', { name: '模板与样式', exact: true }).click()
  const filters = page.getByLabel('模板与样式筛选')
  const list = page.getByRole('list', { name: '模板与样式列表' })
  const categories = [
    { name: '全部', items: [base, style, styleSet] },
    { name: '版面', items: [base] },
    { name: '样式', items: [style] },
    { name: '样式集', items: [styleSet] },
  ]
  await expect(filters.getByRole('button', { name: '全部', exact: true })).toHaveAttribute('aria-pressed', 'true')
  await expect(filters.getByRole('button', { name: '版式', exact: true })).toHaveCount(0)
  for (const colorScheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme })
    for (const category of categories) {
      const selected = filters.getByRole('button', { name: category.name, exact: true })
      await selected.click()
      await expect(selected).toHaveAttribute('aria-pressed', 'true')
      await expect(filters.locator('button[aria-pressed="true"]')).toHaveCount(1)
      await expect(filters.locator('button[aria-pressed="false"]')).toHaveCount(3)
      await expect(list.getByRole('listitem')).toHaveCount(category.items.length)
      for (const item of [base, style, styleSet]) {
        await expect(list.getByRole('button', { name: item.name, exact: true })).toHaveCount(category.items.includes(item) ? 1 : 0)
      }
      await expect.poll(async () => {
        const colors = await filters.locator('button').evaluateAll(buttons => buttons.map(button => ({
          selected: button.getAttribute('aria-pressed') === 'true',
          background: getComputedStyle(button).backgroundColor,
          foreground: getComputedStyle(button).color,
        })))
        const active = colors.find(color => color.selected)!
        return colors.filter(color => !color.selected).every(color => color.background !== active.background && color.foreground !== active.foreground)
      }).toBe(true)
      await expect(list).not.toContainText('版本')
      await expect(list).not.toContainText(base.version)
    }
    await filters.getByRole('button', { name: '版面', exact: true }).click()
    await list.getByRole('button', { name: base.name, exact: true }).click()
    const detail = page.getByRole('heading', { name: '四角版式 · 版面', exact: true }).locator('..')
    await expect(detail).toBeVisible()
    await expect(detail).not.toContainText('版本')
    await expect(detail).not.toContainText(base.version)
    await page.screenshot({ path: `test-results/resource-library-filters-${colorScheme}.png`, fullPage: true })
  }
  await page.emulateMedia({ colorScheme: 'light' })
  await page.getByRole('button', { name: '复制并覆盖内置' }).click()
  await page.getByRole('textbox', { name: '资源名称', exact: true }).fill('我的四角版式')
  await page.getByRole('button', { name: '确定', exact: true }).click()
  await expect.poll(() => copied).toBe(true)
  await expect(page.getByText('已被用户资源覆盖：我的四角版式')).toBeVisible()
  await expect(page.getByText('覆盖内置「四角版式」')).toBeVisible()
  await page.getByRole('button', { name: '四角版式', exact: true }).click()
  await expect(page.getByRole('heading', { name: '四角版式 · 版面' })).toBeVisible()
  await page.getByRole('button', { name: '替换内容' }).click()
  await expect(page.getByRole('dialog').getByText(/示例数据流程（demo）/)).toBeVisible()
  expect(replaced).toBe(false)
  await page.getByRole('textbox', { name: '替换为文件或文件夹' }).fill('/tmp/replacement.qpt')
  await page.getByRole('button', { name: '确定', exact: true }).click()
  await expect.poll(() => replaced).toBe(true)
  await page.getByRole('button', { name: '删除', exact: true }).click()
  await expect(page.getByRole('dialog').getByText(/示例数据流程（demo）/)).toBeVisible()
  expect(removed).toBe(false)
  await page.getByRole('button', { name: '确认删除' }).click()
  await expect.poll(() => removed).toBe(true)
  await expect(page.getByRole('button', { name: '四角版式', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '我的四角版式', exact: true })).toHaveCount(0)
})
