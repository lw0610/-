"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import WorkspaceShell from "@/components/layout/WorkspaceShell";
import { normalizeKindLabel } from "@/components/tool-center/skill-package-utils";
import ActionBar, { ActionButton } from "@/components/settings/ActionBar";
import AlertPanel from "@/components/settings/AlertPanel";
import DetailList from "@/components/settings/DetailList";
import EmptyStatePanel from "@/components/settings/EmptyStatePanel";
import FormSection from "@/components/settings/FormSection";
import PageHeader from "@/components/settings/PageHeader";
import SectionNav from "@/components/settings/SectionNav";
import SummaryStrip from "@/components/settings/SummaryStrip";
import SessionRuntimeSummary from "@/components/runtime/SessionRuntimeSummary";
import api, {
  RuntimeServiceStatus,
  RuntimeSnapshot,
  ToolCatalogContract,
  ToolCatalogItem,
  ToolCatalogResponse,
} from "@/lib/api";
import {
  getRuntimeServiceDegradedDictionary,
  getToolCatalogDegradedDictionary,
  getToolItemDegradedDictionary,
} from "@/lib/degradedDictionary";
import {
  buildRuntimeServiceMeta,
  getRuntimeServiceStatusConfig,
} from "@/lib/serviceStatus";
import { compareSessionRuntimeRecords } from "@/lib/sessionRuntimeDisplay";
import { useChatStore } from "@/stores/chatStore";
import { useConnectionStore } from "@/stores/connectionStore";
import { useRuntimeSnapshotStore } from "@/stores/runtimeSnapshotStore";
import { useSettingsStore } from "@/stores/settingsStore";

function exportConfigJSON(config: unknown) {
  const json = JSON.stringify(config, null, 2);
  const blob = new Blob([json], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = "autocarto-config-export.json";
  anchor.click();
  URL.revokeObjectURL(url);
}

function buildQGISService(snapshot: RuntimeSnapshot) {
  const service = snapshot?.services?.qgis || snapshot?.services?.qgis_bridge;
  if (service) {
    return service;
  }
  return {
    status: "unknown",
    running_count: snapshot?.instances?.running_count,
  } as RuntimeServiceStatus;
}

function normalizeToolCatalog(items: ToolCatalogItem[]) {
  return [...items].sort((left, right) =>
    String(left.name || "").localeCompare(String(right.name || "")),
  );
}

function buildAuthoritativeAgentWsUrl(snapshot: RuntimeSnapshot | null) {
  const port = snapshot?.services?.agent?.port;
  if (typeof port !== "number" || port <= 0) {
    return "-";
  }
  const host =
    String(snapshot?.services?.agent?.host || "127.0.0.1").trim() ||
    "127.0.0.1";
  return `ws://${host}:${port}/ws`;
}

function shouldShowStartupError(startupError: string, backendReady: boolean) {
  if (!startupError.trim()) {
    return false;
  }
  if (!backendReady) {
    return true;
  }
  return startupError.includes("协议版本") || startupError.includes("升级客户端");
}

export default function DiagnosticsWorkspace() {
  const config = useSettingsStore((s) => s.config);
  const llmProviders = useSettingsStore((s) => s.llmProviders);
  const agentConfigs = useSettingsStore((s) => s.agentConfigs);
  const skills = useSettingsStore((s) => s.skills);
  const refreshConfig = useSettingsStore((s) => s.refreshConfig);
  const refreshLLMProviders = useSettingsStore((s) => s.refreshLLMProviders);
  const refreshAgentConfigs = useSettingsStore((s) => s.refreshAgentConfigs);
  const refreshTools = useSettingsStore((s) => s.refreshTools);
  const refreshSkills = useSettingsStore((s) => s.refreshSkills);
  const [toolCatalog, setToolCatalog] = useState<ToolCatalogItem[]>([]);
  const [toolCatalogContract, setToolCatalogContract] = useState<ToolCatalogContract | null>(null);
  const runtimeSnapshot = useRuntimeSnapshotStore((s) => s.snapshot);
  const fetchRuntimeSnapshot = useRuntimeSnapshotStore((s) => s.fetchSnapshot);
  const runtimeLoading = useRuntimeSnapshotStore((s) => s.loading);
  const backendReady = useConnectionStore((s) => s.backendReady);
  const startupError = useConnectionStore((s) => s.startupError);
  const sessionRuntimeStates = useChatStore((s) => s.sessionRuntimeStates);
  const [loading, setLoading] = useState(false);

  const serviceCards = useMemo(() => {
    const qgisBridge = buildQGISService(runtimeSnapshot);
    return [
      {
        name: "智能体系统",
        port: runtimeSnapshot?.services?.agent?.port,
        service: runtimeSnapshot?.services?.agent,
      },
      {
        name: "QGIS 桥接",
        port: qgisBridge?.port,
        service: qgisBridge,
      },
    ];
  }, [runtimeSnapshot]);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const [, catalog] = await Promise.all([
        Promise.all([
          refreshConfig(),
          refreshLLMProviders(),
          refreshAgentConfigs(),
          refreshTools(),
          refreshSkills(),
        ]),
        api.getToolCatalog("global"),
        fetchRuntimeSnapshot(),
      ]);
        const normalizedCatalog = catalog as ToolCatalogResponse;
        const normalizedToolCatalog = normalizeToolCatalog(normalizedCatalog.tool_infos || []);
        setToolCatalog(normalizedToolCatalog);
        setToolCatalogContract(normalizedCatalog.catalog || null);
      } finally {
        setLoading(false);
      }
  }, [
    fetchRuntimeSnapshot,
    refreshAgentConfigs,
    refreshConfig,
    refreshLLMProviders,
    refreshSkills,
    refreshTools,
  ]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const recentSessionRuntimes = useMemo(() => {
    return Object.values(sessionRuntimeStates)
      .slice()
      .sort(compareSessionRuntimeRecords)
      .slice(0, 6);
  }, [sessionRuntimeStates]);

  const healthyServices = serviceCards.filter((item) => {
    const status = String(item.service?.status || "")
      .trim()
      .toLowerCase();
    return status === "running" || status === "ok";
  }).length;

  const diagnosticsSections = [
    { id: "diagnostics-runtime", label: "运行状态", hint: "运行态快照、修订号与健康度" },
    {
      id: "diagnostics-resources",
      label: "配置资源",
      hint: "模型配置、方案与工具包",
    },
    { id: "diagnostics-paths", label: "路径信息", hint: "目录与连接地址" },
    { id: "diagnostics-tools", label: "工具目录", hint: "当前有效工具与可用性" },
  ];

  const pathItems = [
    { label: "运行根目录", value: runtimeSnapshot?.runtime_paths?.runtime_home || "-", mono: true },
    { label: "实际数据目录", value: runtimeSnapshot?.runtime_paths?.data_dir || config?.data_dir || "-", mono: true },
    { label: "当前日志文件", value: runtimeSnapshot?.runtime_paths?.log_file || "-", mono: true },
    {
      label: "Agent WS",
      value: buildAuthoritativeAgentWsUrl(runtimeSnapshot),
      mono: true,
    },
  ];

  const qgisRuntimeService = buildQGISService(runtimeSnapshot);
  const qgisAvailable = ["running", "ok"].includes(
    String(qgisRuntimeService?.status || "")
      .trim()
      .toLowerCase(),
  );
  const qgisInstanceCount = runtimeSnapshot?.instances?.running_count ?? 0;

  return (
    <WorkspaceShell
      actionBar={
        <ActionBar
          left={
            <>
              <ActionButton onClick={() => void refresh()} disabled={loading || runtimeLoading}>
                {loading || runtimeLoading ? "刷新中..." : "刷新状态"}
              </ActionButton>
              <ActionButton
                onClick={() => {
                  if (!config) {
                    return;
                  }
                  exportConfigJSON(config);
                }}
                disabled={!config}
              >
                导出当前配置
              </ActionButton>
            </>
          }
        />
      }
    >
      <PageHeader
        eyebrow="诊断与维护"
        title="运行时诊断"
        description="查看运行时快照、服务健康、工具目录与关键路径。"
      />

      {shouldShowStartupError(startupError, backendReady) ? (
        <AlertPanel
          tone="error"
          title="协议版本不兼容"
          message={startupError}
        />
      ) : null}

      <SummaryStrip
        items={[
          {
            label: "服务健康度",
            value: `${healthyServices}/${serviceCards.length}`,
            tone: healthyServices === serviceCards.length ? "success" : "warning",
          },
          {
            label: "QGIS 环境",
            value: qgisAvailable ? "可用" : "未检测到",
            hint: `实例数量 ${qgisInstanceCount}`,
            tone: qgisAvailable ? "accent" : "warning",
          },
          {
            label: "配置资源",
            value: `${llmProviders?.providers?.length ?? 0} / ${agentConfigs.length}`,
            hint: "模型配置、方案与工具包",
            tone: "accent",
          },
          {
            label: "当前有效工具",
            value: `${toolCatalog.length}`,
          },
        ]}
      />

      <SectionNav items={diagnosticsSections} />

      <div className="space-y-3">
        <div id="diagnostics-runtime" className="settings-anchor-target">
          <FormSection
            title="运行状态"
            description="核心运行时、QGIS 桥接与当前工具能力总览。"
          >
            <DetailList
              columns={2}
              items={[
                {
                  label: "Runtime Revision",
                  value: runtimeSnapshot?.runtime_revision || "-",
                },
                {
                  label: "Tool Schema Rev",
                  value: runtimeSnapshot?.tool_schema_revision || "-",
                },
                {
                  label: "Availability Rev",
                  value: runtimeSnapshot?.tool_availability_revision || "-",
                },
                {
                  label: "Active Config",
                  value: runtimeSnapshot?.runtime_active_agent_config || "-",
                },
              ]}
            />

            {runtimeSnapshot?.last_rebuild_reason || runtimeSnapshot?.planner_ready_reason ? (
              <AlertPanel
                className="mt-2"
                compact
                tone={runtimeSnapshot?.planner_ready_reason ? "warning" : "info"}
                title={
                  runtimeSnapshot?.planner_ready_reason
                    ? "Plan 运行态待处理"
                    : "最近重建信息"
                }
                message={
                  runtimeSnapshot?.planner_ready_reason
                    ? `${runtimeSnapshot.planner_ready_reason}${runtimeSnapshot?.last_rebuild_reason ? `；最近重建原因：${runtimeSnapshot.last_rebuild_reason}` : ""}`
                    : `最近重建原因：${runtimeSnapshot?.last_rebuild_reason || "-"}${runtimeSnapshot?.last_rebuild_at ? `；时间：${runtimeSnapshot.last_rebuild_at}` : ""}`
                }
              />
            ) : null}

            {recentSessionRuntimes.length > 0 ? (
              <div className="mt-2 rounded border border-zinc-200/80 bg-zinc-50/80 px-3 py-2.5 dark:border-zinc-800/80 dark:bg-zinc-900/40">
                <div className="mb-2 text-xs font-semibold text-zinc-900 dark:text-zinc-100">
                  会话运行态
                </div>
                <div className="space-y-2">
                  {recentSessionRuntimes.map((item) => (
                    <div
                      key={item.session_id}
                      className="rounded border border-zinc-200/80 bg-white/80 px-3 py-2 text-xs text-zinc-600 dark:border-zinc-800/80 dark:bg-zinc-950/60 dark:text-zinc-300"
                    >
                      <SessionRuntimeSummary
                        runtime={item}
                        showSessionId
                        badgeSize="xs"
                        chipClassName="bg-white/80 dark:bg-zinc-950/70"
                      />
                    </div>
                  ))}
                </div>
              </div>
            ) : null}

            <div className="mt-2 space-y-2">
              {serviceCards.map((item) => {
                const statusConfig = getRuntimeServiceStatusConfig(
                  item.service?.status || "unknown",
                );
                const meta = buildRuntimeServiceMeta(item.service).join(" · ");
                const diagnostics = item.service?.diagnostics;
                const degraded = getRuntimeServiceDegradedDictionary(item.service);
                const diagnosticsMessage = degraded?.message;
                const hasDiagnosticsEvidence = Boolean(degraded?.evidence_id || diagnostics?.evidence_id);

                return (
                  <div
                    key={item.name}
                    className="rounded border border-zinc-200/80 bg-zinc-50/80 px-3 py-2.5 dark:border-zinc-800/80 dark:bg-zinc-900/40"
                  >
                    <div className="flex flex-wrap items-start justify-between gap-2">
                      <div className="min-w-0">
                        <div className="text-xs font-semibold text-zinc-900 dark:text-zinc-100">
                          {item.name}
                        </div>
                        <div className="mt-0.5 text-[11px] leading-4 text-zinc-500 dark:text-zinc-400">
                          {meta || "等待服务上报更多信息"}
                        </div>
                      </div>
                      <span
                        className={`rounded-sm px-2 py-0.5 text-[11px] font-medium ${statusConfig.badgeClassName}`}
                      >
                        {statusConfig.label}
                      </span>
                    </div>
                    <div className="mt-1.5 flex flex-wrap items-center gap-x-4 gap-y-1 text-[12px] text-zinc-600 dark:text-zinc-300">
                      <span className="inline-flex items-center gap-1">
                        <span>Port</span>
                        <span className="font-semibold text-zinc-900 dark:text-zinc-100">
                          {item.port || "-"}
                        </span>
                      </span>
                    </div>
                    {diagnosticsMessage ? (
                      <AlertPanel
                        className="mt-2"
                        compact
                        tone="error"
                        title={degraded?.code || (item.service?.last_error ? "最近错误" : "启动失败")}
                        message={
                          <div className="space-y-1">
                            <div>{diagnosticsMessage}</div>
                            {degraded?.user_action ? <div>{degraded.user_action}</div> : null}
                          </div>
                        }
                        evidence_id={degraded?.evidence_id || diagnostics?.evidence_id}
                        correlation_id={diagnostics?.correlation_id}
                      />
                    ) : null}
                    {hasDiagnosticsEvidence && !diagnosticsMessage ? (
                      <div className="mt-1.5 flex flex-wrap gap-2 text-[10px] font-mono text-zinc-500 dark:text-zinc-400">
                        <div>
                          EVIDENCE-{diagnostics?.evidence_id?.replace("EVIDENCE-", "")}
                        </div>
                        {diagnostics?.correlation_id ? (
                          <div>Correlation: {diagnostics.correlation_id}</div>
                        ) : null}
                      </div>
                    ) : null}
                    {hasDiagnosticsEvidence ? (
                      <div
                        className="mt-1.5 flex flex-wrap gap-2 text-[10px] font-mono text-zinc-500 dark:text-zinc-400"
                        data-testid="diagnostic-evidence"
                      >
                        <div>Evidence: {diagnostics?.evidence_id}</div>
                        {diagnostics?.correlation_id ? (
                          <div>Correlation: {diagnostics.correlation_id}</div>
                        ) : null}
                      </div>
                    ) : null}
                  </div>
                );
              })}
            </div>
          </FormSection>
        </div>

        <div id="diagnostics-resources" className="settings-anchor-target">
          <FormSection
            title="配置资源"
            description="设置域的核心配置数量与组织规模。"
          >
            <DetailList
              columns={2}
              items={[
                { label: "LLM Provider", value: llmProviders?.providers?.length ?? 0 },
                { label: "智能体方案", value: agentConfigs.length },
                { label: "Skills", value: skills.length },
              ]}
            />
          </FormSection>
        </div>

        <div id="diagnostics-paths" className="settings-anchor-target">
          <FormSection
            title="路径信息"
            description="后端报告的实际目录、当前日志文件与连接地址。"
          >
            <DetailList items={pathItems} />
            {runtimeSnapshot?.runtime_paths?.warnings?.map(warning => (
              <AlertPanel key={warning.code} className="mt-2" tone="warning" title="运行目录提示" message={warning.message} />
            ))}
          </FormSection>
        </div>

        <div id="diagnostics-tools" className="settings-anchor-target">
          <FormSection
            title="工具目录"
            description="当前有效工具与可用性快照。"
          >
            {toolCatalog.length === 0 && getToolCatalogDegradedDictionary(toolCatalogContract) ? (
              <AlertPanel
                className="mb-2"
                compact
                tone="warning"
                title={getToolCatalogDegradedDictionary(toolCatalogContract)?.code}
                message={
                  <div className="space-y-1">
                    <div>{getToolCatalogDegradedDictionary(toolCatalogContract)?.message}</div>
                    <div>{getToolCatalogDegradedDictionary(toolCatalogContract)?.user_action}</div>
                  </div>
                }
              />
            ) : null}
            {toolCatalog.length > 0 ? (
              <div className="space-y-2">
                {toolCatalog.map((item) => (
                  <div
                    key={item.name}
                    className="rounded border border-zinc-200/80 bg-zinc-50/80 px-3 py-2.5 dark:border-zinc-800/80 dark:bg-zinc-900/40"
                  >
                    <div className="flex flex-wrap items-start justify-between gap-2">
                      <div className="min-w-0">
                        <div className="text-xs font-semibold text-zinc-900 dark:text-zinc-100">
                          {item.name}
                        </div>
                        <div className="mt-0.5 text-[11px] leading-4 text-zinc-500 dark:text-zinc-400">
                          {item.description || "未提供描述"}
                        </div>
                      </div>
                      <span
                        className={`rounded-sm px-2 py-0.5 text-[11px] font-medium ${
                          item.available === false
                            ? "bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
                            : "bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300"
                        }`}
                      >
                        {item.available === false ? "待运行态装载" : "可用"}
                      </span>
                    </div>
                    <div className="mt-1.5 flex flex-wrap items-center gap-x-4 gap-y-1 text-[12px] text-zinc-600 dark:text-zinc-300">
                      <span className="inline-flex items-center gap-1">
                        <span>来源</span>
                        <span className="font-semibold text-zinc-900 dark:text-zinc-100">
                          {item.source || "-"}
                        </span>
                      </span>
                      <span className="inline-flex items-center gap-1">
                        <span>后端</span>
                        <span className="font-semibold text-zinc-900 dark:text-zinc-100">
                          {item.backend || "-"}
                        </span>
                      </span>
                      <span className="inline-flex items-center gap-1">
                        <span>分类</span>
                        <span className="font-semibold text-zinc-900 dark:text-zinc-100">
                          {item.category || "-"}
                        </span>
                      </span>
                      <span className="inline-flex items-center gap-1">
                        <span>工具包</span>
                        <span className="font-semibold text-zinc-900 dark:text-zinc-100">
                          {item.package_id || "-"}
                        </span>
                      </span>
                      <span className="inline-flex items-center gap-1">
                        <span>包类型</span>
                        <span className="font-semibold text-zinc-900 dark:text-zinc-100">
                          {item.kind ? normalizeKindLabel(item.kind) : "-"}
                        </span>
                      </span>
                      <span className="inline-flex items-center gap-1">
                        <span>包状态</span>
                        <span className="font-semibold text-zinc-900 dark:text-zinc-100">
                          {typeof item.enabled === "boolean"
                            ? item.enabled
                              ? "启用"
                              : "停用"
                            : "-"}
                        </span>
                      </span>
                    </div>
                    {item.available === false && getToolItemDegradedDictionary(item) ? (
                      <AlertPanel
                        className="mt-2"
                        compact
                        tone="warning"
                        title={getToolItemDegradedDictionary(item)?.code || "不可用原因"}
                        message={
                          <div className="space-y-1">
                            <div>{getToolItemDegradedDictionary(item)?.message || item.missing_reason}</div>
                            {getToolItemDegradedDictionary(item)?.user_action ? (
                              <div>{getToolItemDegradedDictionary(item)?.user_action}</div>
                            ) : null}
                            {typeof getToolItemDegradedDictionary(item)?.retryable === "boolean" ? (
                              <div>{getToolItemDegradedDictionary(item)?.retryable ? "可重试" : "需人工处理"}</div>
                            ) : null}
                          </div>
                        }
                        evidence_id={getToolItemDegradedDictionary(item)?.evidence_id}
                      />
                    ) : null}
                  </div>
                ))}
              </div>
            ) : (
              <EmptyStatePanel
                compact
                eyebrow="工具目录"
                title="尚未上报有效工具"
                description="等待运行态同步工具目录后，这里会显示当前有效工具和可用性快照。"
              />
            )}
          </FormSection>
        </div>
      </div>
    </WorkspaceShell>
  );
}
