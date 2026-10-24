"use client";

import { AgentDefData } from "@/lib/api";
import ToolPackageBindings from "@/components/settings/ToolPackageBindings";
import { ActionButton } from "@/components/settings/ActionBar";
import InfoTooltip from "@/components/settings/InfoTooltip";
import StatusBadge from "@/components/settings/StatusBadge";

interface RuntimeAgentCardProps {
  agentName: string;
  agent: AgentDefData;
  shared?: string[];
  saving: boolean;
  onEdit: () => void;
  onToggle: () => void;
  onDelete: () => void;
}

export default function RuntimeAgentCard({
  agentName,
  agent,
  shared = [],
  saving,
  onEdit,
  onToggle,
  onDelete,
}: RuntimeAgentCardProps) {
  const provider = String(agent.llm?.provider || "").trim() || "继承全局";

  return (
    <div role="group" aria-label={`角色 ${agent.display_name || agentName}`} className="rounded border border-zinc-200 bg-white px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-900">
      <div className="mb-2 flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-1.5">
            <div className="truncate text-xs font-semibold text-zinc-900 dark:text-zinc-100">
              {agent.display_name || agentName}
            </div>
            <InfoTooltip
              content={
                agent.description || "暂无描述，建议补充职责边界与适用场景。"
              }
            />
          </div>
          <div className="mt-1 truncate text-[11px] text-zinc-500 dark:text-zinc-400">
            {agentName} · 模型 {provider}
          </div>
        </div>
        <div className="flex flex-wrap items-center justify-end gap-1.5 text-[10px]">
          <StatusBadge>{agent.kind === "executor" ? "专用执行器" : "通用智能体"}</StatusBadge>
          {agent.is_main && <StatusBadge tone="accent">主控</StatusBadge>}
          <StatusBadge tone={agent.enabled ? "success" : "neutral"}>
            {agent.enabled ? "启用" : "停用"}
          </StatusBadge>
        </div>
      </div>

      <p className="mb-2 line-clamp-2 text-xs text-zinc-500">{agent.description || agent.prompts?.static_instructions || "暂无职责说明"}</p>
      <ToolPackageBindings binding={agent} shared={shared} mode="build" />
      <div className="mt-2 flex flex-wrap items-center gap-2">
        <ActionButton onClick={onEdit} disabled={saving}>
          编辑
        </ActionButton>
        <ActionButton
          onClick={onToggle}
          disabled={saving || Boolean(agent.is_main)}
        >
          {agent.enabled ? "禁用" : "启用"}
        </ActionButton>
        <ActionButton
          variant="danger"
          onClick={onDelete}
          disabled={saving || Boolean(agent.is_main)}
        >
          删除
        </ActionButton>
      </div>
    </div>
  );
}
