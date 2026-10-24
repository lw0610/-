import React from 'react'
import {beforeEach,describe,it,expect,vi} from 'vitest'
import {render,screen,fireEvent,waitFor} from '@testing-library/react'
import ToolPackageSelector from '@/components/settings/ToolPackageSelector'
import AgentEditDrawer from '@/components/settings/AgentEditDrawer'
import AgentFlowSelection from '@/components/settings/AgentFlowSelection'
import ToolCatalogView from '@/components/tool-center/ToolCatalogView'
import ToolPackageBindings from '@/components/settings/ToolPackageBindings'
import {useSettingsStore} from '@/stores/settingsStore'
import type {SkillPackageSummary} from '@/lib/api'
const mocks=vi.hoisted(()=>({detail:vi.fn(),catalog:vi.fn(),flows:vi.fn()}))
vi.mock('@/features/flows/api',()=>({flowApi:{list:mocks.flows}}))
vi.mock('@/lib/api',async(importOriginal)=>({...await importOriginal<typeof import('@/lib/api')>(),default:{getSkillPackage:mocks.detail,getToolCatalog:mocks.catalog}}))
const pkg=(name:string,extra:Partial<SkillPackageSummary>={}):SkillPackageSummary=>({package_id:name,name,display_name:'几何处理',kind:'category',domain:'矢量',description:'通用方法',enabled:true,tags:[],requires_tools:[],tool_count:2,has_tools:true,has_config:false,...extra})
describe('v2 role editor',()=>{
  beforeEach(()=>{mocks.detail.mockReset().mockResolvedValue({tool_refs:['buffer','clip'],tools:[]});mocks.catalog.mockReset().mockResolvedValue({tool_infos:[{name:'buffer',display_name:'生成缓冲区'},{name:'clip',display_name:'按掩膜裁剪'}]});mocks.flows.mockReset().mockResolvedValue([{id:'flow-a',name:'流程甲',version:'1.0',source:'builtin',description:'示例',diagnostics_summary:{errors:0,warnings:1}}])})
  it('groups visible packages, locks foundations and never lists hidden or workflow methods',async()=>{
    const change=vi.fn()
    render(<ToolPackageSelector packages={[pkg('geometry'),pkg('foundation',{display_name:'工程状态',system:true,domain:'系统'}),pkg('hidden',{display_name:'隐藏执行器',hidden:true}),pkg('method',{display_name:'旧方法',kind:'workflow'})]} value={['hidden']} onChange={change}/>)
    expect(screen.getByRole('checkbox',{name:/工程状态/})).toBeChecked()
    expect(screen.getByRole('checkbox',{name:/工程状态/})).toBeDisabled()
    expect(screen.queryByText('隐藏执行器')).not.toBeInTheDocument()
    expect(screen.queryByText('旧方法')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('checkbox',{name:/几何处理/}))
    expect(change).toHaveBeenCalledWith(['hidden','geometry'])
    fireEvent.click(screen.getAllByRole('button',{name:/展开/})[1])
    expect(await screen.findByText('生成缓冲区')).toBeInTheDocument()
    expect(screen.getByText('按掩膜裁剪')).toBeInTheDocument()
  })
  it('uses role kind, not ID, and preserves executor and prompt fields on save',async()=>{
    const save=vi.fn()
    const agent={kind:'executor' as const,executor:'route_knowledge',flow_only:true,display_name:'抽取',description:'职责',enabled:true,tool_packages:[],legacy_unresolved_packages:['keep'],prompts:{static_instructions:'备用规则',user_prompt_template:'original',custom:'untouched'},custom_field:'keep'}
    render(<AgentEditDrawer open mode="runtime" agentName="custom-executor-id" agent={agent} toolPackages={[pkg('geometry')]} modelOptions={[]} onClose={()=>{}} onSave={save}/>)
    expect(screen.getByLabelText('执行器类型')).toHaveValue('route_knowledge')
    expect(screen.queryByRole('checkbox',{name:/几何处理/})).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('显示名称'),{target:{value:'新名'}})
    fireEvent.click(screen.getByRole('button',{name:'保存'}))
    await waitFor(()=>expect(save).toHaveBeenCalledOnce())
    expect(save.mock.calls[0][0]).toMatchObject({...agent,display_name:'新名'})
  })
  it('does not infer executor behavior from the former built-in role ID',()=>{
    render(<AgentEditDrawer open mode="runtime" agentName="knowledge_engineer" agent={{kind:'agent',display_name:'普通角色',description:'',enabled:true,tool_packages:[],prompts:{}}} toolPackages={[pkg('geometry')]} modelOptions={[]} onClose={()=>{}} onSave={()=>{}}/>)
    expect(screen.getByRole('checkbox',{name:/几何处理/})).toBeInTheDocument()
    expect(screen.queryByLabelText('执行器类型')).not.toBeInTheDocument()
  })
  it('keeps explicit missing flow IDs while toggling known flows',async()=>{
    const change=vi.fn()
    render(<AgentFlowSelection scheme="custom" value={{enabled:['missing'],custom:'keep'}} onChange={change}/>)
    expect(await screen.findByText('流程甲')).toBeInTheDocument()
    expect(screen.queryByText('有警告')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('checkbox'))
    expect(change).toHaveBeenCalledWith({enabled:['missing','flow-a'],custom:'keep'})
    expect(mocks.flows).toHaveBeenCalledWith('custom')
  })
  it('handbook groups by real package and searches Chinese keywords',()=>{
    render(<ToolCatalogView loading={false} packages={[pkg('charts',{display_name:'统计图表'})]} items={[{name:'bar_chart',display_name:'绘制分类柱状图',description:'比较数值',keywords:['分类比较'],package_id:'charts',available:true},{name:'buffer',display_name:'生成缓冲区',package_id:'geometry'}]}/>)
    fireEvent.change(screen.getByPlaceholderText('搜索工具...'),{target:{value:'分类比较'}})
    expect(screen.getByRole('heading',{name:'统计图表'})).toBeInTheDocument()
    expect(screen.getByText('绘制分类柱状图')).toBeInTheDocument()
    expect(screen.queryByText('生成缓冲区')).not.toBeInTheDocument()
    expect(screen.getByText(/写流程时可以这样描述：绘制分类柱状图/)).toBeInTheDocument()
  })
  it('changing to an executor clears manual tool grants while keeping other fields',async()=>{
    const save=vi.fn()
    render(<AgentEditDrawer open mode="runtime" agentName="custom" agent={{kind:'agent',display_name:'可切换角色',description:'',enabled:true,tool_packages:['geometry'],prompts:{custom:'keep'},llm:{provider:'special'},custom_field:'keep'}} toolPackages={[pkg('geometry')]} modelOptions={[]} onClose={()=>{}} onSave={save}/>)
    fireEvent.change(screen.getByLabelText('角色类型'),{target:{value:'executor'}})
    expect(screen.getByLabelText('执行器类型')).toHaveValue('route_knowledge')
    expect(screen.queryByRole('checkbox',{name:/几何处理/})).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button',{name:'保存'}))
    await waitFor(()=>expect(save).toHaveBeenCalledOnce())
    expect(save.mock.calls[0][0]).toMatchObject({kind:'executor',executor:'route_knowledge',flow_only:true,tool_packages:[],llm:{provider:'special'},prompts:{custom:'keep'},custom_field:'keep'})
  })
  it('counts distinct actual tool references across shared and role bindings',async()=>{
    useSettingsStore.setState({skillPackages:[pkg('count-first'),pkg('count-second')]})
    mocks.detail.mockImplementation(async(id:string)=>({tool_refs:id==='count-first'?['shared','first']:['shared','second'],tools:[]}))
    const {unmount}=render(<ToolPackageBindings binding={{tool_packages:['count-first']}} shared={['count-second']}/>)
    expect(await screen.findByText('工具包 1 · 工具 3')).toBeInTheDocument()
    unmount()
    useSettingsStore.setState({skillPackages:[]})
  })

})
