"use client";

import { useMemo } from "react";
import { AgentConfigData } from "@/lib/api";
import { ActionButton } from "@/components/settings/ActionBar";
import StatusBadge from "@/components/settings/StatusBadge";
import RuntimeAgentCard from "./RuntimeAgentCard";
import GlobalConfigSection from "./GlobalConfigSection";
import { countRuntimeAgentSkills, normalizeRuntimeAgent } from "./normalizers";

interface RuntimeConfigPanelProps {
  configData: AgentConfigData;
  saving: boolean;
  onEditGlobal: () => void;
  onEditAgent: (name: string) => void;
  onDeleteAgent: (name: string) => void;
  onCreateAgent: () => void;
  onToggleAgent: (name: string, enabled: boolean) => void;
}

export default function RuntimeConfigPanel({
  configData,
  saving,
  onEditGlobal,
  onEditAgent,
  onDeleteAgent,
  onCreateAgent,
  onToggleAgent,
}: RuntimeConfigPanelProps) {
	const runtimeConfig = {
		shared: configData.runtime_shared,
		agents: configData.runtime_agents,
	};
  const sortedRuntimeAgents = useMemo(() => {
    return Object.entries(runtimeConfig.agents).sort(
      ([nameA, agentA], [nameB, agentB]) => {
        if (Boolean(agentA.is_main) !== Boolean(agentB.is_main)) {
          return agentA.is_main ? -1 : 1;
        }
        return String(agentA.display_name || nameA).localeCompare(
          String(agentB.display_name || nameB),
        );
      },
    );
  }, [runtimeConfig.agents]);

  const enabledAgents = sortedRuntimeAgents.filter(
    ([, agent]) => agent.enabled !== false,
  ).length;
  const mainAgents = sortedRuntimeAgents.filter(([, agent]) =>
    Boolean(agent.is_main),
  ).length;
  const skillBindings = sortedRuntimeAgents.reduce(
    (sum, [, agent]) => sum + countRuntimeAgentSkills(agent),
    0,
  );

  return (
    <div className="space-y-3">
      <GlobalConfigSection
        label="Runtime"
        global={runtimeConfig.shared}
        onEdit={onEditGlobal}
      />

      <div className="flex flex-wrap items-center justify-between gap-3 rounded border border-zinc-200/60 bg-zinc-50 px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-950/60 dark:shadow-zinc-950/30">
        <div className="flex flex-wrap items-center gap-2 text-[11px]">
          <StatusBadge>Runtime 智能体 {sortedRuntimeAgents.length}</StatusBadge>
          <StatusBadge tone="success">启用 {enabledAgents}</StatusBadge>
          <StatusBadge tone="accent">主控 {mainAgents}</StatusBadge>
          <StatusBadge>技能 {skillBindings}</StatusBadge>
        </div>
        <ActionButton onClick={onCreateAgent} disabled={saving}>
          新建 Runtime 智能体
        </ActionButton>
      </div>

      <div className="rounded border border-zinc-200 bg-white px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-900">
        <div className="mb-2 text-xs font-semibold uppercase tracking-[0.12em] text-zinc-700 dark:text-zinc-200">
          Runtime 智能体
        </div>
        <div className="grid gap-2.5 md:grid-cols-2 xl:grid-cols-3">
          {sortedRuntimeAgents.map(([agentName, agent]) => (
            <RuntimeAgentCard
              key={agentName}
              agentName={agentName}
              agent={agent}
              saving={saving}
              onEdit={() => onEditAgent(agentName)}
              onToggle={() => {
                const next = normalizeRuntimeAgent(agent);
                next.enabled = !Boolean(agent.enabled);
                onToggleAgent(agentName, next.enabled);
              }}
              onDelete={() => onDeleteAgent(agentName)}
            />
          ))}
        </div>
      </div>
    </div>
  );
}
