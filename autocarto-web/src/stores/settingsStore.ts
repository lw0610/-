import { create } from "zustand";
import api, {
  AgentConfigData,
  AgentConfigSummary,
  LLMConfigPayload,
  SkillItem,
  SkillPackageScope,
  SkillPackageSummary,
  SkillReference,
  ToolCatalogItem,
} from "@/lib/api";
import { resolveSkillId } from "@/lib/settings-utils";
import { AppConfig } from "@/types/config";

interface SettingsState {
  config: AppConfig | null;
  llmProviders: LLMConfigPayload | null;
  agentConfigs: AgentConfigSummary[];
  agentConfigDataMap: Record<string, AgentConfigData>;
  toolCatalog: ToolCatalogItem[];
  skills: SkillItem[];
  skillPackages: SkillPackageSummary[];
  skillReferencesMap: Record<string, SkillReference[]>;
  refreshConfig: () => Promise<void>;
  refreshLLMProviders: () => Promise<void>;
  refreshAgentConfigs: () => Promise<void>;
  refreshAgentConfigData: (name: string) => Promise<AgentConfigData | null>;
  refreshTools: () => Promise<void>;
  refreshSkills: () => Promise<void>;
  refreshSkillPackages: (scope?: SkillPackageScope, configName?: string) => Promise<void>;
  refreshSkillReferences: (skillIds?: string[]) => Promise<void>;
  getAgentConfigData: (name: string) => Promise<AgentConfigData | null>;
  invalidateAgentConfigData: (name?: string) => void;
}

export const useSettingsStore = create<SettingsState>((set, get) => ({
  config: null,
  llmProviders: null,
  agentConfigs: [],
  agentConfigDataMap: {},
  toolCatalog: [],
  skills: [],
  skillPackages: [],
  skillReferencesMap: {},

  refreshConfig: async () => {
    const config = await api.getConfig();
    set({ config });
  },

  refreshLLMProviders: async () => {
    const llmProviders = await api.getLLMProviders();
    set({ llmProviders });
  },

  refreshAgentConfigs: async () => {
    const agentConfigs = await api.getAgentConfigs();
    set({ agentConfigs });
  },

  refreshAgentConfigData: async (name: string) => {
    const key = name.trim();
    if (!key) {
      return null;
    }
    const data = await api.getAgentConfigData(key);
    set((state) => ({
      agentConfigDataMap: {
        ...state.agentConfigDataMap,
        [key]: data,
      },
    }));
    return data;
  },

  refreshTools: async () => {
    const toolCatalog = await api.getTools();
    set({ toolCatalog });
  },

  refreshSkills: async () => {
    const skills = await api.getSkills();
    set({ skills });
    await get().refreshSkillReferences(
      skills.map((skill) => resolveSkillId(skill)).filter(Boolean),
    );
  },

  refreshSkillPackages: async (scope = "all", configName) => {
    const skillPackages = await api.getSkillPackages(scope, configName);
    set({ skillPackages });
  },

  refreshSkillReferences: async (skillIds) => {
    const existing = get().skillReferencesMap;
    const targetIds =
      skillIds && skillIds.length > 0
        ? skillIds
        : get()
            .skills.map((skill) => resolveSkillId(skill))
            .filter(Boolean);

    const nextMap: Record<string, SkillReference[]> = { ...existing };
    await Promise.all(
      targetIds.map(async (skillId) => {
        try { nextMap[skillId] = await api.getSkillReferences(skillId); } catch (error) { void error; nextMap[skillId] = []; }
      }),
    );
    set({ skillReferencesMap: nextMap });
  },

  getAgentConfigData: async (name: string) => {
    const key = name.trim();
    if (!key) {
      return null;
    }

    const cached = get().agentConfigDataMap[key];
    if (cached) {
      return cached;
    }

    return get().refreshAgentConfigData(key);
  },

  invalidateAgentConfigData: (name) => {
    if (!name) {
      set({ agentConfigDataMap: {} });
      return;
    }
    const key = name.trim();
    if (!key) {
      return;
    }
    set((state) => {
      const nextMap = { ...state.agentConfigDataMap };
      delete nextMap[key];
      return { agentConfigDataMap: nextMap };
    });
  },
}));
