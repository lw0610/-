import {expect,test} from '@playwright/test'
import {mockDesktopRuntime} from './helpers/desktopRuntime'

// Browser contract playback: all HTTP responses below are fixtures; no Go
// service, QGIS instance, settings credentials or persistent runtime is used.
test('v2 scheme edits preserve role fields and expose only tools and flows',async({page})=>{
  await mockDesktopRuntime(page,{port:50131})
  await page.addInitScript(()=>{class Socket{static OPEN=1;readyState=1;onopen:((event:Event)=>void)|null=null;constructor(){setTimeout(()=>this.onopen?.(new Event('open')),0)}send(){}close(){}addEventListener(){}removeEventListener(){}};Object.defineProperty(window,'WebSocket',{value:Socket,configurable:true})})
  const packages=[
    {package_id:'project-state',name:'project-state',display_name:'工程与图层状态',kind:'category',domain:'系统',system:true,hidden:false,enabled:true,tool_count:1},
    {package_id:'runtime-control',name:'runtime-control',display_name:'运行控制',kind:'category',domain:'系统',system:true,hidden:false,enabled:true,tool_count:1},
    {package_id:'vector-geometry',name:'vector-geometry',display_name:'几何处理与叠加',kind:'category',domain:'矢量',system:false,hidden:false,enabled:true,tool_count:2,description:'缓冲与裁剪'},
    {package_id:'charts',name:'charts',display_name:'统计图表',kind:'category',domain:'制图',system:false,hidden:false,enabled:true,tool_count:1},
    {package_id:'internal-only',name:'internal-only',display_name:'隐藏执行器工具',kind:'category',domain:'系统',hidden:true,enabled:true,tool_count:1},
    {package_id:'old-method',name:'old-method',display_name:'不应出现的旧方法',kind:'workflow',enabled:true,tool_count:0},
  ]
  const refs:Record<string,string[]>={'project-state':['get_project_info'],'runtime-control':['reference_read'],'vector-geometry':['buffer','clip'],charts:['bar_chart'],'internal-only':['compile']}
  const catalog={id:'tool_catalog/global',version:'1',availability:'available',degradation_status:'none',items:[{id:'buffer',display_name:'生成缓冲区',description:'创建缓冲范围',keywords:['邻近范围'],package_id:'vector-geometry',availability:'available',runtime_target:'qgis'},{id:'clip',display_name:'按掩膜裁剪',package_id:'vector-geometry',availability:'available',runtime_target:'qgis'},{id:'bar_chart',display_name:'绘制分类柱状图',description:'比较类别数值',keywords:['分类比较'],package_id:'charts',availability:'available',runtime_target:'qgis'}]}
  const agent=(display_name:string)=>({display_name,description:'通用职责',kind:'agent',enabled:true,tool_packages:['vector-geometry'],prompts:{static_instructions:'核验真实输入',user_prompt_template:'{{ user_input }}',custom_rule:'keep'},llm:{provider:''}})
  let profile={version:'2.0',meta:{default_execution_mode:'build',description:'界面契约回放',plan_enabled:true},flows:{enabled:['demo']},migration_warnings:[{code:'LEGACY_UNRESOLVED_PACKAGE',path:'runtime.agents.custom-executor',package:'unknown-keep',message:'旧编号已保留'}],runtime_shared:{tool_packages:['charts']},runtime_agents:[{name:'orchestrator',agent:{...agent('编排智能体'),is_main:true}},{name:'custom-executor',agent:{...agent('任意编号执行器'),kind:'executor',executor:'route_knowledge',flow_only:true,tool_packages:[],legacy_unresolved_packages:['unknown-keep']}},{name:'data',agent:agent('数据角色')},{name:'spatial',agent:agent('分析角色')},{name:'cartographer',agent:agent('制图角色')},{name:'quality',agent:agent('审查角色')},{name:'scripting',agent:agent('脚本角色')}],has_plan:true,plan:{enabled:true,max_iterations:17,shared:{tool_packages:[]},agents:[{name:'planner',agent:{display_name:'规划角色',kind:'agent',mode:'readonly',tool_packages:[],prompts:{static_instructions:'plan',custom_rule:'keep-plan'}}}]}}
  let saved:typeof profile|undefined
  await page.route('**/api/**',async(route)=>{
    const request=route.request(),path=decodeURIComponent(new URL(request.url()).pathname)
    if(request.method()==='OPTIONS'){await route.fulfill({status:204});return}
    let data:unknown={}
    if(path==='/api/settings/workspace')data={tasks:[{key:'models-agents',label:'模型与智能体',href:'/settings/models-agents',hint:'方案',routes:['/settings/models-agents']},{key:'tools',label:'工具包与能力',href:'/settings/tools',hint:'工具包',routes:['/settings/tools']}],descriptors:[]}
    else if(path==='/api/agent-configs')data={configs:[{config_name:'default',is_active:true,is_default:true}]}
    else if(path==='/api/agent-configs/default/data')data=profile
    else if(path==='/api/agent-configs/default'&&request.method()==='PUT'){saved=request.postDataJSON();profile=saved!;data={runtime_applied:false,config_saved:true}}
    else if(path==='/api/skill-packages')data={packages}
    else if(path.startsWith('/api/skill-packages/')){const id=path.split('/').pop()!;data={...packages.find(item=>item.package_id===id),tool_refs:refs[id]||[],markdown_body:'# 使用说明\n参考资料：references/方法.md',references:[],config:{}}}
    else if(path==='/api/tools/catalog')data=catalog
    else if(path==='/api/flows')data=[{id:'demo',name:'通用数据流程',description:'检查真实数据',version:'1.0',source:'builtin',enabled:true,diagnostics_summary:{errors:0,warnings:1,infos:0}}]
    else if(path==='/api/flows/demo')data={meta:{id:'demo',name:'通用数据流程',version:'1.0'},flow_md:'# 通用数据流程\n\n## 步骤\n\n### 1. 检查数据\n- 负责：数据角色',source:'builtin',resources:[],diagnostics:[],stale:false,plan:{api_version:'flow-plan/v1',flow_id:'demo',name:'通用数据流程',scheme:'default',inputs:[],resources:[],steps:[]}}
    else if(path.includes('providers'))data={providers:[]}
    else if(path.includes('sessions'))data={sessions:[]}
    await route.fulfill({json:{success:true,data},headers:{'Access-Control-Allow-Origin':'*'}})
  })
  await page.goto('/settings/models-agents')
  await page.getByRole('button',{name:'智能体方案',exact:true}).click()
  await page.getByRole('button',{name:'编辑',exact:true}).first().click()
  await expect(page.getByText('启用的流程',{exact:true})).toBeVisible()
  await expect(page.getByText('通用数据流程',{exact:true})).toBeVisible()
  await expect(page.getByText('有警告',{exact:true})).toHaveCount(0)
  await expect(page.getByRole('group',{name:'角色 数据角色'})).toBeVisible()
  await page.getByRole('group',{name:'角色 数据角色'}).getByRole('button',{name:'编辑',exact:true}).click()
  const drawer=page.getByRole('dialog',{name:'编辑角色 数据角色'})
  await expect(drawer.getByRole('checkbox',{name:/工程与图层状态/})).toBeDisabled()
  await expect(drawer.getByRole('checkbox',{name:/运行控制/})).toBeChecked()
  await expect(drawer.getByText('隐藏执行器工具',{exact:true})).toHaveCount(0)
  await expect(drawer.getByText('不应出现的旧方法',{exact:true})).toHaveCount(0)
  await drawer.getByRole('button',{name:'工具 2 · 展开'}).click()
  await expect(drawer.getByText('生成缓冲区',{exact:true})).toBeVisible()
  await drawer.getByLabel('显示名称',{exact:true}).fill('数据角色已编辑')
  await drawer.getByRole('button',{name:'保存',exact:true}).click()
  await page.getByRole('group',{name:'角色 任意编号执行器'}).getByRole('button',{name:'编辑',exact:true}).click()
  const executor=page.getByRole('dialog',{name:'编辑角色 任意编号执行器'})
  await expect(executor.getByRole('combobox',{name:'执行器类型',exact:true})).toHaveValue('route_knowledge')
  await expect(executor.getByRole('checkbox',{name:/几何处理/})).toHaveCount(0)
  await executor.getByRole('button',{name:'关闭',exact:true}).click()
  await page.screenshot({path:'/tmp/enio2-t14-profile.png',fullPage:true})
  await page.getByRole('button',{name:'保存当前方案',exact:true}).click()
  await expect.poll(()=>saved?.runtime_agents.find(entry=>entry.name==='data')?.agent.display_name).toBe('数据角色已编辑')
  expect(saved?.version).toBe('2.0')
  expect(saved?.flows.enabled).toEqual(['demo'])
  expect(saved?.runtime_agents.find(entry=>entry.name==='custom-executor')?.agent).toMatchObject({kind:'executor',executor:'route_knowledge',flow_only:true,legacy_unresolved_packages:['unknown-keep'],prompts:{custom_rule:'keep'}})
  expect(saved?.plan.agents[0].agent.mode).toBe('readonly')
  expect(saved?.runtime_agents.every(entry=>!('skill_packages' in entry.agent))).toBe(true)
  await page.getByRole('link',{name:'通用数据流程',exact:true}).click()
  await expect(page).toHaveURL(/\/flows\?flow=demo&scheme=default&tab=steps$/)
  await expect(page.getByRole('textbox',{name:'流程内容',exact:true})).toHaveValue(/检查数据/)
  await expect(page.getByRole('combobox',{name:'当前方案',exact:true})).toHaveValue('default')
  await page.goto('/settings/tools')
  await page.getByRole('button',{name:'能力手册',exact:true}).click()
  await page.getByPlaceholder('搜索工具...').fill('分类比较')
  await expect(page.getByRole('heading',{name:'统计图表',exact:true})).toBeVisible()
  await expect(page.getByText('绘制分类柱状图',{exact:true})).toBeVisible()
  await expect(page.getByText('生成缓冲区',{exact:true})).toHaveCount(0)
  await page.screenshot({path:'/tmp/enio2-t14-handbook.png',fullPage:true})
})
