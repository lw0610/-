"use client";

import { AgentGlobalConfig, toolPackageSelections } from "@/lib/api";
import ToolPackageBindings from "@/components/settings/ToolPackageBindings";
import InfoTooltip from "@/components/settings/InfoTooltip";
import StatusBadge from "@/components/settings/StatusBadge";

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
  const selectedSkills = toolPackageSelections(global);
  const skillCount = selectedSkills.length;

  return (
    <div className="rounded border border-zinc-200/60 bg-zinc-50 px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-950/60 dark:shadow-zinc-950/30">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex items-center gap-1.5">
          <div className="text-xs font-semibold uppercase tracking-[0.12em] text-zinc-700 dark:text-zinc-200">
            {label === "Plan" ? "所有规划角色共享的工具包" : "所有执行角色共享的工具包"}
          </div>
          <InfoTooltip content="这里选择的工具包供该模式的所有角色共同使用。" />
        </div>
        <div className="flex flex-wrap items-center gap-2 text-[11px]">
          <StatusBadge>工具包 {skillCount}</StatusBadge>
          <button
            type="button"
            onClick={onEdit}
            className="rounded border border-zinc-200 px-2 py-0.5 text-[11px] text-zinc-600 transition-colors hover:bg-zinc-100 hover:text-zinc-800 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-800 dark:hover:text-zinc-100"
          >
            编辑
          </button>
        </div>
      </div>
      <ToolPackageBindings binding={global} mode={label === "Plan" ? "plan" : "build"} />
    </div>
  );
}
