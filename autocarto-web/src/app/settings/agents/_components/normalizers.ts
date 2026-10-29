"use client";

import {
  AgentConfigData,
  AgentConfigMeta,
  AgentDefData,
  AgentGlobalConfig,
  PlanAgentDefData,
  PlanConfig,
  normalizeAgentGlobalConfig,
  normalizeRuntimeAgentDef,
  normalizePlanAgentDef,
  toolPackageSelections,
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
  return normalizeAgentGlobalConfig(toRecord(globalRaw));
}
export function normalizeRuntimeAgent(agentRaw: unknown): AgentDefData {
  return normalizeRuntimeAgentDef(toRecord(agentRaw));
}
export function normalizePlanAgent(agentRaw: unknown): PlanAgentDefData {
  return normalizePlanAgentDef(toRecord(agentRaw));
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
    ...raw,
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
    ...raw,
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

	return {
    ...rawData,
		meta: normalizedMeta,
		runtime_shared: normalizeGlobalConfig(rawData.runtime_shared),
		runtime_agents: runtimeAgents,
		has_plan: false,
		plan: undefined,
    flows: undefined,
    flow_policy: undefined,
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
  return normalizePlanAgent(agent);
}

export function countRuntimeAgentSkills(agent: AgentDefData): number {
  const values = toolPackageSelections(agent);
  return new Set(values).size;
}

export function countPlanAgentSkills(agent: PlanAgentDefData): number {
  const values = toolPackageSelections(agent);
  return new Set(values).size;
}
