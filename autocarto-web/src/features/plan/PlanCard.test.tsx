import { beforeEach, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import PlanCard from './PlanCard'
import PlanProgress from './PlanProgress'
import { useFlowRunStore } from './store'
import type { PlanSnapshot, FlowRunState } from './types'
const mocks = vi.hoisted(() => ({start:vi.fn(),cancel:vi.fn(),editData:vi.fn(),failure:vi.fn(),list:vi.fn(),layers:vi.fn()}))
vi.mock('./api',()=>({planApi:mocks}))
vi.mock('@/features/guides/api',()=>({resourceApi:mocks,layerApi:mocks}))
vi.mock('@/features/guides/ResourceMedia',()=>({default:({resource}:{resource:{name:string}})=><span>{resource.name}缩略图</span>}))
vi.mock('@/lib/config-form-helpers',()=>({isDesktopRuntime:()=>false,pickDirectoryPath:vi.fn(),pickFilePath:vi.fn()}))
const snapshot = (): PlanSnapshot => ({session_id:'s',plan_id:'p',plan_version:2,status:'awaiting_confirmation',needs_confirmation:true,execution_mode:'build',plan:{api_version:'carto-plan/v2',plan_id:'p',version:2,goal:'湖南河流图',facts:[{name:'范围',value:'湖南',from:'用户消息'}],assumptions:[],data:[{id:'river',name:'河流',kind:'layer',source:'retrieve',need:{category:'river',fields:['name'],description:'主要河流'},extent:{from:'route',expand:0.2}}],steps:[{id:'s1',title:'制作地图',inputs:['river'],produces:[{id:'map',kind:'file',format:'pdf'}]}],deliverables:['map'],checks:['河流名称是否清晰']},precheck:[{data_id:'river',status:'confirm',reason:'按路线范围获取',methods:['osm','web_search']}]})
const run = (plan:PlanSnapshot): FlowRunState => ({api_version:'flow-run/v1',session_id:'s',flow_run_id:'r',flow_id:'',plan_id:'p',carto_plan:plan.plan,status:'interrupted',started_at:'2026-10-27T00:00:00Z',steps:[{id:'s1',index:1,title:'制作地图',role:'',status:'waiting_user',awaiting_choice:true,attempts:[],outputs:{},last_failure:'来源无数据'}],result:{deliverables:{},issues:[],summary:''}})
beforeEach(()=>{vi.clearAllMocks();mocks.list.mockResolvedValue([]);mocks.start.mockResolvedValue(snapshot());mocks.editData.mockResolvedValue({...snapshot(),plan_version:3,precheck:[{data_id:'river',status:'available',reason:'用户数据可读'}]});useFlowRunStore.setState({plans:{},runs:{}})})
it('renders v2 precheck and saves a source edit with the new precheck result',async()=>{
 const value=snapshot();render(<PlanCard snapshot={value} executionMode="build"/>)
 expect(screen.getByText(/范围：湖南/)).toBeInTheDocument();expect(screen.getByText('请确认')).toBeInTheDocument();expect(screen.getByText(/route 外扩20%/)).toBeInTheDocument()
 fireEvent.click(screen.getByText('修改来源'));fireEvent.change(screen.getByLabelText('数据来源方式'),{target:{value:'user'}});fireEvent.change(screen.getByLabelText('数据路径'),{target:{value:'/data/river.gpkg'}});fireEvent.click(screen.getByText('保存来源'))
 await waitFor(()=>expect(mocks.editData).toHaveBeenCalledWith(value,'river',{source:'user',ref:'/data/river.gpkg',method:''}))
 expect(useFlowRunStore.getState().plans.s[0].precheck?.[0].status).toBe('available')
})
it('disables start for unavailable data and offers no start in Plan mode',()=>{
 const value={...snapshot(),status:'ready' as const,needs_confirmation:false};const view=render(<PlanCard snapshot={value} executionMode="build"/>)
 expect(screen.getByRole('button',{name:'开始'})).toBeEnabled()
 view.rerender(<PlanCard snapshot={{...value,precheck:[{data_id:'river',status:'unavailable',reason:'文件不存在',suggestion:'选择其他文件'}]}} executionMode="build"/>)
 expect(screen.getByRole('button',{name:'开始'})).toBeDisabled();expect(screen.getByText('选择其他文件')).toBeVisible()
 view.rerender(<PlanCard snapshot={value} executionMode="plan"/>);expect(screen.queryByRole('button',{name:'开始'})).not.toBeInTheDocument();expect(screen.getByText('切换到 Build 后执行')).toBeVisible()
})
it.each(['user','project','builtin','retrieve'] as const)('renders each precheck state for %s source',(source)=>{
 for(const status of ['available','confirm','unavailable'] as const){
  const value=snapshot();value.plan.data=[{id:'d',name:'数据',source,ref:'示例'}];value.precheck=[{data_id:'d',status,reason:'来源依据'}]
  const view=render(<PlanCard snapshot={value} executionMode="build"/>);expect(screen.getByText({available:'可用',confirm:'请确认',unavailable:'不可用'}[status])).toBeVisible();expect(screen.getByText('依据：来源依据')).toBeVisible();view.unmount()
 }
})
it('keeps v1 readonly and regenerates using its original goal and guide',()=>{
 const value=snapshot();value.plan.api_version='carto-plan/v1';value.plan.guide={id:'river-guide',name:'河流图',version:'1'}
 const listener=vi.fn();window.addEventListener('autocarto:compose-message',listener)
 render(<PlanCard snapshot={value} executionMode="build"/>);expect(screen.getByText('旧版计划（只读）')).toBeVisible();expect(screen.queryByRole('button',{name:'开始'})).not.toBeInTheDocument();expect(screen.queryByText('修改来源')).not.toBeInTheDocument()
 fireEvent.click(screen.getByText('按新格式重新生成'));expect(listener.mock.calls[0][0].detail.text).toContain('湖南河流图');expect(listener.mock.calls[0][0].detail.text).toContain('河流图');window.removeEventListener('autocarto:compose-message',listener)
})
it('shows styles, produced data, notes and checks without inventing an acquisition step',async()=>{
 const value=snapshot();value.plan.data[0].style='河流线';value.notes=['已移除重复的数据准备步骤'];mocks.list.mockResolvedValue([{id:'style.river',name:'河流线',type:'style'}])
 render(<PlanCard snapshot={value} executionMode="build"/>);expect(await screen.findByText('河流线缩略图')).toBeVisible();fireEvent.click(screen.getByText(/步骤 ·/));expect(screen.getByText('产出：map')).toBeVisible();expect(screen.queryByText(/能力：/)).not.toBeInTheDocument()
 fireEvent.click(screen.getByText('成图后请核对'));expect(screen.getByText('河流名称是否清晰')).toBeVisible();fireEvent.click(screen.getByText('计划整理说明'));expect(screen.getByText(value.notes[0])).toBeVisible()
})
it('preserves session/version boundaries and completed plans against replay',()=>{
 const store=useFlowRunStore.getState();store.applyPlan(snapshot());store.applyPlan({...snapshot(),plan_version:1,status:'running'});store.applyPlan({...snapshot(),status:'completed'});store.applyPlan({...snapshot(),status:'running'});store.applyPlan({...snapshot(),session_id:'other',status:'running'})
 expect(useFlowRunStore.getState().plans.s[0].status).toBe('completed');expect(useFlowRunStore.getState().plans.other[0].status).toBe('running');store.applyPlan({session_id:'s',plan:{steps:[]}});expect(useFlowRunStore.getState().plans.s).toHaveLength(1)
})
it('resumes an active plan with its run identity and permits only explicit snapshot continuation',async()=>{
 const value=snapshot(),state=run(value);state.resource_change={fingerprint:'fp',message:'资源发生变化'}
 render(<PlanProgress snapshot={value} run={state} messages={[]} renderMessage={()=>null} executionMode="build"/>);fireEvent.click(screen.getByText('继续使用运行快照'))
 await waitFor(()=>expect(mocks.start).toHaveBeenCalledWith(value,{resume_run_id:'r',resume_resource_choice:'continue_snapshot',resource_fingerprint:'fp'}))
})
it('offers supplemental data for v2 inputs and never skips a required step',()=>{
 const value={...snapshot(),status:'waiting_user' as const};render(<PlanProgress snapshot={value} run={run(value)} messages={[]} renderMessage={()=>null} executionMode="build"/>);expect(screen.getByText('换来源或调整方案')).toBeVisible();expect(screen.queryByText('跳过可选步骤')).not.toBeInTheDocument();expect(screen.getByLabelText('补充数据路径')).toBeVisible()
})
it('uses a newer persisted completed run and rejects delayed running snapshots',()=>{
 const value=snapshot(),running=run(value);running.status='running';running.event_seq=3
 const store=useFlowRunStore.getState();store.restore('s',running,3);store.applyPlan({...value,status:'completed',updated_at:'2026-10-27T00:02:00Z',run:{...running,status:'completed',event_seq:4}});store.applyPlan({...value,status:'running',updated_at:'2026-10-27T00:01:00Z',run:running})
 expect(useFlowRunStore.getState().runs.s[0].status).toBe('completed');expect(useFlowRunStore.getState().plans.s[0].status).toBe('completed')
})
it('changes to another builtin layer without a frozen candidate query',async()=>{
 mocks.list.mockResolvedValue({items:[{id:'builtin-river',name:'河流线',pack:'中国底图'}]});const value=snapshot();render(<PlanCard snapshot={value} executionMode="build"/>);fireEvent.click(screen.getByText('修改来源'));fireEvent.change(screen.getByLabelText('数据来源方式'),{target:{value:'builtin'}})
 expect(await screen.findByRole('option',{name:'河流线（中国底图）'})).toBeVisible();fireEvent.change(screen.getByLabelText('内置图层'),{target:{value:'河流线'}});fireEvent.click(screen.getByText('保存来源'));await waitFor(()=>expect(mocks.editData).toHaveBeenCalledWith(value,'river',{source:'builtin',ref:'河流线',method:''}))
 expect(within(screen.getByRole('region',{name:'制图计划 湖南河流图'})).queryByText('查看候选来源与依据')).not.toBeInTheDocument()
})

it('offers resume, source editing and stop for interrupted execution',()=>{
 const value={...snapshot(),status:'interrupted' as const};const state=run(value);state.steps[0].status='interrupted';state.steps[0].last_failure='执行已中断'
 render(<PlanProgress snapshot={value} run={state} messages={[]} renderMessage={()=>null} executionMode="build"/>);expect(screen.getByText('继续此流程')).toBeVisible();expect(screen.getByText('执行已中断')).toBeVisible();expect(screen.queryByRole('link',{name:'调整执行预算'})).not.toBeInTheDocument();expect(screen.getByText('停止任务')).toBeEnabled();fireEvent.click(screen.getByText('修改数据来源'));expect(screen.getByLabelText('数据来源方式')).toBeVisible()
})
