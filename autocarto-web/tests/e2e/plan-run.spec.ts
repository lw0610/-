import { expect, test, type Page } from '@playwright/test'
import { mockDesktopRuntime } from './helpers/desktopRuntime'
type Harness = Window & { __sent: Record<string, unknown>[]; __opened: string[]; __flowSocket: { onmessage?: (event: { data: string }) => void } }
const emit = (page: Page, message: Record<string, unknown>) => page.evaluate(message => (window as unknown as Harness).__flowSocket.onmessage?.({ data: JSON.stringify(message) }), message)

// Browser HTTP/WS contract playback only. No real Go server, model, QGIS,
// user runtime directory, native file opening or flow execution is used.
test('conversation plan becomes progress in place and survives session restoration', async ({ page }) => {
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
  const plan = {api_version:'carto-plan/v1',plan_id:'plan-1',version:1,goal:'数据检查与导出',facts:[{name:'数据范围',value:'湖南',from:'用户消息'}],assumptions:[],data:[],steps:[{id:'s1',title:'检查数据',kind:'agent',capabilities:['数据加载与导出'],outputs:[]}],deliverables:[{label:'图件',type:'file',format:'pdf'}],checks:['文件存在']}
  let planSnapshot = {session_id:'flow-session',plan_id:plan.plan_id,plan_version:1,plan,status:'awaiting_confirmation',needs_confirmation:true,execution_mode:'build'}
  const starts:unknown[]=[]
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
    else if(path.endsWith('/plans/plan-1/start')){starts.push(route.request().postDataJSON());planSnapshot={...planSnapshot,status:'running'};data=planSnapshot}
    else if(path.endsWith('/plans'))data={plans:session.carto_plans||[]}
    else if (path.startsWith('/api/sessions/')) data = session
    else if (path === '/api/llm/providers') data = { active: '', providers: [] }
    else if (path === '/api/agent-configs') data = { configs: [{ config_name: 'default', is_active: true }] }
    else if(path==='/api/guides') data=[{id:'demo',name:'数据指南',enabled:true}]
    else if (path === '/api/tools/catalog') data = { items: [] }
    else if (path === '/api/skill-packages') data = { packages: [] }
    await route.fulfill({ json: { success: true, data }, headers: { 'Access-Control-Allow-Origin': '*' } })
  })
  await page.goto('/chat')
  await page.getByRole('button',{name:/流程隔离对话/}).first().click()
  await expect(page.getByRole('combobox',{name:'选择流程'})).toHaveCount(0)
  await page.getByRole('textbox',{name:'消息输入'}).fill('制作湖南河流图')
  await page.getByRole('button',{name:'发送',exact:true}).click()
  await expect.poll(()=>page.evaluate(()=>(window as unknown as Harness).__sent.filter(message=>message.type==='chat').length)).toBe(1)
  const task=await page.evaluate(()=>(window as unknown as Harness).__sent.find(message=>message.type==='chat'))
  expect(task?.data).not.toHaveProperty('flow')
  await emit(page,{type:'plan_event',session_id:'flow-session',data:planSnapshot})
  const planCard=page.getByRole('region',{name:'制图计划 数据检查与导出'})
  await expect(planCard.getByText(/数据范围：湖南/)).toBeVisible()
  await page.screenshot({path:test.info().outputPath('K41-plan-card.png'),fullPage:true})
  await planCard.getByRole('button',{name:'开始',exact:true}).click()
  await expect.poll(()=>starts).toEqual([{version:1}])
  const flow=(event:string,payload:Record<string,unknown>)=>emit(page,{type:'flow_event',session_id:'flow-session',data:{event,session_id:'flow-session',flow_run_id:'run-1',payload:{plan_id:'plan-1',plan_version:1,...payload}}})
  await flow('flow.started',{name:plan.goal,steps:plan.steps,carto_plan:plan})
  await flow('flow.step_started',{step_id:'s1',run_id:'attempt-1',attempt:1})
  await expect(planCard).toHaveCount(0)
  const progress=page.getByRole('region',{name:`流程进度 ${plan.goal}`})
  await expect(progress).toBeVisible()
  const toolData = {flow_run_id:'run-1',step_id:'s1',run_id:'attempt-1',call_id:'layers-1',tool:'get_layers_info'}
  await emit(page,{type:'agent_workflow',session_id:'flow-session',run_id:'attempt-1',phase:'tool_call',content:'读取图层',data:{...toolData,tool_args:{layer:'河流'}}})
  await emit(page,{type:'agent_workflow',session_id:'flow-session',phase:'tool_result',content:'真实图层结果',data:{...toolData,result:'图层存在',status:'completed',success:true}})
  await expect(progress.getByText('图层存在',{exact:true})).toHaveCount(0)
  await progress.getByRole('button',{name:'查看详情',exact:true}).click()
  const taskRegion = page.getByTestId('workspace-task-region')
  const processRegion = page.getByTestId('workspace-process-region')
  await expect(taskRegion.getByText('检查数据',{exact:true})).toBeVisible()
  await expect(processRegion.getByText('工具 · get_layers_info',{exact:true})).toHaveCount(1)
  await processRegion.getByRole('button',{name:/工具 · get_layers_info/}).click()
  await expect(processRegion.getByText('图层存在',{exact:true})).toBeVisible()
  await flow('flow.acquisition_progress',{row:'河流',stage:'downloading',bytes:262144,total:1048576,bytes_per_second:65536,message:'正在接收湖南河流数据'})
  await expect(taskRegion.getByText('正在接收湖南河流数据')).toBeVisible()
  await expect(taskRegion.getByText('下载中 25%',{exact:true})).toBeVisible()
  await expect(taskRegion.getByText(/64.0 KB\/s/)).toBeVisible()
  await page.screenshot({path:test.info().outputPath('K42-progress.png'),fullPage:true})
  await flow('flow.step_failed',{step_id:'s1',errors:['当前来源暂不可用'],next:'replan'})
  await emit(page,{type:'plan_event',session_id:'flow-session',data:{...planSnapshot,status:'needs_replan',replanning:false}})
  await expect(page.getByRole('button',{name:'换来源或调整方案'})).toBeVisible()
  await expect(page.getByRole('button',{name:'跳过可选步骤'})).toHaveCount(0)
  await page.getByRole('region',{name:'执行失败处理'}).screenshot({path:test.info().outputPath('K42-failure-actions.png')})
  await flow('flow.step_started',{step_id:'s1',run_id:'attempt-2',attempt:2})
  await emit(page,{type:'plan_event',session_id:'flow-session',data:{...planSnapshot,status:'running',replanning:false}})
  await flow('flow.step_completed',{step_id:'s1',outputs:{图件:'outputs/map.pdf'}})
  await flow('flow.finished',{status:'completed',summary:'图件已导出',deliverables:{图件:'outputs/map.pdf'},issues:[]})
  await expect(progress.getByText('已完成 · 1/1',{exact:true})).toBeVisible()
  await progress.getByRole('button',{name:'outputs/map.pdf',exact:true}).last().click()
  await expect.poll(()=>page.evaluate(()=>(window as unknown as Harness).__opened)).toEqual(['outputs/map.pdf'])
  session.carto_plans=[{...planSnapshot,status:'completed',run:{api_version:'flow-run/v1',flow_run_id:'run-1',session_id:'flow-session',flow_id:'',carto_plan:plan,status:'completed',started_at:'2026-10-27T00:00:00Z',steps:[{id:'s1',index:1,title:'检查数据',role:'',status:'completed',attempts:[],outputs:{}}],result:{deliverables:{图件:'outputs/map.pdf'},issues:[],summary:'恢复完成记录'}}}]
  const restoredTime = Date.parse('2026-10-27T00:00:00Z')
  session.workflow_blocks = [{id:'persisted-agent',agent:'data_engineer',agentDisplayName:'数据工程智能体',summary:'检查数据',status:'completed',startedAt:restoredTime,updatedAt:restoredTime+1000,entries:[{id:'persisted-entry',type:'tool',phase:'tool_result',content:'真实图层结果',timestamp:restoredTime+1000,data:{...toolData,result:'图层存在',status:'completed',success:true},toolCard:{id:'persisted-tool',callId:'layers-1',tool:'get_layers_info',status:'completed',result:'图层存在',args:{layer:'河流'},startedAt:restoredTime,finishedAt:restoredTime+1000}}]}]
  session.messages = [{role:'system',msg_type:'tool',phase:'tool_result',content:'真实图层结果',timestamp:new Date(restoredTime+1000).toISOString(),data:{...toolData,result:'图层存在',status:'completed',success:true}}]
  await page.reload();await page.getByRole('button',{name:/流程隔离对话/}).first().click()
  await expect(page.getByText('恢复完成记录')).toBeVisible()
  await expect(page.getByRole('button',{name:'开始',exact:true})).toHaveCount(0)
  await page.getByRole('region',{name:`流程进度 ${plan.goal}`}).getByRole('button',{name:'查看详情',exact:true}).click()
  await page.getByTestId('workspace-process-region').getByRole('button',{name:'展开全部',exact:true}).click()
  await expect(page.getByTestId('workspace-process-region').getByText('工具 · get_layers_info',{exact:true})).toHaveCount(1)
  await page.getByTestId('workspace-process-region').getByRole('button',{name:/工具 · get_layers_info/}).click()
  await expect(page.getByTestId('workspace-process-region').getByText('图层存在',{exact:true})).toBeVisible()
  await page.screenshot({path:test.info().outputPath('restored-right-process.png'),fullPage:true})
  expect(errors).toEqual([])
})
