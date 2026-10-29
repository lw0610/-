import { expect, test } from '@playwright/test'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { mockDesktopRuntime } from './helpers/desktopRuntime'

test('visual resource library filters two kinds and preserves referenced resources until confirmation', async ({page}) => {
  await mockDesktopRuntime(page, {port: 50131})
  const style = {id: 'style.river', name: '河流线', type: 'style', geometry: 'line', label_field: 'NAME', tags: ['水系'], description: '显示河流与河名', owner: 'library_builtin', origin: 'library_builtin', path: '', size: 100, preview_status: 'ready'}
  const layout = {...style, id: 'layout.example', name: '四角对齐版式', type: 'layout', paper: 'A4', orientation: '横向'}
  let copied = false, replaced = false, removed = false
  let mine: typeof layout | null = null
  const resource = (item: typeof layout) => ({...item, key: item.id, label: item.name, location: '', entries: [], digest: 'sha256:fixture'})
  await page.route('**/api/**', async route => {
    const request = route.request(), url = new URL(request.url()), path = decodeURIComponent(url.pathname)
    if (request.method() === 'OPTIONS') { await route.fulfill({status: 204}); return }
    let data: unknown = {}
    if (path.endsWith('/preview')) {
      const file = path.includes('style.river') ? 'styles/河流线/preview.png' : 'layouts/四角对齐版式/preview.png'
      await route.fulfill({body: await readFile(resolve('../autocarto/resources/system', file)), contentType: 'image/png'}); return
    }
    if (path === '/api/guides') data = []
    else if (path === '/api/resources/types') data = ['style', 'layout'].map(type => ({type, public: true}))
    else if (path === '/api/resources') data = [style, layout, ...(mine ? [mine] : [])]
    else if (path === '/api/runtime/snapshot' || path === '/api/runtime/services') data = {services: {}, instances: {items: [], active_id: null}}
    else if (path === '/api/agent-configs') data = {configs: []}
    else if (path.endsWith('/references')) data = [{flow_id: 'marching-route', flow_name: '行军路线图', source: 'builtin', via: 'lock'}]
    else if (path === '/api/resources/layout.example/copy') { copied = request.postDataJSON().override === false; mine = {...layout, id: 'layout.mine', owner: 'library_user', origin: 'library_user', name: request.postDataJSON().name}; data = resource(mine) }
    else if (path === '/api/resources/layout.mine/content') { replaced = request.postDataJSON().source_path === '/tmp/replacement.qpt'; data = resource(mine!) }
    else if (path === '/api/resources/layout.mine' && request.method() === 'DELETE') { removed = url.searchParams.get('force') === 'true'; mine = null; data = {deleted: true} }
    else if (path.startsWith('/api/resources/by-id/')) data = resource([style, layout, mine].find(item => item && path.endsWith(item.id))! as typeof layout)
    await route.fulfill({json: {success: true, data}, headers: {'Access-Control-Allow-Origin': '*'}})
  })
  await page.goto('/guides'); await page.getByRole('button', {name: '样式与版面', exact: true}).click()
  const tabs = page.getByRole('tablist', {name: '资源种类'}), list = page.getByRole('list', {name: '样式与版面列表'})
  await expect(tabs.getByRole('tab')).toHaveCount(2); await expect(list).toContainText('河流线')
  await page.getByLabel('搜索样式与版面').fill('水系'); await expect(list.getByRole('listitem')).toHaveCount(1); await page.getByLabel('搜索样式与版面').fill('')
  for (const mode of ['light', 'dark'] as const) {
    await page.emulateMedia({colorScheme: mode})
    for (const name of ['版面', '样式']) {
      await tabs.getByRole('tab', {name, exact: true}).click()
      await expect(tabs.getByRole('tab', {name, exact: true})).toHaveAttribute('aria-selected', 'true')
      await expect.poll(async () => tabs.getByRole('tab').evaluateAll(items => getComputedStyle(items[0]).backgroundColor !== getComputedStyle(items[1]).backgroundColor)).toBe(true)
    }
    // The title must inherit theme text, not black on a dark surface.
    await expect.poll(async () => page.getByRole('heading', {name:'流程', exact:true}).evaluate((el, mode) => { const rgb = getComputedStyle(el).color.match(/\d+/g)!.map(Number); return mode === 'dark' ? rgb[0] > 180 : rgb[0] < 100 }, mode)).toBe(true)
    await page.screenshot({path: `../docs/refactor-v6/evidence/L22-library-${mode}.png`, fullPage: true})
  }
  await page.emulateMedia({colorScheme: 'light'}); await tabs.getByRole('tab', {name: '版面', exact: true}).click(); await list.getByRole('button', {name: /四角对齐版式/}).click()
  await page.getByRole('button', {name: '复制为我的资源'}).click(); await page.getByLabel('资源名称', {exact: true}).fill('我的版面'); await page.getByRole('button', {name: '确定', exact: true}).click(); await expect.poll(() => copied).toBe(true)
  await page.getByRole('button', {name: '替换文件'}).click(); await expect(page.getByRole('dialog').getByText('行军路线图')).toBeVisible(); expect(replaced).toBe(false)
  await page.getByLabel('替换为文件', {exact: true}).fill('/tmp/replacement.qpt'); await page.getByRole('button', {name: '确定', exact: true}).click(); await expect.poll(() => replaced).toBe(true)
  await page.getByRole('button', {name: '删除', exact: true}).click(); await expect(page.getByRole('dialog').getByText('行军路线图')).toBeVisible(); expect(removed).toBe(false)
  await page.getByRole('button', {name: '确认删除'}).click(); await expect.poll(() => removed).toBe(true)
})

test('flow document has one reading page, live editing and no data packaging form', async ({page}) => {
  await mockDesktopRuntime(page, {port: 50131})
  const guide = {meta: {id: 'demo', name: '河流流程', version: '5.0.0'}, source: 'user', enabled: true, guide_md: '# 河流流程\n\n加载真实河流。\n\n使用样式「河流线」。', resources: [], diagnostics: [], updated_at: '', guide: {body: '', resources: [], diagnostics: []}}
  const mutations: string[] = []
  await page.route('**/api/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    if (request.method() === 'OPTIONS') { await route.fulfill({status: 204}); return }
    if (request.method() !== 'GET') mutations.push(path)
    let data: unknown = {}
    if (path === '/api/runtime/snapshot' || path === '/api/runtime/services') data = {services: {}, instances: {items: [], active_id: null}}
    else if (path === '/api/guides') data = [{...guide.meta, description: '制作河流图', source: 'user', enabled: true}]
    else if (path === '/api/guides/demo') { if (request.method() === 'PUT') guide.guide_md = request.postDataJSON().guide_md; data = guide }
    else if (path === '/api/guides/import') data = {guide: {...guide, conversion_report: {converted: true, message: '已自动转换，请检查', items: ['原文件已备份']}}, report: {converted: true, message: '已自动转换，请检查', items: []}}
    else if (path === '/api/agent-configs') data = {configs: []}
    await route.fulfill({json: {success: true, data}})
  })
  await page.goto('/flows?flow=demo'); await expect(page).toHaveURL(/\/guides\?guide=demo$/)
  await expect(page.getByRole('article', {name: '流程正文'})).toContainText('加载真实河流')
  await expect(page.getByRole('tablist')).toHaveCount(0)
  await page.getByRole('button', {name: '编辑流程', exact: true}).click(); await page.getByLabel('FLOW.md 内容').fill('# 修改流程\n\n实时预览的新内容')
  await expect(page.getByRole('article', {name: '流程正文'})).toContainText('实时预览的新内容')
  await expect(page.getByRole('link', {name: '在对话中使用'})).toHaveAttribute('aria-disabled', 'true'); await page.getByRole('button', {name: '保存', exact: true}).click(); await expect(page.getByRole('status')).toContainText('已保存')
  await page.getByRole('button', {name: '导入流程', exact: true}).click(); await page.getByLabel('流程 ZIP、FLOW.md 或文件夹路径').fill('/tmp/legacy.zip'); await page.getByRole('button', {name: '确定', exact: true}).click()
  await expect(page.locator('summary').filter({hasText: '已自动转换，请检查'})).toBeVisible(); expect(mutations).toEqual(['/api/guides/demo', '/api/guides/import'])
  await expect(page.getByText(/角色对应|输入表单|重新编译/)).toHaveCount(0)
  await page.screenshot({path: '../docs/refactor-v6/evidence/L11-flow-document.png', fullPage: true})
})
