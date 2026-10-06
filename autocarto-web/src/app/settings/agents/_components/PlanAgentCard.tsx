"use client";

import { PlanAgentDefData } from "@/lib/api";
import { ActionButton } from "@/components/settings/ActionBar";
import InfoTooltip from "@/components/settings/InfoTooltip";
import StatusBadge from "@/components/settings/StatusBadge";
import { countPlanAgentSkills } from "./normalizers";

interface PlanAgentCardProps {
  agentName: string;
  agent: PlanAgentDefData;
  saving: boolean;
  onEdit: () => void;
}

export default function PlanAgentCard({
  agentName,
  agent,
  saving,
  onEdit,
}: PlanAgentCardProps) {
  const provider = String(agent.llm?.provider || "").trim() || "继承全局";
  const skillCount = countPlanAgentSkills(agent);

  return (
    <div className="rounded border border-zinc-200 bg-white px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-900">
      <div className="mb-2 flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-1.5">
            <div className="truncate text-xs font-semibold text-zinc-900 dark:text-zinc-100">
              {agent.display_name || agentName}
            </div>
            <InfoTooltip
              content={
                agent.is_main
                  ? "当前为主规划智能体，负责总体规划与上下文分发。"
                  : "用于分工式规划、拆解或专项分析任务。"
              }
            />
          </div>
          <div className="mt-1 truncate text-[11px] text-zinc-500 dark:text-zinc-400">
            {agentName} · 模型 {provider} · 技能 {skillCount}
          </div>
        </div>
        <div className="flex flex-wrap items-center justify-end gap-1.5 text-[10px]">
          {agent.is_main && <StatusBadge tone="success">主规划</StatusBadge>}
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <ActionButton onClick={onEdit} disabled={saving}>
          编辑
        </ActionButton>
      </div>
    </div>
  );
}
