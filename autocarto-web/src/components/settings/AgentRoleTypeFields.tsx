"use client";
import { AgentDefData } from "@/lib/api";
import { Field, SelectInput } from "./FormField";

export default function AgentRoleTypeFields({agent,onChange}:{agent:AgentDefData;onChange:(next:AgentDefData)=>void}) {
  const executor = agent.kind === "executor";
  return <div className="space-y-3">
    <Field label="角色类型"><SelectInput value={agent.kind || "agent"} disabled={Boolean(agent.is_main)} onChange={(event) => {
      if (event.target.value === "executor") onChange({...agent,kind:"executor",executor:agent.executor || "route_knowledge",flow_only:true,tool_packages:[]});
      else { const next = {...agent,kind:"agent" as const}; delete next.executor; delete next.flow_only; onChange(next); }
    }}><option value="agent">通用智能体</option><option value="executor">专用执行器</option></SelectInput></Field>
    {executor && <><Field label="执行器类型"><SelectInput value={agent.executor || "route_knowledge"} onChange={(event) => onChange({...agent,executor:event.target.value,flow_only:true})}><option value="route_knowledge">路线知识抽取</option>{agent.executor && agent.executor !== "route_knowledge" && <option value={agent.executor}>{agent.executor}</option>}</SelectInput></Field><p className="text-xs text-zinc-500">仅在流程中使用，不参与自由委派。规则来自流程步骤；职责说明可保留为未提供规则时的备用。</p></>}
  </div>;
}
