"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import ActionBar, { ActionButton } from "@/components/settings/ActionBar";
import AlertPanel from "@/components/settings/AlertPanel";
import DropdownMenu from "@/components/settings/DropdownMenu";
import EmptyStatePanel from "@/components/settings/EmptyStatePanel";
import InfoTooltip from "@/components/settings/InfoTooltip";
import {
  CheckboxField,
  Field,
  NumberInput,
  SelectInput,
  TextInput,
} from "@/components/settings/FormField";
import ListDetailLayout from "@/components/settings/ListDetailLayout";
import ListPanelHeader from "@/components/settings/ListPanelHeader";
import StatusBadge from "@/components/settings/StatusBadge";
import FullScreenSettingsDrawer from "@/components/settings/FullScreenSettingsDrawer";
import { useSettingsActionStatus } from "@/hooks/useSettingsActionStatus";
import { useSettingsForm } from "@/hooks/useSettingsForm";
import api, { LLMConfigPayload, LLMProviderRegistryItem } from "@/lib/api";
import { clampInt } from "@/lib/config-utils";
import { useSettingsStore } from "@/stores/settingsStore";
import { LLMProviderConfig } from "@/types/config";

const DEFAULT_PROVIDER_CATALOG = [
  "openai",
  "anthropic",
  "google",
  "ollama",
] as const;
const STRATEGIES = ["summary", "truncate", "sliding_window"];

function normalizeProviderId(value: unknown) {
  const provider = String(value || "")
    .trim()
    .toLowerCase();
  return provider || "openai";
}

function formatProviderLabel(
  providerId: string,
  providerInfo?: LLMProviderRegistryItem,
) {
  const normalized = normalizeProviderId(providerId);
  if (!providerInfo?.display_name) {
    return normalized;
  }
  return providerInfo.display_name.toLowerCase() === normalized
    ? providerInfo.display_name
    : `${providerInfo.display_name} (${normalized})`;
}

function resolveProviderCapabilitySummary(
  provider?: LLMProviderConfig,
  providerInfo?: LLMProviderRegistryItem,
) {
  const hasApiKey = Boolean(provider?.has_api_key) || Boolean(String(provider?.api_key || "").trim());
  const resolvedBaseURL =
    String(provider?.base_url || "").trim() ||
    String(providerInfo?.default_base_url || "").trim();
  const supportsThinking =
    typeof providerInfo?.supports_thinking === "boolean"
      ? providerInfo.supports_thinking
      : Boolean(provider?.supports_thinking);
  const supportsPlanExecute =
    typeof providerInfo?.supports_plan_execute === "boolean"
      ? providerInfo.supports_plan_execute
      : Boolean(provider?.supports_plan_execute);
  const supportsForcedToolChoice =
    typeof providerInfo?.supports_forced_tool_choice === "boolean"
      ? providerInfo.supports_forced_tool_choice
      : Boolean(provider?.supports_forced_tool_choice);

  let readyForChat = true;
  let readinessReason = String(provider?.readiness_reason || "").trim();
  if (providerInfo?.requires_api_key && !hasApiKey) {
    readyForChat = false;
    readinessReason = "缺少 API Key，当前 Provider 无法进入运行态。";
  } else if (providerInfo?.requires_base_url && !resolvedBaseURL) {
    readyForChat = false;
    readinessReason = "缺少服务地址，当前 Provider 无法建立连接。";
  }

  return {
    hasApiKey,
    supportsThinking,
    supportsPlanExecute,
    supportsForcedToolChoice,
    readyForChat,
    readyForPlan: readyForChat && supportsPlanExecute,
    readyForAutoTitle: readyForChat,
    readinessReason,
  };
}

function isOllamaProvider(provider?: LLMProviderConfig, providerInfo?: LLMProviderRegistryItem) {
  const providerId = String(provider?.provider || providerInfo?.id || "").trim().toLowerCase();
  const backendType = String(providerInfo?.backend_type || "").trim().toLowerCase();
  return providerId === "ollama" || backendType === "ollama";
}

function getProviderReadinessLabel(
  duplicated: boolean,
  summary: ReturnType<typeof resolveProviderCapabilitySummary>,
  provider?: LLMProviderConfig,
  providerInfo?: LLMProviderRegistryItem,
) {
  if (duplicated) {
    return "名称冲突";
  }
  if (!summary.readyForChat) {
    return "待补齐";
  }
  if (isOllamaProvider(provider, providerInfo)) {
    return "聊天/工具适配";
  }
  if (!summary.supportsPlanExecute) {
    return "仅聊天";
  }
  return "聊天/Plan 就绪";
}

function getProviderReadinessMessage(
  duplicated: boolean,
  summary: ReturnType<typeof resolveProviderCapabilitySummary>,
  provider?: LLMProviderConfig,
  providerInfo?: LLMProviderRegistryItem,
) {
  if (duplicated) {
    return "名称重复，保存前需修正。";
  }
  if (summary.readyForChat) {
    if (isOllamaProvider(provider, providerInfo)) {
      return "Ollama 将通过文本工具调用适配层尝试执行工具，效果取决于本地模型输出质量。";
    }
    return summary.supportsPlanExecute
      ? "运行条件完整，可作为聊天和 Plan Provider。"
      : "运行条件完整，但当前 Provider 不支持 Plan 协议。";
  }
  return summary.readinessReason || "需要补齐运行条件。";
}

function buildDefaultProvider(seed = 1): LLMProviderConfig {
  return {
    name: `provider-${seed}`,
    provider: "openai",
    model: "gpt-4o-mini",
    base_url: "",
    max_tokens: 4096,
    temperature: 0.7,
    thinking_enabled: false,
    thinking_budget: 10240,
    max_context_tokens: 128000,
    max_context_usage: 90,
    auto_compress_threshold: 85,
    compress_strategy: "summary",
    api_key: "",
    has_api_key: false,
  };
}

function normalizePayload(payload: LLMConfigPayload): LLMConfigPayload {
  const providers = (payload.providers || []).map((item, idx) => {
    const name = (item.name || "").trim() || `provider-${idx + 1}`;
    return {
      ...item,
      name,
      provider: normalizeProviderId(item.provider),
      model: item.model || "",
      max_tokens: item.max_tokens ?? 4096,
      temperature: item.temperature ?? 0.7,
      thinking_enabled: Boolean(item.thinking_enabled),
      thinking_budget: item.thinking_budget ?? 10240,
      max_context_tokens: item.max_context_tokens ?? 128000,
      max_context_usage: item.max_context_usage ?? 90,
      auto_compress_threshold: item.auto_compress_threshold ?? 85,
      compress_strategy: item.compress_strategy || "summary",
      api_key: item.api_key || "",
      has_api_key: Boolean(item.has_api_key),
    } satisfies LLMProviderConfig;
  });
  const active = payload.active || providers[0]?.name || "";
  const quickModel = (payload.quick_model || "").trim();
  const providerNames = new Set(providers.map((item) => item.name.trim()));
  return {
    active,
    quick_model: providerNames.has(quickModel) ? quickModel : "",
    providers,
  };
}

export default function ModelConfigWorkspace() {
  const refreshLLMProviders = useSettingsStore((s) => s.refreshLLMProviders);
  const [selected, setSelected] = useState(0);
  const [registry, setRegistry] = useState<LLMProviderRegistryItem[]>([]);
  const [testing, setTesting] = useState(false);
  const [showApiKey, setShowApiKey] = useState(false);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const importInputRef = useRef<HTMLInputElement | null>(null);
  const { actionStatus, publishActionStatus, showActionStatus } =
    useSettingsActionStatus();

  const {
    form: data,
    setForm: setData,
    isDirty,
    loading,
    saving,
    reload,
    handleSave,
  } = useSettingsForm<LLMConfigPayload>({
    defaultValues: { active: "", quick_model: "", providers: [] },
    load: async () => {
      await refreshLLMProviders();
      const source = useSettingsStore.getState().llmProviders;
      if (!source) {
        throw new Error("LLM 配置读取失败");
      }
      const payload = normalizePayload(source);
      if (payload.providers.length === 0) {
        payload.providers.push(buildDefaultProvider(1));
        payload.active = payload.providers[0].name;
      }
      setSelected(0);
      return payload;
    },
    validate: (values) => {
      if (values.providers.length === 0) {
        return "至少保留一个 LLM 配置";
      }

      const emptyName = values.providers.find((item) => !item.name.trim());
      if (emptyName) {
        return "配置名称不能为空";
      }

      const dupSet = new Set<string>();
      const seen = new Set<string>();
      values.providers.forEach((item) => {
        const key = item.name.trim();
        if (!key) {
          return;
        }
        if (seen.has(key)) {
          dupSet.add(key);
          return;
        }
        seen.add(key);
      });
      if (dupSet.size > 0) {
        return `存在重复配置名称: ${Array.from(dupSet).join(", ")}`;
      }

      const activeNames = new Set(
        values.providers.map((item) => item.name.trim()),
      );
      if (!activeNames.has(values.active.trim())) {
        return "激活模型必须在配置列表中";
      }

      const quickModel = values.quick_model?.trim() || "";
      if (quickModel && !activeNames.has(quickModel)) {
        return "快速模型必须在配置列表中";
      }

      return null;
    },
    save: async (values) => {
      const payload: LLMConfigPayload = {
        active: values.active,
        quick_model: values.quick_model?.trim() || "",
        providers: values.providers.map((item) => ({
          ...item,
          name: item.name.trim(),
          model: item.model?.trim() || "",
          provider: normalizeProviderId(item.provider),
          base_url: item.base_url?.trim() || "",
          api_key: item.api_key?.trim() || "",
          max_tokens: clampInt(Number(item.max_tokens ?? 4096)),
          temperature: Math.max(
            0,
            Math.min(2, Number(item.temperature ?? 0.7)),
          ),
          thinking_budget: clampInt(Number(item.thinking_budget ?? 10240)),
          max_context_tokens: clampInt(
            Number(item.max_context_tokens ?? 128000),
          ),
          max_context_usage: Math.max(
            1,
            Math.min(100, clampInt(Number(item.max_context_usage ?? 90))),
          ),
          auto_compress_threshold: Math.max(
            1,
            Math.min(100, clampInt(Number(item.auto_compress_threshold ?? 85))),
          ),
          compress_strategy: item.compress_strategy || "summary",
        })),
      };
      const result = await api.saveLLMProviders(payload);
      await refreshLLMProviders();
      return result;
    },
    onSaveFeedback: ({ message, warning }) => {
      publishActionStatus(message, warning, false);
    },
    saveSuccessMessage: "LLM 配置已保存",
  });

  useEffect(() => {
    api
      .getLLMRegistry()
      .then((items) => setRegistry(items))
      .catch(() => setRegistry([]));
  }, []);

  useEffect(() => {
    setShowApiKey(false);
  }, [selected]);

  const current = data.providers[selected];
  const providerInfoMap = useMemo(() => {
    return new Map(
      registry.map((item) => [normalizeProviderId(item.id), item] as const),
    );
  }, [registry]);
  const currentProviderInfo = current
    ? providerInfoMap.get(normalizeProviderId(current.provider))
    : undefined;
  const currentCapabilitySummary = useMemo(
    () => resolveProviderCapabilitySummary(current, currentProviderInfo),
    [current, currentProviderInfo],
  );
  const currentIsQuickModel = Boolean(current && data.quick_model === current.name);

  const duplicateNames = useMemo(() => {
    const counter = new Map<string, number>();
    data.providers.forEach((provider) => {
      const name = provider.name.trim();
      counter.set(name, (counter.get(name) ?? 0) + 1);
    });
    const duplicates = new Set<string>();
    counter.forEach((count, name) => {
      if (name && count > 1) {
        duplicates.add(name);
      }
    });
    return duplicates;
  }, [data.providers]);

  const providerCatalog = useMemo(() => {
    const merged = new Set<string>(DEFAULT_PROVIDER_CATALOG);
    registry.forEach((item) => {
      const providerID = normalizeProviderId(item.id);
      if (providerID) {
        merged.add(providerID);
      }
    });
    data.providers.forEach((item) => {
      const providerID = normalizeProviderId(item.provider);
      if (providerID) {
        merged.add(providerID);
      }
    });
    return Array.from(merged);
  }, [data.providers, registry]);

  const updateCurrent = (
    updater: (provider: LLMProviderConfig) => LLMProviderConfig,
  ) => {
    setData((prev) => {
      const nextProviders = [...prev.providers];
      if (!nextProviders[selected]) {
        return prev;
      }
      nextProviders[selected] = updater(nextProviders[selected]);
      return {
        ...prev,
        providers: nextProviders,
      };
    });
  };

  const addProvider = () => {
    setData((prev) => {
      const nextProviders = [
        ...prev.providers,
        buildDefaultProvider(prev.providers.length + 1),
      ];
      return {
        ...prev,
        providers: nextProviders,
        active: prev.active || nextProviders[0].name,
      };
    });
    setSelected(data.providers.length);
  };

  const removeCurrent = () => {
    if (data.providers.length <= 1) {
      return;
    }

    setData((prev) => {
      const removed = prev.providers[selected];
      const nextProviders = prev.providers.filter((_, idx) => idx !== selected);
      const nextActive =
        prev.active === removed?.name
          ? nextProviders[0]?.name || ""
          : prev.active;
      const nextQuickModel =
        prev.quick_model === removed?.name ? "" : prev.quick_model;
      return {
        ...prev,
        active: nextActive,
        quick_model: nextQuickModel,
        providers: nextProviders,
      };
    });
    setSelected((idx) => Math.max(0, idx - 1));
  };

  const testConnection = async () => {
    if (!current) {
      return;
    }
    setTesting(true);
    try {
      await api.testLLMConnection(current);
      showActionStatus("连接测试成功");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "连接测试失败");
    } finally {
      setTesting(false);
    }
  };

  const exportConfig = () => {
    const json = JSON.stringify(data, null, 2);
    const blob = new Blob([json], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = "llm_config.json";
    anchor.click();
    URL.revokeObjectURL(url);
  };

  const importConfig = (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) {
      return;
    }
    const reader = new FileReader();
    reader.onload = (loadEvent) => {
      try { const json = JSON.parse(loadEvent.target?.result as string);
      const payload = normalizePayload(json);
      if (payload.providers.length === 0) {
        payload.providers.push(buildDefaultProvider(1));
        payload.active = payload.providers[0].name;
      }
      setData(payload);
      setSelected(0);
      showActionStatus("配置已导入，请点击保存以生效"); } catch (error) { void error; toast.error("导入失败：无效的 JSON 文件"); }
    };
    reader.readAsText(file);
    event.target.value = "";
  };

  return (
    <>
      <ActionBar
        dirty={isDirty}
        status={actionStatus}
        left={
          <>
            <ActionButton
              onClick={() => void reload()}
              disabled={loading || saving || testing}
            >
              {loading ? "加载中..." : "刷新"}
            </ActionButton>
            <span className="text-xs text-zinc-500 dark:text-zinc-400">
              共 {data.providers.length} 个配置
            </span>
          </>
        }
        right={
          <>
            <DropdownMenu
              label="更多操作"
              items={[
                {
                  key: "add",
                  label: "新增配置",
                  disabled: loading || saving || testing,
                  onClick: addProvider,
                },
                {
                  key: "remove",
                  label: "删除当前",
                  disabled:
                    loading ||
                    saving ||
                    testing ||
                    data.providers.length <= 1,
                  onClick: removeCurrent,
                },
                {
                  key: "export",
                  label: "导出 JSON",
                  disabled: loading || saving || testing,
                  onClick: exportConfig,
                },
                {
                  key: "import",
                  label: "导入 JSON",
                  disabled: loading || saving || testing,
                  onClick: () => importInputRef.current?.click(),
                },
              ]}
            />

            <ActionButton
              variant="primary"
              onClick={() => {
                void (async () => {
                  const ok = await handleSave();
                  if (ok) {
                    await reload();
                  }
                })();
              }}
              disabled={
                loading ||
                saving ||
                testing ||
                !isDirty ||
                duplicateNames.size > 0
              }
            >
              {saving ? "保存中..." : "保存"}
            </ActionButton>

            <input
              ref={importInputRef}
              type="file"
              accept=".json"
              className="hidden"
              onChange={importConfig}
              disabled={loading || saving || testing}
            />
          </>
        }
      />

      <div className="space-y-3">
        <div className="rounded border border-zinc-200 bg-zinc-50 px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-950/55">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-1.5">
              <div className="text-[13px] font-semibold text-zinc-900 dark:text-zinc-100">
                Provider 与推理设置
              </div>
              <InfoTooltip content="管理 Provider、快速模型、鉴权信息与上下文压缩策略。" />
            </div>
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-zinc-500 dark:text-zinc-400">
              <span>Provider {data.providers.length}</span>
              <span>激活 {data.active || "未设置"}</span>
            </div>
          </div>
        </div>

        <div className="rounded border border-zinc-200/60 bg-zinc-50 px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-950/60 dark:shadow-zinc-950/30">
          <div className="grid gap-2.5 md:grid-cols-[minmax(0,1fr),minmax(200px,240px)]">
            <Field
              label="快速模型"
              description="用于系统内部任务（如会话自动命名），留空则使用当前激活模型。"
            >
              <SelectInput
                value={data.quick_model || ""}
                onChange={(event) =>
                  setData((prev) => ({
                    ...prev,
                    quick_model: event.target.value,
                  }))
                }
                disabled={loading || saving || testing}
              >
                <option value="">
                  使用激活模型（{data.active || "未设置"}）
                </option>
                {data.providers.map((provider, idx) => (
                  <option
                    key={`quick-${provider.name}-${idx}`}
                    value={provider.name}
                  >
                    {provider.name || "-"}
                  </option>
                ))}
              </SelectInput>
            </Field>

            <div className="rounded border border-zinc-200 bg-white px-2.5 py-2 text-xs dark:border-zinc-800 dark:bg-zinc-900/80">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="text-xs text-zinc-500 dark:text-zinc-400">
                  当前激活
                </div>
                <div className="text-xs text-zinc-500 dark:text-zinc-400">
                  快速模型 {data.quick_model || "跟随激活"}
                </div>
              </div>
              <div className="mt-0.5 font-medium text-zinc-900 dark:text-zinc-100">
                {data.active || "未设置"}
              </div>
              <div className="mt-0.5 truncate text-xs text-zinc-500 dark:text-zinc-400">
                {current?.model || "从左侧选择配置查看详情"}
              </div>
            </div>
          </div>

          {current &&
            ((currentIsQuickModel && !currentCapabilitySummary.readyForAutoTitle) ||
              (data.active === current.name &&
                !currentCapabilitySummary.readyForPlan)) && (
            <AlertPanel
              className="mt-2"
              compact
              tone="warning"
              title={
                currentIsQuickModel
                  ? "快速模型待补齐运行条件"
                  : "激活模型待补齐 Plan 运行条件"
              }
              message={
                currentIsQuickModel
                  ? currentCapabilitySummary.readinessReason ||
                    "该 Provider 当前不能用于自动命名，系统会回退到其它就绪模型。"
                  : currentCapabilitySummary.readinessReason ||
                    "请先补齐 API Key 或连接地址后再作为主运行模型使用。"
              }
            />
          )}
        </div>

        <ListDetailLayout
          listTitle="Provider 目录"
          listSectionId="models-provider-list"
          list={
            <div className="space-y-1.5">
              <ListPanelHeader
                eyebrow="Provider 目录"
                title="模型配置集合"
                description="左侧管理配置集合，右侧编辑连接信息、鉴权与推理参数。"
                stats={[
                  { label: "Provider", value: `${data.providers.length}` },
                  { label: "激活", value: data.active || "未设置" },
                ]}
              />
              <div className="space-y-1">
                {data.providers.map((provider, idx) => {
                  const duplicated = duplicateNames.has(provider.name.trim());
                  const active = data.active === provider.name;
                  const quick = data.quick_model === provider.name;
                  const providerSummary = resolveProviderCapabilitySummary(
                    provider,
                    providerInfoMap.get(normalizeProviderId(provider.provider)),
                  );
                  return (
                  <div
                      key={`${provider.name}-${idx}`}
                      className={`w-full rounded border px-2.5 py-2 text-left text-xs transition-colors ${
                        idx === selected
                          ? "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900/70 dark:bg-blue-900/30 dark:text-blue-300"
                          : "border-transparent text-zinc-700 hover:border-zinc-200 hover:bg-zinc-100 dark:text-zinc-200 dark:hover:border-zinc-800 dark:hover:bg-zinc-800"
                      }`}
                      onClick={() => {
                        setSelected(idx);
                      }}
                    >
                      <div className="flex items-start gap-2">
                        <div className="min-w-0 flex-1">
                          <div className="flex items-center gap-1.5">
                            <div className="truncate font-medium">
                              {provider.name || `provider-${idx + 1}`}
                            </div>
                            <InfoTooltip
                              content={getProviderReadinessMessage(
                                duplicated,
                                providerSummary,
                                provider,
                                providerInfoMap.get(
                                  normalizeProviderId(provider.provider),
                                ),
                              )}
                            />
                          </div>
                          <div className="mt-1 truncate text-xs text-zinc-500 dark:text-zinc-400">
                            {(provider.model || "未设置模型") +
                              " · " +
                              formatProviderLabel(
                                provider.provider,
                                providerInfoMap.get(
                                  normalizeProviderId(provider.provider),
                                ),
                              ) +
                              " · " +
                              getProviderReadinessLabel(
                                duplicated,
                                providerSummary,
                                provider,
                                providerInfoMap.get(
                                  normalizeProviderId(provider.provider),
                                ),
                              )}
                          </div>
                        </div>
                        <div className="flex flex-col items-end gap-1">
                          {active && (
                            <StatusBadge tone="success" size="xs">
                              激活
                            </StatusBadge>
                          )}
                          {quick && (
                            <StatusBadge tone="accent" size="xs">
                              快速
                            </StatusBadge>
                          )}
                          {duplicated && (
                            <StatusBadge tone="danger" size="xs">
                              重复
                            </StatusBadge>
                          )}
                          <button
                            type="button"
                            className="mt-1 rounded border border-zinc-200 px-1.5 py-0.5 text-[10px] text-zinc-500 hover:bg-zinc-100 hover:text-zinc-700 dark:border-zinc-700 dark:text-zinc-400 dark:hover:bg-zinc-800 dark:hover:text-zinc-200"
                            onClick={(event) => {
                              event.stopPropagation();
                              setSelected(idx);
                              setDrawerOpen(true);
                            }}
                          >
                            编辑
                          </button>
                        </div>
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          }
          detail={
            !current ? (
              <EmptyStatePanel
                eyebrow="详情工作区"
                title="尚未选择配置"
                description="从左侧 Provider 目录选择一个模型配置，以编辑连接与推理参数。"
              />
            ) : null
          }
        />

        <FullScreenSettingsDrawer
          open={Boolean(current) && drawerOpen}
          title={current ? `编辑模型配置 · ${current.name}` : "编辑模型配置"}
          onClose={() => setDrawerOpen(false)}
        >
          {current ? (
            <div id="models-provider-detail" className="settings-anchor-target space-y-3 text-xs">
              <div className="rounded border border-zinc-200/60 bg-zinc-50 px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-950/60 dark:shadow-zinc-950/30">
                  <div className="flex flex-wrap items-center gap-2">
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-1.5">
                        <div className="text-[13px] font-semibold text-zinc-900 dark:text-zinc-100">
                          基础信息
                        </div>
                        <InfoTooltip
                          content={`名称、Provider、模型与激活状态。${
                            currentProviderInfo
                              ? ` 当前后端适配：${currentProviderInfo.backend_type}。`
                              : ""
                          }`}
                        />
                      </div>
                      <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-zinc-500 dark:text-zinc-400">
                        {currentProviderInfo ? (
                          <span>适配 {currentProviderInfo.backend_type}</span>
                        ) : null}
                        <span>
                          状态{" "}
                          {getProviderReadinessLabel(
                            false,
                            currentCapabilitySummary,
                            current,
                            currentProviderInfo,
                          )}
                        </span>
                      </div>
                    </div>
                    <label className="inline-flex h-7 items-center gap-2 rounded border border-zinc-200/80 bg-white/90 px-3.5 text-[12px] font-medium dark:border-zinc-800/80 dark:bg-zinc-950/70">
                      <input
                        type="radio"
                        checked={data.active === current.name}
                        onChange={() =>
                          setData((prev) => ({ ...prev, active: current.name }))
                        }
                      />
                      设为激活模型
                    </label>
                    <ActionButton
                      onClick={() => void testConnection()}
                      disabled={loading || saving || testing}
                    >
                      {testing ? "测试中..." : "测试连接"}
                    </ActionButton>
                  </div>

                  <div className="mt-2 flex flex-wrap items-center gap-1.5">
                    {currentIsQuickModel ? (
                      <StatusBadge tone="accent" size="xs">
                        当前快速模型
                      </StatusBadge>
                    ) : null}
                    {data.active === current.name ? (
                      <StatusBadge tone="success" size="xs">
                        当前激活模型
                      </StatusBadge>
                    ) : null}
                  </div>

                  {!currentCapabilitySummary.readyForPlan && (
                    <AlertPanel
                      className="mt-2"
                      compact
                      tone="warning"
                      title={
                        isOllamaProvider(current, currentProviderInfo)
                          ? "当前 Provider 使用工具调用适配"
                          : currentCapabilitySummary.supportsPlanExecute
                          ? "当前 Provider 仍需补齐运行条件"
                          : "当前 Provider 不支持 Plan 执行协议"
                      }
                      message={
                        isOllamaProvider(current, currentProviderInfo)
                          ? "Ollama 会尝试把模型输出的 JSON 工具计划转换为真实工具调用；若模型输出不规范，将退回普通回复。"
                          : currentCapabilitySummary.supportsPlanExecute
                          ? currentCapabilitySummary.readinessReason ||
                            "请先补齐 API Key 或连接地址后再切换为激活模型。"
                          : "该 Provider 可继续用于普通聊天，但不会满足 Plan 执行协议。"
                      }
                    />
                  )}

                  <div className="mt-2 grid gap-2.5 md:grid-cols-2 xl:grid-cols-3">
                    <Field label="配置名称">
                      <TextInput
                        value={current.name}
                        onChange={(event) =>
                          updateCurrent((item) => ({
                            ...item,
                            name: event.target.value,
                          }))
                        }
                      />
                    </Field>
                    <Field label="提供商">
                      <SelectInput
                        value={current.provider}
                        onChange={(event) =>
                          updateCurrent((item) => ({
                            ...item,
                            provider: event.target.value,
                          }))
                        }
                      >
                        {providerCatalog.map((provider) => (
                          <option key={provider} value={provider}>
                            {formatProviderLabel(
                              provider,
                              providerInfoMap.get(provider),
                            )}
                          </option>
                        ))}
                      </SelectInput>
                    </Field>
                    <Field label="模型">
                      <TextInput
                        value={current.model}
                        onChange={(event) =>
                          updateCurrent((item) => ({
                            ...item,
                            model: event.target.value,
                          }))
                        }
                      />
                    </Field>
                  </div>
                </div>

                <div className="rounded border border-zinc-200/60 bg-zinc-50 px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-950/60 dark:shadow-zinc-950/30">
                  <div className="mb-2 text-[13px] font-semibold text-zinc-900 dark:text-zinc-100">
                    连接信息
                  </div>
                  <div className="grid gap-2.5 md:grid-cols-2">
                    <Field
                      label="服务地址"
                      description={
                        currentProviderInfo?.default_base_url
                          ? `留空时使用默认地址：${currentProviderInfo.default_base_url}`
                          : "支持填写官方地址、代理地址或兼容网关地址。"
                      }
                    >
                      <TextInput
                        value={current.base_url || ""}
                        placeholder={
                          currentProviderInfo?.default_base_url ||
                          "留空使用默认地址"
                        }
                        onChange={(event) =>
                          updateCurrent((item) => ({
                            ...item,
                            base_url: event.target.value,
                          }))
                        }
                      />
                    </Field>
                    <Field
                      label="API Key"
                      description={
                        currentProviderInfo?.requires_api_key === false
                          ? "当前 Provider 默认不需要 API Key；如接入鉴权代理也可填写。"
                          : "支持显隐查看；清空后保存即可删除当前密钥。"
                      }
                    >
                      <div className="flex gap-2">
                        <TextInput
                          className="flex-1"
                          type={showApiKey ? "text" : "password"}
                          value={current.api_key || ""}
                          onChange={(event) =>
                            updateCurrent((item) => ({
                              ...item,
                              api_key: event.target.value,
                              has_api_key: event.target.value.trim() !== "",
                            }))
                          }
                          autoComplete="off"
                          spellCheck={false}
                          placeholder="输入或粘贴 API Key"
                        />
                        <ActionButton
                          onClick={() => {
                            const key = String(current.api_key || "").trim();
                            if (!key) {
                              toast.error("当前没有可复制的 API Key");
                              return;
                            }
                            void navigator.clipboard.writeText(key).then(
                              () => showActionStatus("API Key 已复制"),
                              () => toast.error("复制失败，请手动复制"),
                            );
                          }}
                          disabled={!String(current.api_key || "").trim()}
                        >
                          复制
                        </ActionButton>
                        <ActionButton
                          onClick={() => setShowApiKey((prev) => !prev)}
                        >
                          {showApiKey ? "隐藏" : "显示"}
                        </ActionButton>
                      </div>
                    </Field>
                  </div>
                </div>

                <div className="rounded border border-zinc-200/60 bg-zinc-50 px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-950/60 dark:shadow-zinc-950/30">
                  <div className="mb-2 text-[13px] font-semibold text-zinc-900 dark:text-zinc-100">
                    推理参数
                  </div>
                  <div className="grid gap-2.5 md:grid-cols-2 xl:grid-cols-4">
                    <CheckboxField
                      label="深度思考模式"
                      description="支持推理预算的模型可启用"
                      checked={Boolean(current.thinking_enabled)}
                      onChange={(next) =>
                        updateCurrent((item) => ({
                          ...item,
                          thinking_enabled: next,
                        }))
                      }
                    />
                    <Field label="思考预算">
                      <NumberInput
                        min={1}
                        value={current.thinking_budget ?? 10240}
                        onChange={(event) =>
                          updateCurrent((item) => ({
                            ...item,
                            thinking_budget: clampInt(
                              Number(event.target.value || 1),
                            ),
                          }))
                        }
                      />
                    </Field>
                    <Field
                      label="最大输出"
                      description="单次请求允许生成的最大 Token 数，须符合模型和接口限制。知识抽取按此值调用模型，并结合上下文容量规划分段。"
                    >
                      <NumberInput
                        min={1}
                        value={current.max_tokens ?? 4096}
                        onChange={(event) =>
                          updateCurrent((item) => ({
                            ...item,
                            max_tokens: clampInt(
                              Number(event.target.value || 1),
                            ),
                          }))
                        }
                      />
                    </Field>
                    <Field label="温度">
                      <NumberInput
                        min={0}
                        max={2}
                        step={0.1}
                        value={current.temperature ?? 0.7}
                        onChange={(event) =>
                          updateCurrent((item) => ({
                            ...item,
                            temperature: Math.max(
                              0,
                              Math.min(2, Number(event.target.value || 0)),
                            ),
                          }))
                        }
                      />
                    </Field>
                  </div>
                </div>

                <div className="rounded border border-zinc-200/60 bg-zinc-50 px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-950/60 dark:shadow-zinc-950/30">
                  <div className="mb-2 text-[13px] font-semibold text-zinc-900 dark:text-zinc-100">
                    上下文容量与压缩
                  </div>
                  <div className="grid gap-2.5 md:grid-cols-2 xl:grid-cols-4">
                    <Field
                      label="上下文 Token"
                      description="填写当前模型真实的总上下文容量，包含输入和输出。知识抽取会扣除提示词及输出预留后规划输入分段；这里只是程序配置，不会扩大模型本身的容量。"
                    >
                      <NumberInput
                        min={1}
                        value={current.max_context_tokens ?? 128000}
                        onChange={(event) =>
                          updateCurrent((item) => ({
                            ...item,
                            max_context_tokens: clampInt(
                              Number(event.target.value || 1),
                            ),
                          }))
                        }
                      />
                    </Field>
                    <Field label="最大使用 (%)">
                      <NumberInput
                        min={1}
                        max={100}
                        value={current.max_context_usage ?? 90}
                        onChange={(event) =>
                          updateCurrent((item) => ({
                            ...item,
                            max_context_usage: Math.max(
                              1,
                              Math.min(
                                100,
                                clampInt(Number(event.target.value || 1)),
                              ),
                            ),
                          }))
                        }
                      />
                    </Field>
                    <Field label="压缩阈值 (%)">
                      <NumberInput
                        min={1}
                        max={100}
                        value={current.auto_compress_threshold ?? 85}
                        onChange={(event) =>
                          updateCurrent((item) => ({
                            ...item,
                            auto_compress_threshold: Math.max(
                              1,
                              Math.min(
                                100,
                                clampInt(Number(event.target.value || 1)),
                              ),
                            ),
                          }))
                        }
                      />
                    </Field>
                    <Field label="压缩策略">
                      <SelectInput
                        value={current.compress_strategy || "summary"}
                        onChange={(event) =>
                          updateCurrent((item) => ({
                            ...item,
                            compress_strategy: event.target.value,
                          }))
                        }
                      >
                        {STRATEGIES.map((strategy) => (
                          <option key={strategy} value={strategy}>
                            {strategy}
                          </option>
                        ))}
                      </SelectInput>
                    </Field>
                  </div>
                </div>
              </div>
          ) : null}
        </FullScreenSettingsDrawer>
      </div>
    </>
  );
}
