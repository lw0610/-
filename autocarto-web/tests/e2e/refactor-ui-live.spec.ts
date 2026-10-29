import { expect, test, type APIRequestContext } from '@playwright/test'
import { mkdtemp, mkdir, writeFile, stat, rm } from 'node:fs/promises'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { DEFAULT_DESKTOP_RUNTIME_PORT, mockDesktopRuntime } from './helpers/desktopRuntime'

// Explicit opt-in requires a test-owned production server. No models, QGIS tasks,
// or user runtime are used; source and exports belong to this temporary folder.
const base = `http://127.0.0.1:${DEFAULT_DESKTOP_RUNTIME_PORT}`
async function api<T>(request: APIRequestContext, method: string, path: string, data?: unknown) {
 const response=await request.fetch(`${base}/api${path}`,{method,data,headers:{'X-AutoCarto-Contract-Version':'v1'}})
 expect(response.ok(),`${method} ${path}: ${await response.text()}`).toBeTruthy();return (await response.json()).data as T
}
test('real guide HTTP and disk: legacy import report, deterministic edit and portable export',async({page,request})=>{
 test.skip(process.env.AUTOCARTO_REFACTOR_UI_LIVE!=='1','requires isolated production Go backend')
 const work=await mkdtemp(join(tmpdir(),'autocarto-v6-ui-')),id=`ui-guide-${Date.now()}`,source=join(work,'received')
 await mkdir(source);await writeFile(join(source,'GUIDE.md'),`---\n编号: ${id}\n名称: 界面指南验收\n---\n## 适用场景\n隔离验收\n## 需要准备\n- 标题（默认 核验地图）\n## 步骤\n### 数据检查\n- 负责：用户\n- 做法：检查真实材料。\n- 完成标准：「核验结果」（文本）\n`)
 try {
  await mockDesktopRuntime(page);await page.goto('/guides');await page.getByRole('button',{name:'导入流程',exact:true}).click()
  await page.getByLabel('流程 ZIP、FLOW.md 或文件夹路径').fill(source);await page.getByRole('button',{name:'确定',exact:true}).click()
  await expect(page.getByRole('heading',{name:'界面指南验收',exact:true,level:2})).toBeVisible()
  const imported=await api<{guide_md:string;conversion_report:{converted:boolean};guide:{body:string}}>(request,'GET',`/guides/${id}`)
  expect(imported.conversion_report.converted).toBe(true);expect(imported.guide.body).toContain('检查真实材料')
  await page.getByRole('button',{name:'编辑流程'}).click();const markdown=imported.guide_md+'\n\n## 注意事项\n- 保留真实来源。\n'
  await page.getByLabel('FLOW.md 内容').fill(markdown);await page.getByRole('button',{name:'保存',exact:true}).click()
  await expect(page.getByRole('status')).toContainText('已保存')
  expect((await api<{guide_md:string}>(request,'GET',`/guides/${id}`)).guide_md).toBe(markdown)
  await page.getByRole('button',{name:'导出流程',exact:true}).click();const target=join(work,'guide.zip')
  await page.getByLabel('导出 ZIP 文件完整路径').fill(target);await page.getByRole('button',{name:'确定',exact:true}).click()
  await expect(page.getByRole('status')).toContainText(target);expect((await stat(target)).size).toBeGreaterThan(0)
 } finally {await api(request,'DELETE',`/guides/${id}`);await rm(work,{recursive:true,force:true})}
})

test('real built-in flow and 13 visual resources render with bundled previews', async ({page, request}) => {
 test.skip(process.env.AUTOCARTO_REFACTOR_UI_LIVE !== '1', 'requires isolated production Go backend')
 const resources = await api<{id:string;type:string;preview_status:string}[]>(request,'GET','/resources?scope=all')
 expect(resources.filter(item => item.type === 'style')).toHaveLength(11)
 expect(resources.filter(item => item.type === 'layout')).toHaveLength(2)
 expect(resources.every(item => item.preview_status === 'ready')).toBe(true)
 const flow = await api<{diagnostics:{level:string;code:string}[]}>(request,'GET','/guides/marching-route')
 expect(flow.diagnostics.filter(item=>item.level==='error')).toHaveLength(0)
 expect(flow.diagnostics.every(item=>item.level==='info'||item.code.includes('LAYER'))).toBe(true)
 await mockDesktopRuntime(page)
 await page.goto('/guides?guide=marching-route')
 await expect(page.getByRole('article',{name:'流程正文'})).toContainText('行军路线图')
 await expect(page.getByRole('complementary',{name:'引用的样式与版面'})).toHaveCount(0)
 await page.screenshot({path:'../docs/refactor-v6/evidence/L11-built-in-flow.png',fullPage:true})
 await page.getByRole('button',{name:'样式与版面',exact:true}).click()
 const list=page.getByRole('list',{name:'样式与版面列表'})
 await expect(list.getByRole('listitem')).toHaveCount(11)
 await expect.poll(() => list.getByRole('img').count()).toBe(11)
 await page.screenshot({path:'../docs/refactor-v6/evidence/L22-built-in-styles.png',fullPage:true})
 await list.getByRole('button', {name: /铁路/}).click()
 await page.getByRole('button', {name: '放大铁路预览', exact: true}).click()
 const preview = page.getByRole('dialog')
 await expect(preview.getByRole('img', {name: '铁路符号图例预览'})).toBeVisible()
 await preview.getByRole('button', {name: '原始大小', exact: true}).click()
 await expect(preview.getByText('100%', {exact: true})).toBeVisible()
 await preview.getByRole('button', {name: '适合窗口', exact: true}).click()
 await page.screenshot({path: '../docs/refactor-v6/evidence/L22-style-symbol-preview.png', fullPage: true})
 await preview.getByRole('tab', {name: '用图层预览', exact: true}).click()
 await expect(preview.getByText('请先连接并选择一个 QGIS 工程，再用图层预览样式。')).toBeVisible()
 await preview.getByRole('button', {name: '关闭预览', exact: true}).click()
 await page.getByRole('tab',{name:'版面',exact:true}).click()
 await expect(list.getByRole('listitem')).toHaveCount(2)
 await expect.poll(() => list.getByRole('img').count()).toBe(2)
 await page.screenshot({path:'../docs/refactor-v6/evidence/L22-built-in-layouts.png',fullPage:true})
})
