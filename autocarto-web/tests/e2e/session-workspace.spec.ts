import { expect, test, type Page } from '@playwright/test'
import { mockDesktopRuntime } from './helpers/desktopRuntime'

type Harness = Window & {
  __workspaceSocket: { onmessage?: (event: { data: string }) => void }
}
async function fixture(page: Page) {
  await mockDesktopRuntime(page, { port: 50183 })
  await page.addInitScript(() => {
    localStorage.removeItem('autocarto_workspace')
    localStorage.removeItem('autocarto_files_viewed')
    localStorage.setItem('autocarto_session_panel_collapsed', '0')
    class Socket {
      static OPEN = 1
      static CONNECTING = 0
      readyState = 1
      onopen: ((event: Event) => void) | null = null
      onmessage?: (event: { data: string }) => void
      constructor() {
        ;(window as unknown as Harness).__workspaceSocket = this
        setTimeout(() => this.onopen?.(new Event('open')), 0)
      }
      send() {}
      close() {
        this.readyState = 3
      }
      addEventListener() {}
      removeEventListener() {}
    }
    Object.defineProperty(window, 'WebSocket', {
      configurable: true,
      value: Socket,
    })
  })
  const session = {
    session_id: 'workspace-session',
    name: '文件区验收会话',
    project_path: '/tmp/workspace-fixture/map.qgz',
    bound_instance_id: 'bound-fixture',
    execution_mode: 'build',
    run_intent: 'execute',
    created_at: '2026-10-25T00:00:00Z',
    updated_at: '2026-10-25T00:00:00Z',
    messages: [],
  }
  const instance = {
    id: 'bound-fixture',
    pid: 1,
    project_path: session.project_path,
    status: 'running',
    qgis_version: '3.40',
    services: {},
    last_heartbeat: '2026-10-25T00:00:00Z',
  }
  const snapshot = {
    services: { agent: { status: 'running' } },
    instances: {
      items: [instance],
      active_id: instance.id,
      instance_count: 1,
      running_count: 1,
    },
    mode_readiness: {
      plan: { enabled: true, ready: true },
      build: { enabled: true, ready: true },
    },
    stale: false,
  }
  const base = {
    members: [],
    size: 4096,
    registered_at: '2026-10-24T12:00:00Z',
    modified_at: '2026-10-24T12:00:00Z',
    exists: true,
  }
  const items = [
    {
      ...base,
      id: 'f_map',
      path: '/tmp/workspace-session/exports/路线成果图.svg',
      name: '路线成果图.svg',
      kind: 'image',
      group: 'deliverable',
      origin: { type: 'tool', label: '导出版面', call_id: 'export-1' },
    },
    {
      ...base,
      id: 'f_roads',
      path: '/tmp/workspace-session/output/roads.shp',
      name: 'roads.shp',
      kind: 'vector',
      group: 'data',
      members: ['roads.dbf', 'roads.shx'],
      origin: { type: 'osm', label: 'OSM 下载' },
    },
    {
      ...base,
      id: 'f_csv',
      path: '/tmp/workspace-session/output/统计数据.csv',
      name: '统计数据.csv',
      kind: 'table',
      group: 'data',
      origin: { type: 'acquire', label: '自动获取' },
    },
    {
      ...base,
      id: 'f_run',
      path: '/tmp/workspace-session/flows/run-1/run.json',
      name: 'run.json',
      kind: 'run',
      group: 'flow',
      origin: { type: 'scan', label: '流程运行' },
    },
  ]
  const svg =
    '<svg xmlns="http://www.w3.org/2000/svg" width="480" height="320"><rect width="480" height="320" fill="#eef4eb"/><path d="M0 220L130 170L270 250L480 130" stroke="#a4cde5" stroke-width="14" fill="none"/><path d="M60 250L150 160L250 190L380 80" stroke="#d45442" stroke-width="5" fill="none"/><g fill="#c84435"><circle cx="60" cy="250" r="6"/><circle cx="150" cy="160" r="6"/><circle cx="250" cy="190" r="6"/><circle cx="380" cy="80" r="6"/></g><text x="24" y="40" font-size="20" fill="#283f42">路线成果 · 合成验收样例</text></svg>'
  await page.route('**/api/**', async (route) => {
    const pathname = new URL(route.request().url()).pathname,
      method = route.request().method()
    if (method === 'OPTIONS') {
      await route.fulfill({ status: 204 })
      return
    }
    if (pathname.endsWith('/f_map/content')) {
      await route.fulfill({ contentType: 'image/svg+xml', body: svg })
      return
    }
    if (pathname.endsWith('/f_run/content')) {
      await route.fulfill({
        contentType: 'text/plain',
        body: JSON.stringify({
          name: '行军路线图',
          status: 'completed',
          inputs: { 区域: '验收区域' },
          steps: [{ title: '导出地图', status: 'completed' }],
          result: { deliverables: { 地图: '路线成果图.svg' } },
        }),
      })
      return
    }
    let data: unknown = {}
    if (
      pathname === '/api/runtime/snapshot' ||
      pathname === '/api/runtime/services'
    )
      data = snapshot
    else if (pathname === '/api/health') data = { ok: true }
    else if (pathname === '/api/capabilities')
      data = { qgis_available: true, instance_count: 1 }
    else if (pathname === '/api/instances')
      data = { instances: [instance], active_id: instance.id }
    else if (pathname === '/api/sessions')
      data = method === 'POST' ? session : { sessions: [session] }
    else if (pathname.endsWith('/files'))
      data = { items, session_dir: '/tmp/workspace-session' }
    else if (pathname.endsWith('/f_csv/content'))
      data = {
        rows: [
          ['地区', '人口'],
          ['甲区', '1200'],
          ['乙区', '900'],
        ],
        truncated: false,
      }
    else if (pathname.endsWith('/inspect')) {
      await route.fulfill({
        status: 503,
        json: {
          success: false,
          error: '连接此会话绑定的 QGIS 后可查看数据信息或加载数据',
        },
      })
      return
    } else if (pathname.startsWith('/api/sessions/')) data = session
    else if (pathname === '/api/llm/providers')
      data = { active: '', providers: [] }
    else if (pathname === '/api/agent-configs')
      data = { configs: [{ config_name: 'default', is_active: true }] }
    else if (pathname === '/api/guides') data = []
    else if (pathname === '/api/tools/catalog') data = { items: [] }
    else if (pathname === '/api/skill-packages') data = { packages: [] }
    await route.fulfill({
      json: { success: true, data },
      headers: { 'Access-Control-Allow-Origin': '*' },
    })
  })
  await page.goto('/chat')
  await page
    .getByRole('button', { name: /文件区验收会话/ })
    .first()
    .click()
  await page.getByRole('button', { name: '工作区文件', exact: true }).click()
  await expect(page.getByText('路线成果图.svg', { exact: true })).toBeVisible()
}
for (const width of [1200, 900])
  test(`会话文件区 ${width} 像素布局与预览`, async ({ page }) => {
    await page.setViewportSize({ width, height: 820 })
    await fixture(page)
    const workspace = page.getByRole('complementary', { name: '会话工作区' })
    await expect(workspace).toHaveAttribute(
      'data-mode',
      width === 900 ? 'drawer' : 'panel',
    )
    const chat = await page.getByTestId('chat-content').boundingBox()
    expect(chat?.width || 0).toBeGreaterThanOrEqual(420)
    await expect(page.getByText('roads.shp', { exact: true })).toHaveCount(1)
    await page.screenshot({
      path: test.info().outputPath(`workspace-${width}.png`),
      fullPage: true,
    })
    await page.getByText('统计数据.csv', { exact: true }).click()
    await expect(page.getByRole('cell', { name: '甲区' })).toBeVisible()
    await page.getByRole('button', { name: '← 返回', exact: true }).click()
    await page.getByText('路线成果图.svg', { exact: true }).click()
    await expect(
      page.getByRole('img', { name: '路线成果图.svg' }),
    ).toBeVisible()
    await page
      .getByRole('button', { name: '放大', exact: true })
      .first()
      .click()
    await expect(
      page.getByRole('dialog', { name: '文件全屏预览' }),
    ).toBeVisible()
    await page.getByRole('button', { name: '退出全屏' }).click()
    await page.getByRole('button', { name: '← 返回', exact: true }).click()
    await page.getByText('roads.shp', { exact: true }).click()
    await expect(workspace.getByRole('alert')).toContainText('此会话绑定')
    await workspace
      .getByRole('button', { name: '收起工作区', exact: true })
      .click()
    await expect(workspace).toHaveCount(0)
  })
