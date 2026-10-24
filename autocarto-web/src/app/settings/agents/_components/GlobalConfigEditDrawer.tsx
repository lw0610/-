"use client";

import { useEffect, useState } from "react";
import { AgentGlobalConfig, SkillPackageSummary, toolPackageSelections } from "@/lib/api";
import FullScreenSettingsDrawer from "@/components/settings/FullScreenSettingsDrawer";
import InfoTooltip from "@/components/settings/InfoTooltip";
import ToolPackageSelector from "@/components/settings/ToolPackageSelector";


interface GlobalConfigEditDrawerProps {
  open: boolean;
  mode: "runtime" | "plan";
  global: AgentGlobalConfig;
  toolPackages: SkillPackageSummary[];
  onClose: () => void;
  onSave: (global: AgentGlobalConfig) => void;
}

export default function GlobalConfigEditDrawer({
  open,
  mode,
  global,
  toolPackages,
  onClose,
  onSave,
}: GlobalConfigEditDrawerProps) {
  const [draft, setDraft] = useState<AgentGlobalConfig>({ tool_packages: [] });

  useEffect(() => {
    if (!open) {
      return;
    }
    setDraft({ ...global, tool_packages: toolPackageSelections(global) });
  }, [global, open]);

  return (
    <FullScreenSettingsDrawer
      open={open}
      title={`${mode === "runtime" ? "Runtime" : "Plan"} 共享工具包`}
      onClose={onClose}
    >
      <div className="space-y-3">
        <div className="rounded border border-zinc-200/60 bg-zinc-50 px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-950/60 dark:shadow-zinc-950/30">
          <div className="mb-2 flex items-center gap-1.5">
            <div className="text-xs font-semibold uppercase tracking-[0.12em] text-zinc-700 dark:text-zinc-200">
              {mode === "runtime" ? "Runtime" : "Plan"} 所有角色共享的工具包
            </div>
            <InfoTooltip content="统一控制当前方案下该模式智能体默认继承的工具包。" />
          </div>
          <ToolPackageSelector
            packages={toolPackages}
            value={toolPackageSelections(draft)}
            onChange={(next) => {
              setDraft({
                ...draft,
                tool_packages: next,
              });
            }}
          />
        </div>

        <div className="flex items-center justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded-sm border border-zinc-300 px-3 py-1.5 text-xs text-zinc-700 transition-colors hover:bg-zinc-100 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800"
          >
            取消
          </button>
          <button
            type="button"
            onClick={() => onSave({ ...draft, tool_packages: toolPackageSelections(draft) })}
            className="rounded-sm border border-blue-300 bg-blue-50 px-3 py-1.5 text-xs text-blue-700 transition-colors hover:bg-blue-100 dark:border-blue-900/60 dark:bg-blue-950/30 dark:text-blue-300 dark:hover:bg-blue-950/50"
          >
            保存共享工具包
          </button>
        </div>
      </div>
    </FullScreenSettingsDrawer>
  );
}
