"use client";

import { useMemo, useState } from "react";
import AlertPanel from "@/components/settings/AlertPanel";
import EmptyStatePanel from "@/components/settings/EmptyStatePanel";
import ListPanelHeader from "@/components/settings/ListPanelHeader";
import StatusBadge from "@/components/settings/StatusBadge";
import { ToolCatalogContract, ToolCatalogItem } from "@/lib/api";
import {
  getToolCatalogDegradedDictionary,
  getToolItemDegradedDictionary,
} from "@/lib/degradedDictionary";

const FILTER_SELECT_CLASS =
  "h-8 rounded-sm border border-zinc-300 bg-zinc-100 px-2.5 text-xs text-zinc-800 outline-none dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100";

interface ToolCatalogViewProps {
  catalog?: ToolCatalogContract | null;
  items: ToolCatalogItem[];
  loading: boolean;
}

export default function ToolCatalogView({
  catalog,
  items,
  loading,
}: ToolCatalogViewProps) {
  const [query, setQuery] = useState("");
  const [runtimeFilter, setRuntimeFilter] = useState("all");
  const [domainFilter, setDomainFilter] = useState("all");
  const [availabilityFilter, setAvailabilityFilter] = useState("all");

  const runtimeOptions = useMemo(() => {
    return Array.from(
      new Set(items.map((item) => String(item.backend || "").trim()).filter(Boolean)),
    ).sort();
  }, [items]);

  const domainOptions = useMemo(() => {
    return Array.from(
      new Set(
        items
          .map((item) => String(item.domain || item.category || "").trim())
          .filter(Boolean),
      ),
    ).sort();
  }, [items]);

  const filteredItems = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase();
    return items.filter((item) => {
      if (
        normalizedQuery &&
        ![
          item.name,
          item.description,
          item.domain,
          item.category,
          item.backend,
          item.source,
          item.package_id,
          item.missing_reason,
        ]
          .join("\n")
          .toLowerCase()
          .includes(normalizedQuery)
      ) {
        return false;
      }
      if (runtimeFilter !== "all" && String(item.backend || "") !== runtimeFilter) {
        return false;
      }
      if (
        domainFilter !== "all" &&
        String(item.domain || item.category || "") !== domainFilter
      ) {
        return false;
      }
      if (availabilityFilter === "available" && item.available === false) {
        return false;
      }
      if (availabilityFilter === "unavailable" && item.available !== false) {
        return false;
      }
      return true;
    });
  }, [availabilityFilter, domainFilter, items, query, runtimeFilter]);

  const catalogDegraded = getToolCatalogDegradedDictionary(catalog);

  return (
    <>
      <ListPanelHeader
        eyebrow="工具目录"
        title="统一工具目录"
        description="查看当前运行时可发现的扁平工具列表与可用性状态。"
        stats={[
          {
            label: "结果",
            value: `${filteredItems.length}/${items.length}`,
          },
        ]}
      >
        <div className="space-y-2">
          <input
            className="h-8 w-full rounded-sm border border-zinc-300 bg-zinc-100 px-2.5 text-xs text-zinc-800 outline-none transition-colors placeholder:text-zinc-400 focus:border-blue-500 focus:bg-white focus:ring-2 focus:ring-blue-500/10 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="搜索工具..."
          />
          <div className="grid gap-2 md:grid-cols-3 xl:grid-cols-3">
            <select
              className={FILTER_SELECT_CLASS}
              value={runtimeFilter}
              onChange={(event) => setRuntimeFilter(event.target.value)}
            >
              <option value="all">全部运行时</option>
              {runtimeOptions.map((item) => (
                <option key={item} value={item}>
                  {item}
                </option>
              ))}
            </select>
            <select
              className={FILTER_SELECT_CLASS}
              value={domainFilter}
              onChange={(event) => setDomainFilter(event.target.value)}
            >
              <option value="all">全部域</option>
              {domainOptions.map((item) => (
                <option key={item} value={item}>
                  {item}
                </option>
              ))}
            </select>
            <select
              className={FILTER_SELECT_CLASS}
              value={availabilityFilter}
              onChange={(event) => setAvailabilityFilter(event.target.value)}
            >
              <option value="all">全部状态</option>
              <option value="available">仅可用</option>
              <option value="unavailable">仅不可用</option>
            </select>
          </div>
        </div>
      </ListPanelHeader>

      {filteredItems.length === 0 && catalogDegraded ? (
        <AlertPanel
          className="mb-2"
          compact
          tone="warning"
          title={catalogDegraded.code}
          message={
            <div className="space-y-1">
              <div>{catalogDegraded.message}</div>
              <div>{catalogDegraded.user_action}</div>
              <div>{catalogDegraded.retryable ? '可重试' : '需人工处理'}</div>
            </div>
          }
        />
      ) : null}

      <div className="max-h-[620px] space-y-1 overflow-auto">
        {filteredItems.length === 0 && !loading ? (
          <EmptyStatePanel
            compact
            eyebrow="空列表"
            title="没有匹配的工具"
            description="调整筛选条件后再试。"
          />
        ) : null}

        {filteredItems.map((item) => {
          const available = item.available !== false;
          const degraded = getToolItemDegradedDictionary(item);
          return (
            <div
              key={`${item.name}-${item.package_id || item.backend || "tool"}`}
              className="rounded border border-zinc-200/70 bg-zinc-50/80 px-3 py-3 text-left text-sm dark:border-zinc-800/70 dark:bg-zinc-900/50 dark:text-zinc-200"
            >
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <div className="truncate font-medium text-base">{item.name}</div>
                    <StatusBadge size="xs">{item.backend || "-"}</StatusBadge>
                    {item.domain || item.category ? (
                      <StatusBadge size="xs">{item.domain || item.category}</StatusBadge>
                    ) : null}
                    <StatusBadge size="xs" tone={available ? "success" : "warning"}>
                      {available ? "可用" : "不可用"}
                    </StatusBadge>
                  </div>
                  <div className="mt-1.5 text-xs text-zinc-500 dark:text-zinc-400">
                    来源 {item.source || "-"} · 包 {item.package_id || "-"} · 实例{" "}
                    {(item.instance_ids || []).length}
                  </div>
                  {item.description ? (
                    <div className="mt-1.5 line-clamp-2 text-xs text-zinc-500 dark:text-zinc-400">
                      {item.description}
                    </div>
                  ) : null}
                  {!available && degraded ? (
                    <AlertPanel
                      className="mt-1.5"
                      compact
                      tone="warning"
                      title={degraded.code}
                      message={
                        <div className="space-y-1">
                          <div>{degraded.message || item.missing_reason}</div>
                          {degraded.user_action ? <div>{degraded.user_action}</div> : null}
                          {typeof degraded.retryable === 'boolean' ? (
                            <div>{degraded.retryable ? '可重试' : '需人工处理'}</div>
                          ) : null}
                        </div>
                      }
                      evidence_id={degraded.evidence_id}
                      correlation_id={item.correlation_id}
                    />
                  ) : null}
                  {!degraded && (item.evidence_id || item.correlation_id) ? (
                    <div className="mt-1.5 flex flex-wrap gap-2 text-[10px] font-mono text-zinc-500 dark:text-zinc-400">
                      {item.evidence_id ? (
                        <div>EVIDENCE-{item.evidence_id.replace("EVIDENCE-", "")}</div>
                      ) : null}
                      {item.correlation_id ? (
                        <div>Correlation: {item.correlation_id}</div>
                      ) : null}
                    </div>
                  ) : null}
                  {(item.instance_ids || []).length > 0 ? (
                    <div className="mt-1 text-xs text-zinc-500 dark:text-zinc-400">
                      实例：{(item.instance_ids || []).join("、")}
                    </div>
                  ) : null}
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </>
  );
}
