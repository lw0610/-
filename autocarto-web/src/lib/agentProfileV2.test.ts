import { describe,it,expect } from 'vitest'
import { normalizeAgentProfilePayload,serializeAgentProfilePayload,normalizeToolCatalogResponse } from './api'
import { normalizeConfigData } from '@/app/settings/agents/_components/normalizers'

describe('v2 profile editor contract',()=>{
  it('round-trips fields when only a display name changes',()=>{
    const raw={version:'2.0',meta:{default_execution_mode:'build' as const,description:'方案',plan_enabled:true,future_meta:'keep'},flows:{enabled:['flow-a'],future_binding:'keep'},migration_warnings:[{code:'unknown',path:'runtime.agents.custom',package:'unknown-package',message:'保留'}],runtime_shared:{tool_packages:['vector-fields'],legacy_unresolved_packages:['shared-unknown'],future_shared:17},runtime_agents:[{name:'custom-role',agent:{display_name:'旧名',description:'职责',kind:'executor' as const,executor:'route_knowledge',flow_only:true,enabled:true,tool_packages:[],legacy_unresolved_packages:['unknown-package'],llm:{provider:'special',future_model:'keep'},prompts:{static_instructions:'备用',user_prompt_template:'{{ raw }}',future_prompt:'keep'},future_role:'keep'}}],has_plan:true,plan:{enabled:true,max_iterations:17,shared:{tool_packages:['crs-projection']},agents:[{name:'planner',agent:{display_name:'规划',kind:'agent' as const,mode:'readonly',tool_packages:['file-operations'],prompts:{static_instructions:'plan',future_prompt:'keep'}}}],future_plan:'keep'},future_top:'keep'}
    const data=normalizeConfigData(normalizeAgentProfilePayload(raw))
    data.runtime_agents['custom-role'].display_name='新名'
    const saved=serializeAgentProfilePayload(data)
    expect(saved.version).toBe('2.0')
    expect(saved.flows).toBeUndefined()
    expect(saved.migration_warnings).toEqual(raw.migration_warnings)
    expect(saved.runtime_shared).toMatchObject(raw.runtime_shared)
    expect(saved.runtime_agents[0].agent).toMatchObject({...raw.runtime_agents[0].agent,display_name:'新名'})
    expect(saved.plan).toBeUndefined()
    expect(saved.has_plan).toBe(false)
    expect(saved).toMatchObject({future_top:'keep'})
    expect(saved.runtime_agents[0].agent).not.toHaveProperty('skill_packages')
    expect(saved.runtime_shared).not.toHaveProperty('skill_packages')
  })
  it('reads v1 selection for one-version compatibility and submits v1 for server migration',()=>{
    const data=normalizeAgentProfilePayload({meta:{default_execution_mode:'build',description:'old',plan_enabled:false},runtime_shared:{skill_packages:['global']},runtime_agents:[{name:'unrelated-id',agent:{display_name:'role',description:'',enabled:true,skill_packages:['method'],prompts:{}}}],has_plan:false})
    expect(data.version).toBe('1.0')
    expect(data.runtime_agents['unrelated-id'].tool_packages).toEqual(['method'])
    const saved=serializeAgentProfilePayload(data)
    expect(saved.runtime_agents[0].agent.skill_packages).toEqual(['method'])
    expect(saved.runtime_agents[0].agent).not.toHaveProperty('tool_packages')
    expect(saved.runtime_agents[0].agent.kind).toBeUndefined()
  })
  it('honors explicit empty v2 packages instead of reviving legacy selection',()=>{
    const data=normalizeAgentProfilePayload({version:'2.0',runtime_shared:{tool_packages:[],skill_packages:['old']},runtime_agents:[],has_plan:false})
    expect(data.runtime_shared.tool_packages).toEqual([])
    expect(serializeAgentProfilePayload(data).runtime_shared).not.toHaveProperty('skill_packages')
  })
  it('retains public Chinese catalog metadata from actual top-level items',()=>{
    const result=normalizeToolCatalogResponse({items:[{id:'bar_chart',display_name:'绘制分类柱状图',description:'按字段比较',keywords:['分类比较'],package_id:'charts',availability:'available'}]})
    expect(result.tool_infos[0]).toMatchObject({name:'bar_chart',display_name:'绘制分类柱状图',keywords:['分类比较'],package_id:'charts'})
  })
})
