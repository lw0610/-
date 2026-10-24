"use client";

import { AgentConfigData, PlanConfig, toolPackageSelections } from "@/lib/api";
import { Field, TextInput } from "@/components/settings/FormField";
import InfoTooltip from "@/components/settings/InfoTooltip";
import StatusBadge from "@/components/settings/StatusBadge";
import GlobalConfigSection from "./GlobalConfigSection";
import PlanAgentCard from "./PlanAgentCard";
import { countPlanAgentSkills } from "./normalizers";

interface PlanConfigPanelProps {
  configData: AgentConfigData;
  saving: boolean;
  onChangePlan: (plan: PlanConfig) => void;
  onEditGlobal: () => void;
  onEditAgent: (name: string) => void;
}

export default function PlanConfigPanel({
  configData,
  saving,
  onChangePlan,
  onEditGlobal,
  onEditAgent,
}: PlanConfigPanelProps) {
	if (!configData.has_plan || !configData.plan) {
		return (
		  <div className="rounded border border-zinc-200/60 bg-zinc-50 px-3 py-2.5 text-xs text-zinc-500 dark:border-zinc-800 dark:bg-zinc-950/60 dark:text-zinc-400 dark:shadow-zinc-950/30">
			当前方案未配置 Plan 结构，请在上方「方案设置」中开启 Plan。
		  </div>
		);
	}

	if (!configData.plan.enabled) {
    return (
      <div className="rounded border border-zinc-200/60 bg-zinc-50 px-3 py-2.5 text-xs text-zinc-500 dark:border-zinc-800 dark:bg-zinc-950/60 dark:text-zinc-400 dark:shadow-zinc-950/30">
        Plan runtime 未启用，请在上方「方案设置」中开启。
      </div>
    );
  }

  const plan = configData.plan;
  const sortedPlanAgents = Object.entries(plan.agents).sort(
    ([nameA, agentA], [nameB, agentB]) => {
      if (Boolean(agentA.is_main) !== Boolean(agentB.is_main)) {
        return agentA.is_main ? -1 : 1;
      }
      return String(agentA.display_name || nameA).localeCompare(
        String(agentB.display_name || nameB),
      );
    },
  );

  const mainAgents = sortedPlanAgents.filter(([, agent]) =>
    Boolean(agent.is_main),
  ).length;
  const skillBindings = sortedPlanAgents.reduce(
    (sum, [, agent]) => sum + countPlanAgentSkills(agent),
    0,
  );

  return (
    <div className="space-y-3">
      <div className="rounded border border-zinc-200/60 bg-zinc-50 px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-950/60 dark:shadow-zinc-950/30">
        <div className="mb-2 flex flex-wrap items-start justify-between gap-3">
          <div className="flex items-center gap-1.5">
            <div className="text-xs font-semibold uppercase tracking-[0.12em] text-zinc-700 dark:text-zinc-200">
              Plan Runtime 参数
            </div>
            <InfoTooltip content="控制 Planner 的最大迭代次数。" />
          </div>
          <div className="flex flex-wrap items-center gap-2 text-[11px]">
            <StatusBadge>迭代 {plan.max_iterations}</StatusBadge>
            <StatusBadge tone="success">主规划 {mainAgents}</StatusBadge>
            <StatusBadge>总数 {sortedPlanAgents.length}</StatusBadge>
          </div>
        </div>
        <div className="grid gap-2.5 md:grid-cols-[minmax(0,220px),minmax(0,1fr)]">
          <Field
            label="最大规划迭代数"
            description="限制一次规划的最大轮次，复杂任务可按需要提高上限。"
          >
            <TextInput
              type="number"
              value={String(plan.max_iterations)}
              disabled={saving}
              onChange={(event) => {
                const value = Number(event.target.value || 0);
                onChangePlan({
                  ...plan,
                  max_iterations: value > 0 ? value : 1,
                });
              }}
            />
          </Field>
        </div>
      </div>

      <GlobalConfigSection
        label="Plan"
        global={plan.shared}
        onEdit={onEditGlobal}
      />

      <div className="rounded border border-zinc-200 bg-white px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-900">
        <div className="mb-2 flex flex-wrap items-center justify-between gap-3">
          <div className="text-xs font-semibold uppercase tracking-[0.12em] text-zinc-700 dark:text-zinc-200">
            Plan 智能体
          </div>
          <div className="flex flex-wrap items-center gap-2 text-[11px]">
            <StatusBadge>总数 {sortedPlanAgents.length}</StatusBadge>
            <StatusBadge tone="success">主规划 {mainAgents}</StatusBadge>
            <StatusBadge>工具包 {skillBindings}</StatusBadge>
          </div>
        </div>
        <div className="grid gap-2.5 md:grid-cols-2 xl:grid-cols-3">
          {sortedPlanAgents.map(([agentName, agent]) => (
            <PlanAgentCard
              key={agentName}
              agentName={agentName}
              agent={agent}
              shared={toolPackageSelections(plan.shared)}
              saving={saving}
              onEdit={() => onEditAgent(agentName)}
            />
          ))}
        </div>
      </div>
    </div>
  );
}
