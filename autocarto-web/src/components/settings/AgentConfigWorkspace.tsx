"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import ConfirmDialog from "@/components/common/ConfirmDialog";

import ActionBar, { ActionButton } from "@/components/settings/ActionBar";
import AlertPanel from "@/components/settings/AlertPanel";
import StatusBadge from "@/components/settings/StatusBadge";
import AgentCreateDialog from "@/components/settings/AgentCreateDialog";
import AgentEditDrawer from "@/components/settings/AgentEditDrawer";
import EmptyStatePanel from "@/components/settings/EmptyStatePanel";
import { TextInput } from "@/components/settings/FormField";
import InfoTooltip from "@/components/settings/InfoTooltip";
import ListDetailLayout from "@/components/settings/ListDetailLayout";
import ListPanelHeader from "@/components/settings/ListPanelHeader";

import DropdownMenu from "@/components/settings/DropdownMenu";
import TabBar from "@/components/settings/TabBar";
import AgentFlowSelection from "./AgentFlowSelection";
import FlowPolicySettings from "./FlowPolicySettings";
import FullScreenSettingsDrawer from "@/components/settings/FullScreenSettingsDrawer";
import { useSettingsActionStatus } from "@/hooks/useSettingsActionStatus";
import {
  confirmDiscardChanges,
  useUnsavedChangesGuard,
} from "@/hooks/useUnsavedChangesGuard";
import api, {
  AgentConfigData,
  AgentConfigMeta,
  AgentDefData,
  describeSettingsMutation,
  extractMutationFromError,
  getApiErrorMessage,
  LLMConfigPayload,
  PlanAgentDefData,
} from "@/lib/api";
import { formatDateTime } from "@/lib/settings-utils";
import { useSettingsStore } from "@/stores/settingsStore";
import { useRuntimeSnapshotStore } from "@/stores/runtimeSnapshotStore";
import MetaConfigSection from "@/app/settings/agents/_components/MetaConfigSection";
import PlanConfigPanel from "@/app/settings/agents/_components/PlanConfigPanel";
import RuntimeConfigPanel from "@/app/settings/agents/_components/RuntimeConfigPanel";
import GlobalConfigEditDrawer from "@/app/settings/agents/_components/GlobalConfigEditDrawer";
import {
  normalizeConfigData,
  normalizePlanAgentForSave,
  normalizeRuntimeAgentForSave,
} from "@/app/settings/agents/_components/normalizers";

function buildEmptyPlanConfig(): NonNullable<AgentConfigData["plan"]> {
  return {
    enabled: true,
    max_iterations: 100,
    shared: {
      tool_packages: [],
    },
    agents: {},
  };
}

function PromptDialog({
  title,
  placeholder,
  value,
  onChange,
  loading,
  onCancel,
  onConfirm,
}: {
  title: string;
  placeholder: string;
  value: string;
  onChange: (v: string) => void;
  loading: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return createPortal(
    <div
      className="fixed inset-0 z-[80] flex items-center justify-center bg-zinc-950/60 px-3 backdrop-blur-sm"
      onClick={() => { if (!loading) onCancel(); }}
    >
      <div
        className="w-full max-w-md rounded border border-zinc-200/80 bg-white/96 p-4 dark:border-zinc-800/80 dark:bg-zinc-950/96"
        role="dialog"
        aria-modal="true"
        aria-label={title}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="text-[13px] font-semibold text-zinc-900 dark:text-zinc-100">{title}</div>
        <input
          autoFocus
          className="mt-3 h-8 w-full rounded-sm border border-zinc-300 bg-zinc-100 px-2.5 text-xs text-zinc-800 outline-none transition-colors placeholder:text-zinc-400 focus:border-blue-500 focus:bg-white focus:ring-2 focus:ring-blue-500/10 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100"
          placeholder={placeholder}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter" && value.trim()) onConfirm(); if (e.key === "Escape") onCancel(); }}
        />
        <div className="mt-4 flex items-center justify-end gap-2">
          <button
            className="rounded-sm border border-zinc-300 px-3 py-1.5 text-xs text-zinc-700 transition-colors hover:bg-zinc-100 disabled:opacity-50 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800"
            onClick={onCancel}
            disabled={loading}
          >
            取消
          </button>
          <button
            className="rounded-sm border border-blue-300 bg-blue-50 px-3 py-1.5 text-xs text-blue-700 transition-colors hover:bg-blue-100 disabled:opacity-50 dark:border-blue-900/60 dark:bg-blue-950/30 dark:text-blue-300 dark:hover:bg-blue-950/50"
            onClick={onConfirm}
            disabled={!value.trim() || loading}
          >
            {loading ? "处理中..." : "确认"}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}

function formatMutationError(error: unknown, fallback: string) {
  const mutation = extractMutationFromError(error);
  if (mutation?.required_action === "stop_active_runs") {
    const details = [
      (mutation.blocking_runs || []).length > 0
        ? `阻塞会话：${mutation.blocking_runs?.join("、")}`
        : "",
      (mutation.blocking_instances || []).length > 0
        ? `关联实例：${mutation.blocking_instances?.join("、")}`
        : "",
      mutation.next_step || "",
    ]
      .filter(Boolean)
      .join("；");
    return [getApiErrorMessage(error, fallback), details].filter(Boolean).join("；");
  }
  return getApiErrorMessage(error, fallback);
}

function cloneConfigData(data: AgentConfigData): AgentConfigData {
  return normalizeConfigData(
    JSON.parse(JSON.stringify(data)) as AgentConfigData,
  );
}

export default function AgentConfigWorkspace() {
  const configs = useSettingsStore((s) => s.agentConfigs);
  const skillPackages = useSettingsStore((s) => s.skillPackages);
  const llmProviders = useSettingsStore((s) => s.llmProviders);
  const refreshLLMProviders = useSettingsStore((s) => s.refreshLLMProviders);
  const refreshAgentConfigs = useSettingsStore((s) => s.refreshAgentConfigs);
  const getAgentConfigData = useSettingsStore((s) => s.getAgentConfigData);
  const refreshAgentConfigData = useSettingsStore(
    (s) => s.refreshAgentConfigData,
  );
  const invalidateAgentConfigData = useSettingsStore(
    (s) => s.invalidateAgentConfigData,
  );
  const refreshSkillPackages = useSettingsStore((s) => s.refreshSkillPackages);
  const fetchRuntimeSnapshot = useRuntimeSnapshotStore((s) => s.fetchSnapshot);

  const [selectedConfigName, setSelectedConfigName] = useState("");
  const [configData, setConfigData] = useState<AgentConfigData | null>(null);
  const [originalConfigData, setOriginalConfigData] =
    useState<AgentConfigData | null>(null);
  const [activeTab, setActiveTab] = useState("runtime");
  const [loadingList, setLoadingList] = useState(true);
  const [loadingData, setLoadingData] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [newConfigName, setNewConfigName] = useState("");
  const [cloneConfigName, setCloneConfigName] = useState("");
  const [configSearchQuery, setConfigSearchQuery] = useState("");

  const [createAgentOpen, setCreateAgentOpen] = useState(false);
  const [editingRuntimeAgentName, setEditingRuntimeAgentName] = useState("");
  const [editingPlanAgentName, setEditingPlanAgentName] = useState("");
  const [deleteConfigDialogOpen, setDeleteConfigDialogOpen] = useState(false);
  const [deleteRuntimeAgentDialogOpen, setDeleteRuntimeAgentDialogOpen] =
    useState(false);
  const [pendingDeleteRuntimeAgentName, setPendingDeleteRuntimeAgentName] =
    useState("");
  const [createAgentDirty, setCreateAgentDirty] = useState(false);
  const [createConfigDialogOpen, setCreateConfigDialogOpen] = useState(false);
  const [cloneConfigDialogOpen, setCloneConfigDialogOpen] = useState(false);
  const [runtimeDrawerDirty, setRuntimeDrawerDirty] = useState(false);
  const [planDrawerDirty, setPlanDrawerDirty] = useState(false);
  const [configDrawerOpen, setConfigDrawerOpen] = useState(false);
  const [editingGlobalMode, setEditingGlobalMode] = useState<"runtime" | "plan" | "">("");
  const { actionStatus, showActionStatus } = useSettingsActionStatus();

  const selectedConfigRef = useRef("");
  useEffect(() => {
    selectedConfigRef.current = selectedConfigName;
  }, [selectedConfigName]);

  const normalizedDraftConfig = useMemo(() => {
    if (!configData) {
      return "";
    }
    return JSON.stringify(normalizeConfigData(configData));
  }, [configData]);

  const normalizedOriginalConfig = useMemo(() => {
    if (!originalConfigData) {
      return "";
    }
    return JSON.stringify(normalizeConfigData(originalConfigData));
  }, [originalConfigData]);

  const isConfigDirty = Boolean(
    normalizedDraftConfig && normalizedDraftConfig !== normalizedOriginalConfig,
  );
  const hasUnsavedChanges =
    isConfigDirty || createAgentDirty || runtimeDrawerDirty || planDrawerDirty;

  useUnsavedChangesGuard(hasUnsavedChanges);

  const reportMutation = useCallback(
    (
      result: Parameters<typeof describeSettingsMutation>[0],
      message: string,
    ) => {
      const feedback = describeSettingsMutation(result, message);
      showActionStatus(feedback.message, feedback.warning);
    },
    [showActionStatus],
  );

  const loadConfigData = useCallback(
    async (name: string) => {
      if (!name) {
        return;
      }
      setLoadingData(true);
      setError("");
      try {
        const [rawData] = await Promise.all([
          getAgentConfigData(name),
          refreshSkillPackages("all", name).catch(() => undefined),
        ]);
        if (!rawData) {
          throw new Error("配置不存在");
        }
        const draft = cloneConfigData(rawData);
        setConfigData(draft);
        setOriginalConfigData(cloneConfigData(draft));
      } catch (e) {
        setError(e instanceof Error ? e.message : "加载配置失败");
        setConfigData(null);
        setOriginalConfigData(null);
      } finally {
        setLoadingData(false);
      }
    },
    [getAgentConfigData, refreshSkillPackages],
  );

  const loadConfigs = useCallback(
    async (preferred?: string) => {
      setLoadingList(true);
      setError("");
      try {
        await refreshAgentConfigs();
        const list = useSettingsStore.getState().agentConfigs;

        const target =
          preferred ||
          (selectedConfigRef.current &&
          list.some((item) => item.config_name === selectedConfigRef.current)
            ? selectedConfigRef.current
            : "") ||
          list.find((item) => item.is_active)?.config_name ||
          list[0]?.config_name ||
          "";

        setSelectedConfigName(target);
        if (target) {
          await loadConfigData(target);
        } else {
          setConfigData(null);
          setOriginalConfigData(null);
        }
      } catch (e) {
        setError(e instanceof Error ? e.message : "加载失败");
      } finally {
        setLoadingList(false);
      }
    },
    [loadConfigData, refreshAgentConfigs],
  );

  useEffect(() => {
    void loadConfigs();
    void refreshLLMProviders().catch(() => {});
  }, [loadConfigs, refreshLLMProviders]);

  const activeConfigName =
    configs.find((item) => item.is_active)?.config_name || "";

  const toolPackages = useMemo(() => skillPackages.filter((item) => item.kind === "category" && !item.deprecated),[skillPackages]);

  const modelOptions = useMemo(() => {
    const providers = (llmProviders as LLMConfigPayload | null)?.providers ?? [];
    return providers
      .map((provider) => {
        const value = String(provider.name || "").trim();
        if (!value) {
          return null;
        }
        const backend = String(provider.provider || "").trim();
        const model = String(provider.model || "").trim();
        return {
          value,
          label: [value, backend && model ? `${backend}/${model}` : model || backend]
            .filter(Boolean)
            .join(" · "),
        };
      })
      .filter(
        (item): item is { value: string; label: string } => item !== null,
      );
  }, [llmProviders]);

  const filteredConfigs = useMemo(() => {
    const query = configSearchQuery.trim().toLowerCase();
    if (!query) {
      return configs;
    }
    return configs.filter((item) => {
      const name = String(item.config_name || "").toLowerCase();
      const status = [
        item.is_active ? "active" : "",
        item.is_default ? "default" : "",
      ]
        .filter(Boolean)
        .join(" ");
      return name.includes(query) || status.includes(query);
    });
  }, [configSearchQuery, configs]);

  const selectedConfigMetrics = useMemo(() => {
    if (!configData) {
      return {
        runtimeAgents: 0,
        runtimeMainAgents: 0,
        planAgents: 0,
        planMainAgents: 0,
      };
    }
	const runtimeAgents = Object.values(configData.runtime_agents || {});
	const planAgents = Object.values(configData.has_plan ? configData.plan?.agents || {} : {});
    return {
      runtimeAgents: runtimeAgents.length,
      runtimeMainAgents: runtimeAgents.filter((agent) => Boolean(agent.is_main))
        .length,
      planAgents: planAgents.length,
      planMainAgents: planAgents.filter((agent) => Boolean(agent.is_main))
        .length,
    };
  }, [configData]);
  const agentTabs = useMemo(() => {
    return [
      {
        key: "runtime",
        label: "Runtime",
        badge: String(selectedConfigMetrics.runtimeAgents),
        hint:
          selectedConfigMetrics.runtimeMainAgents > 0
            ? `主控 ${selectedConfigMetrics.runtimeMainAgents}`
            : "运行智能体",
      },
      {
        key: "plan",
        label: "Plan",
		badge: configData?.has_plan && configData?.plan?.enabled
		  ? String(selectedConfigMetrics.planAgents)
		  : "关闭",
		hint: configData?.has_plan && configData?.plan?.enabled
		  ? `主规划 ${selectedConfigMetrics.planMainAgents}`
		  : "当前方案未启用",
      },
    ];
  }, [configData?.has_plan, configData?.plan?.enabled, selectedConfigMetrics]);

  const editingRuntimeAgent = useMemo(() => {
    if (!configData || !editingRuntimeAgentName) {
      return null;
    }
	return configData.runtime_agents[editingRuntimeAgentName] || null;
  }, [configData, editingRuntimeAgentName]);

  const editingPlanAgent = useMemo(() => {
    if (!configData || !configData.has_plan || !configData.plan || !editingPlanAgentName) {
      return null;
    }
    return configData.plan.agents[editingPlanAgentName] || null;
  }, [configData, editingPlanAgentName]);

  const updateDraftConfig = useCallback(
    (updater: (current: AgentConfigData) => AgentConfigData) => {
      setConfigData((prev) => {
        if (!prev) {
          return prev;
        }
        return cloneConfigData(updater(prev));
      });
    },
    [],
  );

  const saveCurrentConfig = useCallback(async () => {
    if (!selectedConfigName || !configData) {
      return false;
    }
    setSaving(true);
    setError("");
    try {
      const nextData = cloneConfigData(configData);
      const result = await api.saveAgentConfig(selectedConfigName, nextData);
      invalidateAgentConfigData(selectedConfigName);
      const refreshed = await refreshAgentConfigData(selectedConfigName);
      await refreshAgentConfigs();
      if (refreshed) {
        const nextDraft = cloneConfigData(refreshed);
        setConfigData(nextDraft);
        setOriginalConfigData(cloneConfigData(nextDraft));
      }
      if (result.runtime_applied) {
        await fetchRuntimeSnapshot().catch(() => undefined);
      }
      reportMutation(result, `方案 ${selectedConfigName} 已保存`);
      return true;
    } catch (e) {
      setError(formatMutationError(e, "保存失败"));
      return false;
    } finally {
      setSaving(false);
    }
  }, [
    configData,
    fetchRuntimeSnapshot,
    invalidateAgentConfigData,
    reportMutation,
    refreshAgentConfigData,
    refreshAgentConfigs,
    selectedConfigName,
  ]);

  const updateMeta = (nextMeta: AgentConfigMeta) => {
    updateDraftConfig((current) => ({
      ...current,
      meta: nextMeta,
		has_plan: Boolean(nextMeta.plan_enabled) || current.has_plan,
		plan: Boolean(nextMeta.plan_enabled)
		  ? {
			  ...(current.plan || buildEmptyPlanConfig()),
			  enabled: true,
		    }
		  : current.has_plan
		    ? {
				...(current.plan || buildEmptyPlanConfig()),
				enabled: false,
		      }
		    : undefined,
    }));
  };

  const updateRuntimeGlobal = useCallback(
    (nextGlobal: AgentConfigData["runtime_shared"]) => {
      updateDraftConfig((current) => ({
        ...current,
        runtime_shared: nextGlobal,
      }));
    },
    [updateDraftConfig],
  );

  const updatePlanConfig = useCallback(
    (nextPlan: AgentConfigData["plan"]) => {
		if (!nextPlan) {
			return;
		}
      updateDraftConfig((current) => ({
        ...current,
			has_plan: true,
        meta: {
          ...current.meta,
          plan_enabled: Boolean(nextPlan.enabled),
        },
        plan: nextPlan,
      }));
    },
    [updateDraftConfig],
  );

  const discardDraftChanges = useCallback(() => {
    if (!isConfigDirty || !originalConfigData) {
      return;
    }
    if (!confirmDiscardChanges("当前方案有未保存修改，确定放弃吗？")) {
      return;
    }
    setConfigData(cloneConfigData(originalConfigData));
    setError("");
  }, [isConfigDirty, originalConfigData]);

  const selectConfig = useCallback(
    async (name: string) => {
      if (!name || name === selectedConfigName) {
        return;
      }
      if (
        isConfigDirty &&
        !confirmDiscardChanges("切换方案会丢失当前未保存修改，确定继续吗？")
      ) {
        return;
      }
      setSelectedConfigName(name);
      await loadConfigData(name);
    },
    [isConfigDirty, loadConfigData, selectedConfigName],
  );

  const createConfig = async (overrideName?: string) => {
    const name = (overrideName ?? newConfigName).trim();
    if (!name) {
      return;
    }
    setError("");
    try {
      const result = await api.createAgentConfig(name);
      setNewConfigName("");
      await refreshAgentConfigs();
      const feedback = describeSettingsMutation(
        result.mutation,
        `配置 ${name} 已创建`,
      );
      if (!isConfigDirty) {
        setSelectedConfigName(name);
        await loadConfigData(name);
        showActionStatus(feedback.message, feedback.warning);
      } else {
        showActionStatus(
          feedback.warning
            ? `${feedback.message}；可在左侧列表中手动切换`
            : `${feedback.message}，可在左侧列表中手动切换`,
          feedback.warning,
        );
      }
    } catch (e) {
      setError(formatMutationError(e, "创建失败"));
    }
  };

  const cloneConfig = async (overrideName?: string) => {
    const source = selectedConfigName.trim();
    const target = (overrideName ?? cloneConfigName).trim();
    if (!source || !target) {
      return;
    }
    setError("");
    try {
      const result = await api.cloneAgentConfig(source, target);
      setCloneConfigName("");
      await refreshAgentConfigs();
      const feedback = describeSettingsMutation(
        result.mutation,
        `已从 ${source} 复制为 ${target}`,
      );
      if (!isConfigDirty) {
        setSelectedConfigName(target);
        await loadConfigData(target);
        showActionStatus(feedback.message, feedback.warning);
      } else {
        showActionStatus(
          feedback.warning
            ? `${feedback.message}；可在左侧列表中手动切换`
            : `${feedback.message}，可在左侧列表中手动切换`,
          feedback.warning,
        );
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "复制失败");
    }
  };

  const activateConfig = async () => {
    if (!selectedConfigName || isConfigDirty) {
      if (isConfigDirty) {
        setError("请先保存当前方案，再执行激活操作");
      }
      return;
    }
    setError("");
    try {
      const result = await api.activateAgentConfig(selectedConfigName);
      await loadConfigs(selectedConfigName);
      if (result.runtime_applied) {
        await fetchRuntimeSnapshot().catch(() => undefined);
      }
      reportMutation(result, `已激活配置 ${selectedConfigName}`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "激活失败");
    }
  };

  const deleteConfig = async () => {
    if (!selectedConfigName || selectedConfigName === "default") {
      return;
    }
    setSaving(true);
    setError("");
    try {
      const deletedName = selectedConfigName;
      const result = await api.deleteAgentConfig(deletedName);
      invalidateAgentConfigData(selectedConfigName);
      await loadConfigs();
      setDeleteConfigDialogOpen(false);
      reportMutation(result, `配置 ${deletedName} 已删除`);
    } catch (e) {
      setError(formatMutationError(e, "删除失败"));
    } finally {
      setSaving(false);
    }
  };

  const updateRuntimeAgent = async (
    agentName: string,
    nextAgent: AgentDefData,
  ) => {
    updateDraftConfig((current) => ({
      ...current,
      runtime_agents: {
		...current.runtime_agents,
        [agentName]: normalizeRuntimeAgentForSave(nextAgent),
      },
    }));
  };

  const addRuntimeAgent = async (agentName: string, agent: AgentDefData) => {
    if (!configData) {
      throw new Error("请先选择配置方案");
    }
    const trimmedName = agentName.trim();
    if (!trimmedName) {
      throw new Error("智能体标识不能为空");
    }
		const runtimeAgents = configData.runtime_agents;
    if (runtimeAgents[trimmedName]) {
      throw new Error(`Runtime 智能体 ${trimmedName} 已存在`);
    }
    updateDraftConfig((current) => ({
      ...current,
      runtime_agents: {
			...current.runtime_agents,
        [trimmedName]: normalizeRuntimeAgentForSave(agent),
      },
    }));
  };

  const removeRuntimeAgent = async (agentName: string) => {
    updateDraftConfig((current) => {
		const nextAgents = { ...current.runtime_agents };
      delete nextAgents[agentName];
      return {
        ...current,
        runtime_agents: nextAgents,
      };
    });
    setDeleteRuntimeAgentDialogOpen(false);
    setPendingDeleteRuntimeAgentName("");
  };

  return (
    <div className="h-full min-h-0 flex-1 overflow-hidden">
      <ActionBar
        dirty={hasUnsavedChanges}
        status={actionStatus}
        left={
          <>
            <ActionButton
              onClick={() => {
                if (
                  isConfigDirty &&
                  !confirmDiscardChanges(
                    "刷新会丢失当前方案未保存修改，确定继续吗？",
                  )
                ) {
                  return;
                }
                void loadConfigs(selectedConfigName);
              }}
              disabled={loadingList || loadingData || saving}
            >
              {loadingList || loadingData ? "加载中..." : "刷新"}
            </ActionButton>

            <ActionButton
              variant="primary"
              onClick={() => void saveCurrentConfig()}
              disabled={
                !selectedConfigName || !configData || !isConfigDirty || saving
              }
            >
              保存方案
            </ActionButton>
          </>
        }
        right={
          <DropdownMenu
            label="更多操作"
            items={[
              { key: "discard", label: "放弃修改", disabled: !isConfigDirty || saving, onClick: discardDraftChanges },
              { key: "activate", label: "设为激活", disabled: !selectedConfigName || selectedConfigName === activeConfigName || isConfigDirty, onClick: () => void activateConfig() },
              { key: "delete", label: "删除方案", disabled: !selectedConfigName || selectedConfigName === "default", onClick: () => setDeleteConfigDialogOpen(true) },
              { key: "sep1", separator: true },
              { key: "create", label: "新建方案", disabled: saving, onClick: () => {
                setNewConfigName("");
                setCreateConfigDialogOpen(true);
              } },
              { key: "clone", label: "复制方案", disabled: !selectedConfigName || saving, onClick: () => {
                setCloneConfigName("");
                setCloneConfigDialogOpen(true);
              } }
            ]}
          />
        }
      />

      <div className="mt-3">
        {error && (
          <AlertPanel
            className="mb-3"
            tone="error"
            title="智能体配置失败"
            message={error}
          />
        )}
      </div>

        <ListDetailLayout
          listTitle="方案目录"
          listSectionId="agent-config-list"
          list={
            <div className="space-y-2">
              <ListPanelHeader
                eyebrow="方案目录"
                title="配置检索"
                description="搜索配置方案并切换右侧工作区。"
                stats={[
                  {
                    label: "结果",
                    value: `${filteredConfigs.length}/${configs.length}`,
                  },
                ]}
              >
                <TextInput
                  value={configSearchQuery}
                  onChange={(event) => setConfigSearchQuery(event.target.value)}
                  placeholder="搜索方案..."
                />
              </ListPanelHeader>

              <div className="max-h-[620px] space-y-1 overflow-auto">
                {filteredConfigs.length === 0 && !loadingList && (
                  <EmptyStatePanel
                    compact
                    eyebrow="空列表"
                    title="暂无匹配方案"
                    description="当前关键词下未找到配置方案，请调整检索条件或创建新方案。"
                  />
                )}
                {filteredConfigs.map((cfg) => (
                  <div
                    key={cfg.config_name}
                    className={`w-full rounded border px-2.5 py-2 text-left text-xs transition-colors ${
                      selectedConfigName === cfg.config_name
                        ? "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900/70 dark:bg-blue-900/30 dark:text-blue-300"
                        : "border-zinc-200/70 bg-zinc-50/80 text-zinc-700 hover:border-zinc-300 hover:bg-zinc-100 dark:border-zinc-800/70 dark:bg-zinc-900/50 dark:text-zinc-200 dark:hover:border-zinc-700 dark:hover:bg-zinc-800"
                    }`}
                    onClick={() => {
                      void selectConfig(cfg.config_name);
                    }}
                  >
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0">
                        <div className="truncate font-medium">
                          {cfg.config_name}
                        </div>
                        <div className="mt-1 text-[11px] text-zinc-500 dark:text-zinc-400">
                          最近更新：{formatDateTime(cfg.updated_at)}
                        </div>
                      </div>
                      <div className="flex flex-wrap items-center justify-end gap-1.5 text-[10px]">
                        {cfg.is_active && (
                          <StatusBadge tone="success" size="xs">
                            激活
                          </StatusBadge>
                        )}
                        {cfg.is_default && (
                          <StatusBadge tone="accent" size="xs">
                            默认
                          </StatusBadge>
                        )}
                        <button
                          type="button"
                          className="rounded border border-zinc-200 px-1.5 py-0.5 text-[10px] text-zinc-500 hover:bg-zinc-100 hover:text-zinc-700 dark:border-zinc-700 dark:text-zinc-400 dark:hover:bg-zinc-800 dark:hover:text-zinc-200"
                          onClick={(event) => {
                            event.stopPropagation();
                            void selectConfig(cfg.config_name);
                            setConfigDrawerOpen(true);
                          }}
                        >
                          编辑
                        </button>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          }
          detail={
            !selectedConfigName ? (
              <EmptyStatePanel
                eyebrow="详情工作区"
                title="尚未选择方案"
                description="从左侧方案目录选择一个配置方案，以编辑 Runtime / Plan 协作策略。"
              />
            ) : !configData ? (
              <EmptyStatePanel
                eyebrow="详情工作区"
                title="配置数据加载中"
                description="正在同步当前方案的全局设置、Runtime 角色与 Plan 角色配置。"
              />
            ) : null
          }
        />

        <FullScreenSettingsDrawer
          open={Boolean(selectedConfigName) && Boolean(configData) && configDrawerOpen}
          title={selectedConfigName ? `编辑智能体方案 · ${selectedConfigName}` : "编辑智能体方案"}
          onClose={() => setConfigDrawerOpen(false)}
        >
          {selectedConfigName && configData ? (
            <div id="agent-config-detail" className="settings-anchor-target space-y-3">
              <ActionBar dirty={isConfigDirty} status={actionStatus} right={<ActionButton variant="primary" disabled={!isConfigDirty || saving} onClick={() => void saveCurrentConfig()}>{saving ? "保存中..." : "保存当前方案"}</ActionButton>} />
              <div className="rounded border border-zinc-200 bg-zinc-50 px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-950/55">
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <div className="flex items-center gap-1.5">
                      <div className="text-[11px] font-semibold text-zinc-900 dark:text-zinc-100">
                        {selectedConfigName}
                      </div>
                      <InfoTooltip content="直接维护全局元数据，以及 Runtime / Plan 智能体的协作编排。" />
                    </div>
                    <div className="text-[11px] text-zinc-500 dark:text-zinc-400">
                      Runtime {selectedConfigMetrics.runtimeAgents} · Plan{" "}
						{configData.has_plan && configData.plan?.enabled
						  ? selectedConfigMetrics.planAgents
						  : "关闭"}
                    </div>
                  </div>
                </div>

                <div className="space-y-3">
                  <MetaConfigSection
                    meta={configData.meta}
                    saving={saving}
                    onChange={updateMeta}
                  />

                  {(configData.migration_warnings || []).length > 0 && <AlertPanel tone="warning" title="配置迁移提示" message={<ul>{configData.migration_warnings?.map((warning,index) => <li key={`${warning.code}-${index}`}>{warning.message} · {warning.path}{warning.package ? ` · ${warning.package}` : ""}</li>)}</ul>} />}
                  <AgentFlowSelection scheme={selectedConfigName} value={configData.flows} onChange={(flows) => updateDraftConfig((current) => ({...current,flows}))} />
                  <FlowPolicySettings value={configData.flow_policy} onChange={(flow_policy) => updateDraftConfig((current) => ({...current,flow_policy}))} />

                  <div className="rounded border border-zinc-200/60 bg-zinc-50 px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-950/60 dark:shadow-zinc-950/30">
                    <div className="mb-2 flex flex-wrap items-start justify-between gap-3">
                      <div className="flex items-center gap-1.5">
                        <div className="text-xs font-semibold uppercase tracking-[0.12em] text-zinc-700 dark:text-zinc-200">
                          角色工作区
                        </div>
                        <InfoTooltip content="在 Runtime 与 Plan 两类智能体间切换，查看并维护职责分工。" />
                      </div>
                      <div className="text-[11px] text-zinc-500 dark:text-zinc-400">
                        当前聚焦：
                        {activeTab === "runtime" ? "运行智能体" : "规划智能体"}
                      </div>
                    </div>

                    <TabBar
                      tabs={agentTabs}
                      activeKey={activeTab}
                      onChange={setActiveTab}
                    />
                  </div>

                  {activeTab === "runtime" && (
                    <RuntimeConfigPanel
                      configData={configData}
                      saving={saving}
                      onEditGlobal={() => setEditingGlobalMode("runtime")}
                      onEditAgent={setEditingRuntimeAgentName}
                      onDeleteAgent={(name) => {
                        setPendingDeleteRuntimeAgentName(name);
                        setDeleteRuntimeAgentDialogOpen(true);
                      }}
                      onCreateAgent={() => setCreateAgentOpen(true)}
                      onToggleAgent={(name, enabled) => {
                        if (!configData) {
                          return;
                        }
                        const current =
		configData.runtime_agents[name];
                        if (!current) {
                          return;
                        }
                        void updateRuntimeAgent(name, { ...current, enabled });
                      }}
                    />
                  )}

                  {activeTab === "plan" && (
                    <PlanConfigPanel
                      configData={configData}
                      saving={saving}
                      onChangePlan={updatePlanConfig}
                      onEditGlobal={() => setEditingGlobalMode("plan")}
                      onEditAgent={setEditingPlanAgentName}
                    />
                  )}
                </div>
            </div>
          ) : null}
        </FullScreenSettingsDrawer>

        <AgentCreateDialog
          open={createAgentOpen}
          toolPackages={toolPackages}
          modelOptions={modelOptions}
          onClose={() => setCreateAgentOpen(false)}
          onDirtyChange={setCreateAgentDirty}
          onSubmit={async (agentName, agent) => {
            await addRuntimeAgent(agentName, agent);
          }}
        />

        <AgentEditDrawer
          open={Boolean(editingRuntimeAgentName)}
          mode="runtime"
          agentName={editingRuntimeAgentName}
          agent={editingRuntimeAgent}
          toolPackages={toolPackages}
          modelOptions={modelOptions}
          onClose={() => setEditingRuntimeAgentName("")}
          onDirtyChange={setRuntimeDrawerDirty}
          onSave={async (agent) => {
            if (!editingRuntimeAgentName) {
              return;
            }
            await updateRuntimeAgent(
              editingRuntimeAgentName,
              normalizeRuntimeAgentForSave(agent as AgentDefData),
            );
          }}
        />

        <AgentEditDrawer
          open={Boolean(editingPlanAgentName)}
          mode="plan"
          agentName={editingPlanAgentName}
          agent={editingPlanAgent}
          toolPackages={toolPackages}
          modelOptions={modelOptions}
          onClose={() => setEditingPlanAgentName("")}
          onDirtyChange={setPlanDrawerDirty}
	          onSave={async (agent) => {
	            if (!configData || !editingPlanAgentName) {
	              return;
	            }
            const nextAgent = normalizePlanAgentForSave(
              agent as PlanAgentDefData,
            );
	            updateDraftConfig((current) => ({
	              ...current,
				  has_plan: true,
	              plan: {
	                ...(current.plan || buildEmptyPlanConfig()),
	                agents: {
	                  ...(current.plan?.agents || {}),
	                  [editingPlanAgentName]: nextAgent,
	                },
	              },
	            }));
	          }}
        />

        {configData && (
          <GlobalConfigEditDrawer
            open={editingGlobalMode === "runtime"}
            mode="runtime"
            global={configData.runtime_shared}
            toolPackages={toolPackages}
            onClose={() => setEditingGlobalMode("")}
            onSave={(nextGlobal) => {
              updateRuntimeGlobal(nextGlobal);
              setEditingGlobalMode("");
            }}
          />
        )}

        {configData?.has_plan && configData.plan && (
          <GlobalConfigEditDrawer
            open={editingGlobalMode === "plan"}
            mode="plan"
            global={configData.plan.shared}
            toolPackages={toolPackages}
            onClose={() => setEditingGlobalMode("")}
            onSave={(nextGlobal) => {
              updatePlanConfig({
                ...configData.plan!,
                shared: nextGlobal,
              });
              setEditingGlobalMode("");
            }}
          />
        )}

        <ConfirmDialog
          open={deleteConfigDialogOpen}
          title="删除配置方案"
          description={`确定删除配置方案 ${selectedConfigName} 吗？该操作不可撤销。`}
          confirmText="删除"
          danger
          loading={saving}
          onCancel={() => setDeleteConfigDialogOpen(false)}
          onConfirm={() => {
            void deleteConfig();
          }}
        />

        <ConfirmDialog
          open={deleteRuntimeAgentDialogOpen}
          title="删除 Runtime 智能体"
          description={`确定删除 Runtime 智能体 ${pendingDeleteRuntimeAgentName} 吗？该操作不可撤销。`}
          confirmText="删除"
          danger
          loading={saving}
          onCancel={() => {
            setDeleteRuntimeAgentDialogOpen(false);
            setPendingDeleteRuntimeAgentName("");
          }}
          onConfirm={() => {
            void removeRuntimeAgent(pendingDeleteRuntimeAgentName);
          }}
        />

        {createConfigDialogOpen && (
          <PromptDialog
            title="新建方案"
            placeholder="请输入方案名称"
            value={newConfigName}
            onChange={setNewConfigName}
            loading={saving}
            onCancel={() => setCreateConfigDialogOpen(false)}
            onConfirm={() => {
              if (newConfigName.trim()) {
                setCreateConfigDialogOpen(false);
                void createConfig(newConfigName.trim());
              }
            }}
          />
        )}

        {cloneConfigDialogOpen && (
          <PromptDialog
            title="复制方案"
            placeholder="请输入新方案名称"
            value={cloneConfigName}
            onChange={setCloneConfigName}
            loading={saving}
            onCancel={() => setCloneConfigDialogOpen(false)}
            onConfirm={() => {
              if (cloneConfigName.trim()) {
                setCloneConfigDialogOpen(false);
                void cloneConfig(cloneConfigName.trim());
              }
            }}
          />
        )}
      </div>
    
  );
}
