import { beforeEach, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import PlanCard from './PlanCard'
import PlanProgress from './PlanProgress'
import { useFlowRunStore } from './store'
import type { PlanSnapshot, FlowRunState } from './types'
const mocks = vi.hoisted(() => ({ start:vi.fn(),cancel:vi.fn(),editData:vi.fn(),failure:vi.fn(),find:vi.fn(),capabilities:vi.fn(),list:vi.fn() }))
vi.mock('./api',()=>({planApi:mocks}))
vi.mock('@/features/guides/api',()=>({guideApi:mocks,resourceApi:mocks}))
vi.mock('@/features/guides/ResourceMedia',()=>({default:({resource}:{resource:{name:string}})=><span>{resource.name}缩略图</span>}))
vi.mock('@/lib/config-form-helpers',()=>({isDesktopRuntime:()=>false,pickDirectoryPath:vi.fn(),pickFilePath:vi.fn()}))
const snapshot = (): PlanSnapshot => ({session_id:'s',plan_id:'p',plan_version:2,status:'awaiting_confirmation',needs_confirmation:true,execution_mode:'build',plan:{api_version:'carto-plan/v1',plan_id:'p',version:2,goal:'湖南河流图',facts:[{name:'范围',value:'湖南',from:'用户消息'}],assumptions:[{text:'采用现代河流数据',impact:'high'}],data:[{id:'river',name:'河流',from:'acquire',find_id:'find-1',choice:'source-a'}],steps:[{id:'s1',title:'获取数据',kind:'acquire',data:['river'],optional:false}],deliverables:[{label:'地图',type:'file',format:'pdf'}],checks:['文件可读']},find_results:{'find-1':{rows:[{row:'river',category:'river',recommended:'source-a',candidates:[{id:'source-a',kind:'source',name:'来源甲',available:true,reasons:['覆盖湖南']},{id:'source-b',kind:'source',name:'来源乙',available:true,reasons:['字段匹配']}],excluded:[{source_id:'old',reason:'范围不覆盖'}]}]}}})
const run = (plan:PlanSnapshot): FlowRunState => ({api_version:'flow-run/v1',session_id:'s',flow_run_id:'r',flow_id:'',plan_id:'p',carto_plan:plan.plan,status:'interrupted',started_at:'2026-10-27T00:00:00Z',steps:[{id:'s1',index:1,title:'获取数据',role:'',status:'waiting_user',awaiting_choice:true,attempts:[],outputs:{},last_failure:'来源无数据'}],result:{deliverables:{},issues:[],summary:''}})
beforeEach(()=>{vi.clearAllMocks();mocks.capabilities.mockResolvedValue([]);mocks.start.mockResolvedValue(snapshot());mocks.editData.mockResolvedValue({...snapshot(),plan_version:3});useFlowRunStore.setState({plans:{},runs:{}})})
it('shows facts, evidence and versioned deterministic source edits',async()=>{
 const value=snapshot();render(<PlanCard snapshot={value} executionMode="build"/> )
 expect(screen.getByText(/范围：湖南/)).toBeInTheDocument()
 fireEvent.click(screen.getByText('查看候选来源与依据'))
 expect(screen.getAllByText(/覆盖湖南/).length).toBeGreaterThan(0)
 fireEvent.click(screen.getByText('修改来源'));fireEvent.change(screen.getByLabelText('river'),{target:{value:'source-b'}})
 fireEvent.click(screen.getByText('保存来源'))
 await waitFor(()=>expect(mocks.editData).toHaveBeenCalledWith(value,'river',{from:'acquire',choice:{river:'source-b'}}))
 expect(useFlowRunStore.getState().plans.s[0].plan_version).toBe(3)
})
it('offers start in ready and confirmation states, never in Plan mode',()=>{
 const value={...snapshot(),status:'ready' as const,needs_confirmation:false};const view=render(<PlanCard snapshot={value} executionMode="build"/> )
 expect(screen.getByRole('button',{name:'开始'})).toBeEnabled()
 view.rerender(<PlanCard snapshot={value} executionMode="plan"/>)
 expect(screen.queryByRole('button',{name:'开始'})).not.toBeInTheDocument();expect(screen.getByText('切换到 Build 后执行')).toBeInTheDocument()
})
it('preserves session/version boundaries and completed plans against replay',()=>{
 const store=useFlowRunStore.getState();store.applyPlan(snapshot());store.applyPlan({...snapshot(),plan_version:1,status:'running'});store.applyPlan({...snapshot(),status:'completed'});store.applyPlan({...snapshot(),status:'running'});store.applyPlan({...snapshot(),session_id:'other',status:'running'})
 expect(useFlowRunStore.getState().plans.s[0].status).toBe('completed');expect(useFlowRunStore.getState().plans.other[0].status).toBe('running')
 store.applyPlan({session_id:'s',plan:{steps:[]}});expect(useFlowRunStore.getState().plans.s).toHaveLength(1)
})
it('resumes a new plan with its run identity and permits only explicit snapshot continuation',async()=>{
 const value=snapshot(),state=run(value);state.resource_change={fingerprint:'fp',message:'资源发生变化'}
 render(<PlanProgress snapshot={value} run={state} messages={[]} renderMessage={()=>null} executionMode="build"/> )
 fireEvent.click(screen.getByText('继续使用运行快照'))
 await waitFor(()=>expect(mocks.start).toHaveBeenCalledWith(value,{resume_run_id:'r',resume_resource_choice:'continue_snapshot',resource_fingerprint:'fp'}))
 expect(screen.queryByText('使用当前资源重新开始')).not.toBeInTheDocument()
})
it('never offers skip for a required failed step',()=>{
 const value={...snapshot(),status:'waiting_user' as const};render(<PlanProgress snapshot={value} run={run(value)} messages={[]} renderMessage={()=>null} executionMode="build"/>)
 expect(screen.getByText('换来源或调整方案')).toBeInTheDocument();expect(screen.queryByText('跳过可选步骤')).not.toBeInTheDocument()
})

it('uses a newer persisted completed run and rejects delayed running snapshots',()=>{
 const value=snapshot(),running=run(value);running.status='running';running.event_seq=3
 const store=useFlowRunStore.getState();store.restore('s',running,3)
 store.applyPlan({...value,status:'completed',updated_at:'2026-10-27T00:02:00Z',run:{...running,status:'completed',event_seq:4}})
 store.applyPlan({...value,status:'running',updated_at:'2026-10-27T00:01:00Z',run:running})
 expect(useFlowRunStore.getState().runs.s[0].status).toBe('completed')
 expect(useFlowRunStore.getState().plans.s[0].status).toBe('completed')
})

it('shows failure actions while replanning and permits stopping but prevents concurrent revisions',async()=>{
 const value={...snapshot(),status:'needs_replan' as const,replanning:true};render(<PlanProgress snapshot={value} run={run(value)} messages={[]} renderMessage={()=>null} executionMode="build"/>)
 expect(screen.getByText('换来源或调整方案')).toBeDisabled();expect(screen.getByText('停止任务')).toBeEnabled()
 await waitFor(()=>expect(mocks.capabilities).toHaveBeenCalled())
})

it('shows every source, style preview and uncertain folder match directly in the plan', async () => {
 const value=snapshot()
 value.plan.data=[{id:'river',name:'河流',from:'acquire',find_id:'find-1',style:'河流线'},{id:'city',name:'市界',from:'user',path:'/data/city.shp'},{id:'layer',name:'省界',from:'project',layer:'uuid-123',layer_name:'已有省界'},{id:'route',name:'路线数据集',from:'step',step:'s1'}]
 value.find_results!['find-1'].rows[0].candidates![0].requires_confirmation=true
 mocks.list.mockResolvedValue([{id:'style.river',name:'河流线',type:'style'}])
 render(<PlanCard snapshot={value} executionMode="build"/>)
 expect(screen.getByText(/河流 · 自动获取/)).toBeInTheDocument();expect(screen.getByText(/市界 · 您提供/)).toBeInTheDocument();expect(screen.getByText(/已有省界 · 工程图层/)).toBeInTheDocument();expect(screen.getByText(/路线数据集 · 由第 1 步产生/)).toBeInTheDocument()
 expect(screen.getByText('请确认')).toBeInTheDocument();expect(await screen.findByText('河流线缩略图')).toBeInTheDocument()
})

it('shows and edits actual exploration sources without presenting them as existing files', async () => {
 const value = snapshot()
 const result = value.find_results!['find-1']
 result.rows[0].candidates = null
 result.rows[0].recommended = undefined
 result.fallback = [
  {id:'explore-osm',kind:'custom_osm',name:'自定义 OSM 查询',available:true,reasons:['按标签查询后登记'],notes:['保留来源与许可']},
  {id:'explore-web',kind:'web_search',name:'公开来源搜索',available:true,reasons:['查找真实数据后登记']},
 ]
 value.plan.data[0].choice = 'auto'
 render(<PlanCard snapshot={value} executionMode="build"/>)
 expect(screen.queryByText('尚无可用来源')).not.toBeInTheDocument()
 expect(screen.getAllByText(/自定义 OSM 查询 · 探索获取/)).toHaveLength(2)
 fireEvent.click(screen.getByText('查看候选来源与依据'))
 expect(screen.getByText('保留来源与许可')).toBeInTheDocument()
 fireEvent.click(screen.getByText('修改来源'))
 expect(screen.getByLabelText('river')).toHaveValue('explore-osm')
 fireEvent.change(screen.getByLabelText('river'), {target:{value:'explore-web'}})
 fireEvent.click(screen.getByText('保存来源'))
 await waitFor(()=>expect(mocks.editData).toHaveBeenCalledWith(value,'river',{from:'acquire',choice:{river:'explore-web'}}))
})
