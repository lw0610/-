"use client";

import { useState } from "react";
import api, { SkillPackageSummary, ToolCatalogItem } from "@/lib/api";

export const TOOL_PACKAGE_GROUPS = ["数据", "在线数据", "矢量", "栅格与分析", "制图", "版面与输出", "脚本", "质检"];
export const packageLabel = (item: SkillPackageSummary) => item.display_name || item.name || item.package_id;

export default function ToolPackageSelector({ packages, value, onChange }: {
  packages: SkillPackageSummary[];
  value: string[];
  onChange: (next: string[]) => void;
}) {
  const [query,setQuery] = useState("");
  const [expanded,setExpanded] = useState<string[]>([]);
  const [tools,setTools] = useState<Record<string,ToolCatalogItem[]>>({});
  const [errors,setErrors] = useState<Record<string,string>>({});
  const categories = packages.filter((item) => item.kind === "category" && !item.deprecated);
  const unique = [...new Map(categories.map((item) => [item.package_id,item])).values()];
  const visible = unique.filter((item) => !item.hidden && `${packageLabel(item)} ${item.description || ""} ${item.domain || ""}`.toLowerCase().includes(query.toLowerCase()));
  const foundation = visible.filter((item) => item.system);
  const selectable = visible.filter((item) => !item.system);
  const groups = [...TOOL_PACKAGE_GROUPS,...new Set(selectable.map((item) => item.domain || "其他").filter((name) => !TOOL_PACKAGE_GROUPS.includes(name)))];
  const unknown = value.filter((name) => !categories.some((item) => item.package_id === name));
  const expand = async (item: SkillPackageSummary) => {
    if (expanded.includes(item.package_id)) { setExpanded((current) => current.filter((id) => id !== item.package_id)); return; }
    setExpanded((current) => [...current,item.package_id]);
    if (tools[item.package_id]) return;
    try {
      const [detail,catalog] = await Promise.all([api.getSkillPackage(item.package_id,item.scope,item.owner_config),api.getToolCatalog("global")]);
      const names = new Set(detail.tool_refs || detail.front_matter?.tools || detail.tools.map((tool) => String(tool.metadata?.name || "")));
      const index = new Map(catalog.tool_infos.map((tool) => [tool.name,tool]));
      setTools((current) => ({...current,[item.package_id]:[...names].map((name) => index.get(name) || {name})}));
    } catch (error) { setErrors((current) => ({...current,[item.package_id]:error instanceof Error ? error.message : "工具清单加载失败"})); }
  };
  const row = (item: SkillPackageSummary, locked = false) => {
    const selected = locked || value.includes(item.package_id);
    return <div key={item.package_id} className="rounded border border-zinc-200 p-3 text-xs dark:border-zinc-700">
      <div className="flex items-start justify-between gap-3">
        <label className="flex min-w-0 gap-2">
          <input type="checkbox" checked={selected} disabled={locked || (item.enabled === false && !selected)} onChange={(event) => onChange(event.target.checked ? [...new Set([...value,item.package_id])] : value.filter((id) => id !== item.package_id))} />
          <span><span className="font-medium">{packageLabel(item)}</span>{locked && <span className="ml-2 text-zinc-500">所有角色自动拥有 · 已锁定</span>}<span className="mt-1 block text-zinc-500">{item.description}</span>{item.enabled === false && <span className="text-amber-600">已停用</span>}</span>
        </label>
        <button type="button" className="shrink-0 text-blue-600" aria-expanded={expanded.includes(item.package_id)} onClick={() => void expand(item)}>工具 {item.tool_ref_count ?? item.tool_count} · {expanded.includes(item.package_id) ? "收起" : "展开"}</button>
      </div>
      {expanded.includes(item.package_id) && <ul className="mt-2 space-y-1 border-t border-zinc-200 pt-2 dark:border-zinc-700">{errors[item.package_id] ? <li role="alert">{errors[item.package_id]}</li> : tools[item.package_id] ? tools[item.package_id].map((tool) => <li key={tool.name}><span>{tool.display_name || tool.name}</span><span className="ml-2 text-zinc-400">{tool.name}</span></li>) : <li>加载工具清单中…</li>}</ul>}
    </div>;
  };
  return <div className="space-y-3">
    <input aria-label="搜索工具包" placeholder="搜索工具包名称或说明" value={query} onChange={(event) => setQuery(event.target.value)} className="w-full rounded border border-zinc-300 bg-transparent p-2 text-xs dark:border-zinc-700" />
    {foundation.length > 0 && <section aria-label="系统基础包" className="space-y-2"><h4 className="text-xs font-medium">系统基础包</h4>{foundation.map((item) => row(item,true))}</section>}
    {groups.map((group) => { const items = selectable.filter((item) => (item.domain || "其他") === group); return items.length > 0 ? <section key={group} className="space-y-2"><h4 className="text-xs font-medium">{group}</h4>{items.map((item) => row(item))}</section> : null; })}
    {visible.length === 0 && <p className="text-xs text-zinc-500">没有匹配的工具包。</p>}
    {unknown.length > 0 && <p role="alert" className="text-xs text-amber-700">以下既有编号未在工具包目录中找到，保存时保留：{unknown.join("、")}</p>}
  </div>;
}
