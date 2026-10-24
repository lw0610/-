"use client";
import { useEffect,useState } from "react";
import api, { SkillPackageSummary, toolPackageSelections, AgentDefData, AgentGlobalConfig, PlanAgentDefData } from "@/lib/api";
import { useSettingsStore } from "@/stores/settingsStore";
import { packageLabel } from "./ToolPackageSelector";

const referenceCache = new Map<string,Promise<string[]>>();
function references(item:SkillPackageSummary) {
  const key=[item.package_id,item.scope,item.owner_config,item.updated_at].join("::");
  if(!referenceCache.has(key)) referenceCache.set(key,api.getSkillPackage(item.package_id,item.scope,item.owner_config).then((detail)=>detail.tool_refs||detail.front_matter?.tools||detail.tools.map((tool)=>String(tool.metadata?.name||""))).catch((error)=>{referenceCache.delete(key);throw error;}));
  return referenceCache.get(key)!;
}
export default function ToolPackageBindings({binding,shared=[],mode="build"}:{binding:AgentDefData|PlanAgentDefData|AgentGlobalConfig;shared?:string[];mode?:"build"|"plan"}) {
  const packages=useSettingsStore((state)=>state.skillPackages);
  const explicit=toolPackageSelections(binding);
  const role=binding as AgentDefData;
  const automatic=packages.filter((item)=>item.system||item.hidden&&(item.auto_mount==="main"&&role.is_main&&mode==="build"||item.auto_mount==="plan_main"&&role.is_main&&mode==="plan"||role.executor&&item.auto_mount===`executor:${role.executor}`)).map((item)=>item.package_id);
  const allNames=[...new Set([...explicit,...shared,...automatic])];
  const selected=allNames.map((name)=>packages.find((item)=>item.package_id===name)).filter((item):item is SkillPackageSummary=>Boolean(item));
  const key=selected.map((item)=>[item.package_id,item.scope,item.owner_config,item.updated_at].join(":")).join("|");
  const [count,setCount]=useState<number|null>(null);
  useEffect(()=>{let current=true;setCount(null);void Promise.all(selected.map(references)).then((lists)=>{if(current)setCount(new Set(lists.flat()).size)}).catch(()=>{if(current)setCount(null)});return()=>{current=false}; // selected identities are fully represented by key.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  },[key]);
  return <div className="mt-2 space-y-1 text-xs"><p className="text-zinc-500">工具包 {explicit.length}{count!==null?` · 工具 ${count}`:""}</p><div className="flex flex-wrap gap-1">{explicit.map((name)=>{const item=packages.find((pkg)=>pkg.package_id===name);return <span key={name} className="rounded border border-zinc-200 px-1.5 py-0.5 dark:border-zinc-700">{item?packageLabel(item):name}</span>})}</div>{(binding.legacy_unresolved_packages||[]).length>0&&<p className="text-amber-700">未识别旧包：{binding.legacy_unresolved_packages?.join("、")}</p>}</div>;
}
