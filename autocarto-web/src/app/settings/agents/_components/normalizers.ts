"use client";

import {
  AgentConfigData,
  AgentConfigMeta,
  AgentDefData,
  AgentGlobalConfig,
  PlanAgentDefData,
  PlanConfig,
} from "@/lib/api";
export { resolveSkillId } from "@/lib/settings-utils";
import { parseExecutionMode } from "@/lib/runIntent";

export function toStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.map((item) => String(item || "").trim()).filter(Boolean);
}

export function toRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object") {
    return {};
  }
  return value as Record<string, unknown>;
}

export function ensureSkillPackages(
  packages?: string[] | null,
): string[] {
  return toStringArray(packages);
}

export function ensureSkillPackagesFromUnknown(value: unknown): string[] {
  if (Array.isArray(value)) {
    return toStringArray(value);
  }
  const record = toRecord(value);
  if (record && Array.isArray(record.skill_packages)) {
    return toStringArray(record.skill_packages);
  }
  return [];
}

export function collectSkillSelections(packages?: string[] | null): string[] {
  const normalized = ensureSkillPackages(packages);
  return Array.from(new Set([...normalized]));
}

export function normalizeGlobalConfig(globalRaw: unknown): AgentGlobalConfig {
  const raw = toRecord(globalRaw);
  return {
    skill_packages: toStringArray(raw.skill_packages),
  };
}

export function normalizeRuntimeAgent(agentRaw: unknown): AgentDefData {
  const raw = toRecord(agentRaw);
  const prompts = toRecord(raw.prompts);
  return {
    display_name: String(raw.display_name || ""),
    description: String(raw.description || ""),
    is_main: Boolean(raw.is_main),
    deletable: Boolean(raw.deletable),
    enabled: raw.enabled !== false,
    llm: {
      provider: String(toRecord(raw.llm).provider || ""),
    },
    skill_packages: ensureSkillPackagesFromUnknown(raw.skill_packages),
    prompts: Object.fromEntries(
      Object.entries(prompts)
        .filter(([key]) => String(key || "").trim())
        .map(([key, value]) => [key, String(value || "")]),
    ),
  };
}

export function normalizePlanAgent(agentRaw: unknown): PlanAgentDefData {
  const raw = toRecord(agentRaw);
  const prompts = toRecord(raw.prompts);
	const mode = String(raw.mode || "").trim();
	return {
		display_name: String(raw.display_name || ""),
		is_main: Boolean(raw.is_main),
		...(mode ? { mode } : {}),
		llm: {
		  provider: String(toRecord(raw.llm).provider || ""),
		},
    skill_packages: ensureSkillPackagesFromUnknown(raw.skill_packages),
    prompts: {
      static_instructions: String(prompts.static_instructions || ""),
      runtime_mode_instruction: String(prompts.runtime_mode_instruction || ""),
      user_prompt_template: String(
        prompts.user_prompt_template || "{{ user_input }}",
      ),
    },
  };
}

export function normalizePlanConfig(planRaw: unknown): PlanConfig | undefined {
	if (!planRaw || typeof planRaw !== "object") {
		return undefined;
	}
  const raw = toRecord(planRaw);
	if (!raw.agents || typeof raw.agents !== "object") {
		return undefined;
	}
  const agents: Record<string, PlanAgentDefData> = {};
  Object.entries(toRecord(raw.agents)).forEach(([name, value]) => {
    agents[name] = normalizePlanAgent(value);
  });

  const maxIterations = Number(raw.max_iterations || 100);
  return {
    enabled: raw.enabled !== false,
    max_iterations:
      Number.isFinite(maxIterations) && maxIterations > 0 ? maxIterations : 100,
    shared: normalizeGlobalConfig(raw.shared),
    agents,
  };
}

export function normalizeConfigMeta(meta: unknown): AgentConfigMeta {
  const raw = toRecord(meta);
  const rawMode = String(raw.default_execution_mode || "")
    .trim()
    .toLowerCase();
  const normalizedMode = parseExecutionMode(rawMode) || "build";
  return {
    default_execution_mode: normalizedMode,
    description: String(raw.description || "").trim(),
    plan_enabled: raw.plan_enabled !== false,
  };
}

export function normalizeConfigData(rawData: AgentConfigData): AgentConfigData {
	if (!rawData.runtime_agents || typeof rawData.runtime_agents !== "object") {
		throw new Error("missing runtime_agents in agent config data");
	}
  const runtimeAgents: Record<string, AgentDefData> = {};
  Object.entries(rawData.runtime_agents).forEach(([name, value]) => {
    runtimeAgents[name] = normalizeRuntimeAgent(value);
  });

	const normalizedMeta = normalizeConfigMeta(rawData.meta);
	const hasPlan = Boolean(rawData.has_plan);
	const normalizedPlan = hasPlan ? normalizePlanConfig(rawData.plan) : undefined;
	if (hasPlan && !normalizedPlan) {
		throw new Error("missing plan config in agent profile payload");
	}
	if (normalizedPlan) {
		normalizedPlan.enabled = normalizedMeta.plan_enabled;
	}

	return {
		meta: normalizedMeta,
		runtime_shared: normalizeGlobalConfig(rawData.runtime_shared),
		runtime_agents: runtimeAgents,
		has_plan: hasPlan,
		plan: normalizedPlan,
		diagnostics: rawData.diagnostics,
	};
}

export function normalizeRuntimeAgentForSave(
  agent: AgentDefData,
): AgentDefData {
  return normalizeRuntimeAgent(agent);
}

export function normalizePlanAgentForSave(
  agent: PlanAgentDefData,
): PlanAgentDefData {
  return {
    ...normalizePlanAgent(agent),
    mode: 'all',
  };
}

export function countRuntimeAgentSkills(agent: AgentDefData): number {
  const values = collectSkillSelections(agent.skill_packages);
  return new Set(values).size;
}

export function countPlanAgentSkills(agent: PlanAgentDefData): number {
  const values = collectSkillSelections(agent.skill_packages);
  return new Set(values).size;
}
