import {expect,test,type Page} from '@playwright/test'
import {mockDesktopRuntime} from './helpers/desktopRuntime'
type Harness=Window&{__v7socket:{onmessage?:(event:{data:string})=>void}}
const emit=(page:Page,message:Record<string,unknown>)=>page.evaluate(message=>(window as unknown as Harness).__v7socket.onmessage?.({data:JSON.stringify(message)}),message)

test('v2 plan shows precheck, edits a source and displays preparation ledger in task details',async({page})=>{
 await mockDesktopRuntime(page,{port:50132})
 await page.addInitScript(()=>{const runtime=window as unknown as Harness;class Socket{static OPEN=1;static CONNECTING=0;readyState=1;onopen:((event:Event)=>void)|null=null;onmessage?:(event:{data:string})=>void;constructor(){runtime.__v7socket=this;setTimeout(()=>this.onopen?.(new Event('open')),0)}send(){}close(){this.readyState=3}addEventListener(){}removeEventListener(){}}Object.defineProperty(window,'WebSocket',{configurable:true,value:Socket})})
 const plan={api_version:'carto-plan/v2',plan_id:'cp-v7',version:1,goal:'湖南河流专题图',facts:[{name:'范围',value:'湖南',from:'用户消息'}],assumptions:[],data:[{id:'src',name:'史料',kind:'file',source:'user',ref:'附件:史料.txt'},{id:'city',name:'市域',kind:'layer',source:'project',ref:'已有市域'},{id:'province',name:'省域',kind:'layer',source:'builtin',ref:'省域面'},{id:'river',name:'河流',kind:'layer',source:'retrieve',need:{category:'river',fields:['name']},extent:{from:'city',expand:0.2}}],steps:[{id:'map',title:'制图并导出',inputs:['city','province','river'],produces:[{id:'map_file',kind:'file',format:'png'}]}],deliverables:['map_file'],checks:['河流名称清晰']}
 const snapshot={session_id:'v7-session',plan_id:plan.plan_id,plan_version:1,plan,status:'awaiting_confirmation',needs_confirmation:true,execution_mode:'build',precheck:plan.data.map(data=>({data_id:data.id,status:data.id==='river'?'confirm':'available',reason:data.id==='river'?'按范围查询主要河流':'本机可用',methods:data.id==='river'?['osm','web_search']:[]})),notes:['数据准备在使用前进行']}
 const session={session_id:'v7-session',name:'V7 界面验收',project_path:'/fixture/project.qgz',execution_mode:'build',created_at:'2026-10-29T00:00:00Z',updated_at:'2026-10-29T00:00:00Z',messages:[]}
 const instance={id:'v7-instance',pid:1,project_path:session.project_path,status:'running',qgis_version:'3.40',services:{},last_heartbeat:'2026-10-29T00:00:00Z'}
 const runtime={services:{agent:{status:'running'}},instances:{items:[instance],active_id:instance.id,instance_count:1,running_count:1},mode_readiness:{plan:{enabled:true,ready:true},build:{enabled:true,ready:true}},stale:false}
 const edits:unknown[]=[],errors:string[]=[];page.on('pageerror',error=>errors.push(error.message))
 await page.route('**/api/**',async route=>{
  const path=new URL(route.request().url()).pathname
  if(route.request().method()==='OPTIONS'){await route.fulfill({status:204});return}
  let data:unknown={}
  if(path==='/api/runtime/snapshot'||path==='/api/runtime/services')data=runtime
  else if(path==='/api/health')data={ok:true}
  else if(path==='/api/capabilities')data={qgis_available:true,instance_count:1}
  else if(path==='/api/instances')data={instances:[instance],active_id:instance.id}
  else if(path==='/api/sessions')data={sessions:[session]}
  else if(path.endsWith('/plans/cp-v7/edits')){edits.push(route.request().postDataJSON());data={...snapshot,plan_version:2,plan:{...plan,version:2,data:plan.data.map(item=>item.id==='river'?{...item,source:'user',ref:'/fixture/river.gpkg'}:item)},precheck:snapshot.precheck.map(item=>({...item,status:'available'}))}}
  else if(path.endsWith('/plans'))data={plans:[]}
  else if(path.startsWith('/api/sessions/'))data=session
  else if(path==='/api/llm/providers')data={active:'',providers:[]}
  else if(path==='/api/agent-configs')data={configs:[{config_name:'default',is_active:true}]}
  else if(path==='/api/guides')data=[]
  await route.fulfill({json:{success:true,data},headers:{'Access-Control-Allow-Origin':'*'}})
 })
 await page.goto('/chat');await page.getByRole('button',{name:/V7 界面验收/}).first().click();await expect.poll(()=>page.evaluate(()=>Boolean((window as unknown as Harness).__v7socket?.onmessage))).toBe(true)
 await emit(page,{type:'plan_event',session_id:session.session_id,data:snapshot})
 const card=page.getByRole('region',{name:`制图计划 ${plan.goal}`});await expect(card.getByText('请确认',{exact:true})).toBeVisible();await expect(card.getByText('来源：内置图层「省域面」')).toBeVisible();await expect(card.getByText(/市域 外扩20%/)).toBeVisible()
 await card.getByText('修改来源').last().click();await card.getByLabel('数据来源方式').selectOption('user');await card.getByLabel('数据路径').fill('/fixture/river.gpkg');await card.getByText('保存来源').click();await expect.poll(()=>edits).toEqual([{version:1,data_id:'river',source:'user',ref:'/fixture/river.gpkg',method:''}]);await expect(card.getByText('请确认',{exact:true})).toHaveCount(0);await card.getByText('成图后请核对').click();await expect(card.getByText('河流名称清晰')).toBeVisible();await page.screenshot({path:'../docs/refactor-v7/evidence/D32-plan-data.png',fullPage:true})
 // Progress is a separate run snapshot: the same persisted fields restore after reconnect.
 const run={api_version:'flow-run/v1',session_id:session.session_id,flow_run_id:'v7-run',flow_id:'',plan_id:plan.plan_id,carto_plan:plan,status:'running',started_at:'2026-10-29T00:00:00Z',steps:[{id:'map',index:1,title:'制图并导出',role:'',status:'running',attempts:[],outputs:{}}],result:{deliverables:{},issues:[],summary:''},data_preparation:[{data_id:'river',name:'河流',status:'running',budget:{elapsed_seconds:800,limit_seconds:900,tool_calls:70,max_tool_calls:80,near_limit:true},ledger:[{method:'OSM',started_at:'2026-10-29T00:00:00Z',result:'成功',reason:'取得主要河流，字段核验通过'}]}]}
 await emit(page,{type:'plan_event',session_id:session.session_id,data:{...snapshot,status:'running',run,plan_version:2}})
 await page.getByRole('button',{name:'查看详情',exact:true}).first().click();await expect(page.getByRole('region',{name:'数据准备与取数台账'})).toBeVisible();await page.getByText('取数台账 · 1 次尝试').click();await expect(page.getByText('取得主要河流，字段核验通过')).toBeVisible();await expect(page.getByText(/即将达到预算/)).toBeVisible();await page.screenshot({path:'../docs/refactor-v7/evidence/D32-preparation-ledger.png',fullPage:true});expect(errors).toEqual([])
})
