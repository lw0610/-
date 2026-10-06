"use client";

import { AgentGlobalConfig } from "@/lib/api";
import InfoTooltip from "@/components/settings/InfoTooltip";
import StatusBadge from "@/components/settings/StatusBadge";
import {
  collectSkillSelections,
} from "./normalizers";

interface GlobalConfigSectionProps {
  label: string;
  global: AgentGlobalConfig;
  onEdit: () => void;
}

export default function GlobalConfigSection({
  label,
  global,
  onEdit,
}: GlobalConfigSectionProps) {
  const selectedSkills = collectSkillSelections(global.skill_packages);
  const skillCount = selectedSkills.length;
  const skillPreview = selectedSkills.slice(0, 3);

  return (
    <div className="rounded border border-zinc-200/60 bg-zinc-50 px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-950/60 dark:shadow-zinc-950/30">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex items-center gap-1.5">
          <div className="text-xs font-semibold uppercase tracking-[0.12em] text-zinc-700 dark:text-zinc-200">
            {label} 全局配置
          </div>
          <InfoTooltip content="统一控制公共技能包的默认注入。" />
        </div>
        <div className="flex flex-wrap items-center gap-2 text-[11px]">
          <StatusBadge>技能 {skillCount}</StatusBadge>
          <button
            type="button"
            onClick={onEdit}
            className="rounded border border-zinc-200 px-2 py-0.5 text-[11px] text-zinc-600 transition-colors hover:bg-zinc-100 hover:text-zinc-800 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-800 dark:hover:text-zinc-100"
          >
            编辑
          </button>
        </div>
      </div>
      <div className="mt-2 space-y-1">
        <div className="flex items-center gap-1 text-[12px] font-medium text-zinc-700 dark:text-zinc-200">
          <span>公共技能包摘要</span>
          <InfoTooltip content="统一选择当前方案内所有智能体默认可用的技能包。" />
        </div>
        {skillPreview.length > 0 ? (
          <div className="flex flex-wrap items-center gap-1.5 text-[11px] text-zinc-500 dark:text-zinc-400">
            {skillPreview.map((item) => (
              <span
                key={item}
                className="rounded border border-zinc-200 bg-white px-2 py-0.5 dark:border-zinc-700 dark:bg-zinc-900"
              >
                {item}
              </span>
            ))}
            {skillCount > skillPreview.length ? <span>+{skillCount - skillPreview.length}</span> : null}
          </div>
        ) : (
          <div className="text-[11px] text-zinc-500 dark:text-zinc-400">
            当前未配置公共技能包。
          </div>
        )}
      </div>
    </div>
  );
}
