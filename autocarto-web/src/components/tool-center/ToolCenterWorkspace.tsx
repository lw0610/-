"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import ActionBar, { ActionButton } from "@/components/settings/ActionBar";
import AlertPanel from "@/components/settings/AlertPanel";
import EmptyStatePanel from "@/components/settings/EmptyStatePanel";
import ListPanelHeader from "@/components/settings/ListPanelHeader";
import StatusBadge from "@/components/settings/StatusBadge";
import ToolCenterDetailDrawer from "./ToolCenterDetailDrawer";
import ToolCatalogView from "./ToolCatalogView";
import { useSettingsActionStatus } from "@/hooks/useSettingsActionStatus";
import api, {
  describeSettingsMutation,
  SkillPackageDetail,
  SkillPackageScope,
  SkillPackageSummary,
  ToolCatalogContract,
  ToolCatalogItem,
  ToolCatalogResponse,
} from "@/lib/api";
import {
  normalizeKindLabel,
  normalizeScopeLabel,
  packageKey,
} from "./skill-package-utils";

const FILTER_SELECT_CLASS =
  "h-8 rounded-sm border border-zinc-300 bg-zinc-100 px-2.5 text-xs text-zinc-800 outline-none dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100";

function getDisplayedToolCount(item: { tool_ref_count?: number; tool_count?: number }) {
  if (typeof item.tool_ref_count === "number") {
    return item.tool_ref_count;
  }
  return typeof item.tool_count === "number" ? item.tool_count : 0;
}

export default function ToolCenterWorkspace() {
  const { actionStatus, publishActionStatus } = useSettingsActionStatus();
  const [viewTab, setViewTab] = useState<"packages" | "catalog">("packages");
  const [packages, setPackages] = useState<SkillPackageSummary[]>([]);
  const [toolCatalog, setToolCatalog] = useState<ToolCatalogItem[]>([]);
  const [toolCatalogContract, setToolCatalogContract] = useState<ToolCatalogContract | null>(null);
  const [selectedKey, setSelectedKey] = useState("");
  const selectedKeyRef = useRef(selectedKey);
  selectedKeyRef.current = selectedKey;
  const [detail, setDetail] = useState<SkillPackageDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [detailLoading, setDetailLoading] = useState(false);
  const [catalogLoading, setCatalogLoading] = useState(false);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [scopeFilter, setScopeFilter] = useState<SkillPackageScope>("all");
  const [backendFilter, setBackendFilter] = useState<string>("all");
  const [sourceFilter, setSourceFilter] = useState<string>("all");
  const [enabledFilter, setEnabledFilter] = useState<string>("all");
  const [toggling, setToggling] = useState(false);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const packageRequestRef = useRef(0);
  const detailRequestRef = useRef(0);
  const catalogRequestRef = useRef(0);
  const catalogRequestedRef = useRef(false);

  const loadDetail = useCallback(
    async (
      item?: {
        package_id?: string;
        scope?: string;
        owner_config?: string;
      } | null,
    ) => {
      const request = ++detailRequestRef.current;
      const packageId = String(item?.package_id || "").trim();
      if (!packageId) {
        setDetail(null);
        setDetailLoading(false);
        return;
      }
      setDetail(null);
      setDetailLoading(true);
      try {
        const nextDetail = await api.getSkillPackage(
          packageId,
          String(item?.scope || "").trim() as SkillPackageScope | undefined,
          String(item?.owner_config || "").trim() || undefined,
        );
        if (request === detailRequestRef.current) setDetail(nextDetail);
      } catch (loadError) {
        if (request === detailRequestRef.current) {
          setError(loadError instanceof Error ? loadError.message : "加载工具包详情失败");
        }
      } finally {
        if (request === detailRequestRef.current) setDetailLoading(false);
      }
    },
    [],
  );

  const loadPackages = useCallback(async () => {
    const request = ++packageRequestRef.current;
    setLoading(true);
    setError("");
    try {
      const nextPackages = (await api.getSkillPackages(scopeFilter)).filter((item) => item.kind === "category" && !item.deprecated);
      if (request !== packageRequestRef.current) return;
      setPackages(nextPackages);
      const target =
        nextPackages.find(
          (item) => packageKey(item) === selectedKeyRef.current,
        ) ||
        nextPackages[0] ||
        null;
      selectedKeyRef.current = packageKey(target);
      setSelectedKey(selectedKeyRef.current);
      if (target) {
        await loadDetail(target);
      } else {
        await loadDetail(null);
      }
    } catch (loadError) {
      if (request === packageRequestRef.current) setError(
        loadError instanceof Error ? loadError.message : "加载工具包失败",
      );
    } finally {
      if (request === packageRequestRef.current) setLoading(false);
    }
  }, [loadDetail, scopeFilter]);

  const loadToolCatalog = useCallback(async () => {
    const request = ++catalogRequestRef.current;
    catalogRequestedRef.current = true;
    setCatalogLoading(true);
    setError("");
    try {
      const globalCatalog = await api.getToolCatalog("global") as ToolCatalogResponse;
      if (request !== catalogRequestRef.current) return;
      setToolCatalog(globalCatalog.tool_infos || []);
      setToolCatalogContract(globalCatalog.catalog || null);
    } catch (loadError) {
      if (request === catalogRequestRef.current) setError(
        loadError instanceof Error ? loadError.message : "加载工具目录失败",
      );
    } finally {
      if (request === catalogRequestRef.current) setCatalogLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadPackages();
  }, [loadPackages]);

  useEffect(() => {
    if (viewTab === "catalog") {
      void loadToolCatalog();
    }
  }, [loadToolCatalog, viewTab]);

  useEffect(() => { if (drawerOpen && !catalogRequestedRef.current) void loadToolCatalog(); },[drawerOpen,loadToolCatalog]);

  useEffect(() => {
    const refreshCatalogs = () => {
      void loadPackages();
      if (catalogRequestedRef.current) void loadToolCatalog();
    };
    window.addEventListener("autocarto:skill_catalog_changed", refreshCatalogs);
    return () => window.removeEventListener("autocarto:skill_catalog_changed", refreshCatalogs);
  }, [loadPackages, loadToolCatalog]);

  const backendOptions = useMemo(() => {
    return Array.from(
      new Set(
        packages
          .map((item) => String(item.backend || "").trim())
          .filter(Boolean),
      ),
    ).sort();
  }, [packages]);

  const sourceOptions = useMemo(() => {
    return Array.from(
      new Set(
        packages
          .map((item) => String(item.source || "").trim())
          .filter(Boolean),
      ),
    ).sort();
  }, [packages]);

  const filteredPackages = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase();
    return packages.filter((item) => {
      if (
        normalizedQuery &&
        ![
          item.package_id,
          item.name, item.display_name, item.domain,
          item.description,
          item.kind,
          item.scope,
          item.owner_config,
          ...(item.tags || []),
        ]
          .join("\n")
          .toLowerCase()
          .includes(normalizedQuery)
      ) {
        return false;
      }
      if (
        backendFilter !== "all" &&
        String(item.backend || "") !== backendFilter
      ) {
        return false;
      }
      if (
        sourceFilter !== "all" &&
        String(item.source || "") !== sourceFilter
      ) {
        return false;
      }
      if (enabledFilter === "enabled" && item.enabled === false) {
        return false;
      }
      if (enabledFilter === "disabled" && item.enabled !== false) {
        return false;
      }
      return true;
    });
  }, [backendFilter, enabledFilter, packages, query, sourceFilter]);

  const currentDetail =
    detail && packageKey(detail) === selectedKey ? detail : null;

  const referenceItems = useMemo(() => {
    return (currentDetail?.references || []).map((item, index) => ({
      id: `${item.config_name}-${item.agent_name}-${item.layer}-${index}`,
      label: `${item.config_name}/${item.agent_name}/${item.layer}`,
    }));
  }, [currentDetail]);

  const toggleCurrentPackage = async () => {
    if (!currentDetail) {
      return;
    }
    setToggling(true);
    setError("");
    try {
      const result = await api.toggleSkillPackage(
        currentDetail.package_id,
        !currentDetail.enabled,
        currentDetail.scope as SkillPackageScope | undefined,
        currentDetail.owner_config,
      );
      const mutationFeedback = describeSettingsMutation(
        result.mutation,
        `${currentDetail.name || currentDetail.package_id} 已${currentDetail.enabled ? "停用" : "启用"}`,
      );
      publishActionStatus(
        mutationFeedback.message,
        mutationFeedback.warning,
        false,
        result.mutation?.evidence_id,
        result.mutation?.correlation_id,
      );
      await loadPackages();
    } catch (toggleError) {
      setError(
        toggleError instanceof Error ? toggleError.message : "切换工具包失败",
      );
    } finally {
      setToggling(false);
    }
  };

  return (
    <div className="space-y-3">
      <ActionBar
        status={actionStatus}
        left={
          <>
            <ActionButton
              onClick={() =>
                void (viewTab === "catalog" ? loadToolCatalog() : loadPackages())
              }
              disabled={loading || catalogLoading || toggling}
            >
              {loading || catalogLoading ? "加载中..." : "刷新"}
            </ActionButton>
            <span className="text-xs text-zinc-500 dark:text-zinc-400">
              {viewTab === "catalog"
                ? `工具 ${toolCatalog.length} 个`
                : `工具包 ${packages.length} 个`}
            </span>
          </>
        }
        right={
          <>
            {viewTab === "packages" ? (
              <ActionButton
                variant={currentDetail?.enabled === false ? "primary" : "danger"}
                disabled={!currentDetail || currentDetail.system || toggling || detailLoading}
                onClick={() => void toggleCurrentPackage()}
              >
                {toggling
                  ? "处理中..."
                  : currentDetail?.enabled === false
                    ? "启用工具包"
                    : "停用工具包"}
              </ActionButton>
            ) : null}
          </>
        }
      />

      {error && (
        <AlertPanel tone="error" title="工具中心操作失败" message={error} />
      )}

      <div id="tool-center-list" className="space-y-2">
        <div className="flex flex-wrap gap-2">
          {([
            ["packages", "工具包"],
            ["catalog", "能力手册"],
          ] as const).map(([key, label]) => (
            <button
              key={key}
              type="button"
              className={`rounded border px-4 py-1.5 text-sm transition-colors ${
                viewTab === key
                  ? "border-blue-500 bg-blue-50 text-blue-700 dark:border-blue-600 dark:bg-blue-900/30 dark:text-blue-300"
                  : "border-zinc-300 bg-white text-zinc-600 hover:bg-zinc-100 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300 dark:hover:bg-zinc-800"
              }`}
              onClick={() => setViewTab(key)}
            >
              {label}
            </button>
          ))}
        </div>

        {viewTab === "catalog" ? (
          <ToolCatalogView
            items={toolCatalog}
            loading={catalogLoading}
            catalog={toolCatalogContract}
            packages={packages}
          />
        ) : (
          <>
            <ListPanelHeader
              eyebrow="工具包目录"
              title="工具中心"
              description="查看工具包的能力、使用说明与参考资料。"
              stats={[
                {
                  label: "结果",
                  value: `${filteredPackages.length}/${packages.length}`,
                },
              ]}
            >
              <div className="space-y-2">
                <input
                  className="h-8 w-full rounded-sm border border-zinc-300 bg-zinc-100 px-2.5 text-xs text-zinc-800 outline-none transition-colors placeholder:text-zinc-400 focus:border-blue-500 focus:bg-white focus:ring-2 focus:ring-blue-500/10 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  placeholder="搜索工具包..."
                />
                <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-5">
                  <select
                    aria-label="工具包范围"
                    className={FILTER_SELECT_CLASS}
                    value={scopeFilter}
                    onChange={(event) =>
                      setScopeFilter(event.target.value as SkillPackageScope)
                    }
                  >
                    <option value="all">全部</option>
                    <option value="builtin">仅内置</option>
                    <option value="user">仅用户</option>
                  </select>
                  <select
                    aria-label="工具后端"
                    className={FILTER_SELECT_CLASS}
                    value={backendFilter}
                    onChange={(event) => setBackendFilter(event.target.value)}
                  >
                    <option value="all">全部后端</option>
                    {backendOptions.map((item) => (
                      <option key={item} value={item}>
                        {item}
                      </option>
                    ))}
                  </select>
                  <select
                    aria-label="工具来源"
                    className={FILTER_SELECT_CLASS}
                    value={sourceFilter}
                    onChange={(event) => setSourceFilter(event.target.value)}
                  >
                    <option value="all">全部来源</option>
                    {sourceOptions.map((item) => (
                      <option key={item} value={item}>
                        {item}
                      </option>
                    ))}
                  </select>
                  <select
                    aria-label="启用状态"
                    className={FILTER_SELECT_CLASS}
                    value={enabledFilter}
                    onChange={(event) => setEnabledFilter(event.target.value)}
                  >
                    <option value="all">全部状态</option>
                    <option value="enabled">仅启用</option>
                    <option value="disabled">仅停用</option>
                  </select>
                </div>
              </div>
            </ListPanelHeader>

            <div className="max-h-[620px] space-y-1 overflow-auto">
              {filteredPackages.length === 0 && !loading ? (
                <EmptyStatePanel
                  compact
                  eyebrow="空列表"
                  title="没有匹配的工具包"
                  description="调整筛选条件后再试。"
                />
              ) : null}

              {[false,true].map((system) => {
                const items = filteredPackages.filter((item) => Boolean(item.system || item.hidden) === system);
                if (!items.length) return null;
                const rows = <>{items.map((item) => {
                const itemKey = packageKey(item);
                const active = itemKey === selectedKey;
                return (
                  <button
                    key={itemKey}
                    type="button"
                    className={`w-full rounded border px-3 py-3 text-left text-sm transition-colors ${
                      active
                        ? "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900/70 dark:bg-blue-900/30 dark:text-blue-300"
                        : "border-zinc-200/70 bg-zinc-50/80 text-zinc-700 hover:border-zinc-300 hover:bg-zinc-100 dark:border-zinc-800/70 dark:bg-zinc-900/50 dark:text-zinc-200 dark:hover:border-zinc-700 dark:hover:bg-zinc-800"
                    }`}
                    onClick={() => {
                      selectedKeyRef.current = itemKey;
                      setSelectedKey(itemKey);
                      setDrawerOpen(true);
                      void loadDetail(item);
                    }}
                  >
                    <div className="flex items-start justify-between gap-3">
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <div className="truncate font-medium text-base">
                            {item.display_name || item.name || item.package_id}
                          </div>
                          <StatusBadge size="xs">
                            {normalizeKindLabel(String(item.kind || ""))}
                          </StatusBadge>
                          <StatusBadge size="xs">
                            {normalizeScopeLabel(String(item.scope || ""))}
                          </StatusBadge>
                          {item.scope === "exclusive" && item.owner_config ? (
                            <StatusBadge size="xs">{item.owner_config}</StatusBadge>
                          ) : null}
                          {item.enabled === false ? (
                            <StatusBadge size="xs" tone="warning">
                              停用
                            </StatusBadge>
                          ) : null}
                        </div>
                        <div className="mt-1.5 text-xs text-zinc-500 dark:text-zinc-400">
                          工具 {getDisplayedToolCount(item)} · 标签 {(item.tags || []).length} ·
                          来源 {item.source || "-"} · 引用{" "}
                          {itemKey === packageKey(currentDetail)
                            ? referenceItems.length
                            : "-"}
                        </div>
                        {item.scope === "exclusive" && item.owner_config ? (
                          <div className="mt-1 text-xs text-zinc-500 dark:text-zinc-400">
                            归属方案 {item.owner_config}
                          </div>
                        ) : null}
                        {item.description ? (
                          <div className="mt-1.5 line-clamp-2 text-xs text-zinc-500 dark:text-zinc-400">
                            {item.description}
                          </div>
                        ) : null}
                      </div>
                    </div>
                  </button>
                );
              })}</>;
                return system ? <details key="system"><summary className="cursor-pointer py-2 text-sm">系统（{items.length}）</summary>{rows}</details> : <div key="selectable" className="space-y-1">{rows}</div>;
              })}
            </div>
          </>
        )}
      </div>

      <ToolCenterDetailDrawer
        open={viewTab === "packages" && drawerOpen}
        detail={currentDetail}
        catalog={toolCatalog}
        loading={detailLoading}
        toggling={toggling}
        onClose={() => setDrawerOpen(false)}
        onToggle={() => void toggleCurrentPackage()}
      />
    </div>
  );
}
