"use client";
import { useEffect,useState } from "react";
import Link from "next/link";
import { AgentFlowsConfig } from "@/lib/api";
import { flowApi } from "@/features/flows/api";
import { FlowSummary } from "@/features/flows/types";
import { EditorSection } from "./shared/AgentFormParts";

export default function AgentFlowSelection({scheme,value,onChange}:{scheme:string;value?:AgentFlowsConfig;onChange:(next:AgentFlowsConfig|undefined)=>void}) {
  const [flows,setFlows]=useState<FlowSummary[]>([]),[loading,setLoading]=useState(true),[error,setError]=useState("");
  useEffect(() => { let current=true;setLoading(true);setError("");void flowApi.list(scheme).then((items)=>{if(current)setFlows(items);}).catch((error)=>{if(current)setError(error instanceof Error?error.message:"流程列表加载失败");}).finally(()=>{if(current)setLoading(false);});return()=>{current=false};},[scheme]);
  const selected = value ? value.enabled : flows.map((flow)=>flow.id);
  const missing = value?.enabled.filter((id)=>!flows.some((flow)=>flow.id===id))||[];
  return <EditorSection title="启用的流程" description="流程按步骤安排角色，工具包决定角色具备哪些能力。未指定清单时全部启用。">
    <div className="mb-2 flex items-center justify-between text-xs"><span>{value ? `已选 ${selected.length} 个流程` : "默认全部启用"}</span><button type="button" onClick={()=>onChange(undefined)} className="text-blue-600">恢复全部启用</button></div>
    {loading?<p className="text-xs text-zinc-500">加载流程中…</p>:error?<p role="alert" className="text-xs text-amber-700">{error}。当前启用设置已保留。</p>:flows.length===0?<p className="text-xs text-zinc-500">暂无流程包，可前往流程中心导入或新建。</p>:<ul className="space-y-2">{flows.map((flow)=>{const counts=flow.diagnostics_summary;return <li key={flow.id} className="flex items-start justify-between gap-3 rounded border border-zinc-200 p-3 text-xs dark:border-zinc-700"><label className="flex gap-2"><input type="checkbox" checked={selected.includes(flow.id)} onChange={(event)=>onChange({...value,enabled:event.target.checked?[...new Set([...selected,flow.id])]:selected.filter((id)=>id!==flow.id)})}/><span><Link href={`/flows?flow=${encodeURIComponent(flow.id)}&scheme=${encodeURIComponent(scheme)}`} className="font-medium text-blue-600">{flow.name}</Link><span className="ml-2 text-zinc-500">{flow.version} · {flow.source==="builtin"?"内置":"用户"}</span><span className="mt-1 block text-zinc-500">{flow.description}</span></span></label>{counts?.errors > 0 && <span className="text-red-600">有错误</span>}</li>})}</ul>}
    {missing.length>0&&!loading&&<p role="alert" className="mt-2 text-xs text-amber-700">以下已启用流程当前未找到，设置将保留：{missing.join("、")}</p>}
  </EditorSection>;
}
