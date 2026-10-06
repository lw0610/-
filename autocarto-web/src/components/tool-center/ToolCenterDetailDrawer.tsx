"use client";

import { useMemo, useState } from "react";
import { createPortal } from "react-dom";
import MarkdownContent from "@/components/chat/MarkdownContent";
import ActionBar, { ActionButton } from "@/components/settings/ActionBar";
import EmptyStatePanel from "@/components/settings/EmptyStatePanel";
import InfoTooltip from "@/components/settings/InfoTooltip";
import ReferencesBadge from "@/components/settings/ReferencesBadge";
import StatusBadge from "@/components/settings/StatusBadge";
import { SkillPackageDetail } from "@/lib/api";
import { normalizeKindLabel, normalizeScopeLabel } from "./skill-package-utils";

function getDisplayedToolCount(detail: { tool_ref_count?: number; tool_count?: number }) {
  if (typeof detail.tool_ref_count === "number") {
    return detail.tool_ref_count;
  }
  return typeof detail.tool_count === "number" ? detail.tool_count : 0;
}

type DetailTab = "overview" | "markdown" | "tools" | "references" | "config";

interface ToolCenterDetailDrawerProps {
  open: boolean;
  detail: SkillPackageDetail | null;
  loading: boolean;
  toggling: boolean;
  onClose: () => void;
  onToggle: () => void;
}

function toolNameOf(tool: Record<string, unknown>) {
  const metadata =
    tool.metadata && typeof tool.metadata === "object"
      ? (tool.metadata as Record<string, unknown>)
      : {};
  return String(metadata.name || tool.name || "").trim();
}

function toolDescriptionOf(tool: Record<string, unknown>) {
  const metadata =
    tool.metadata && typeof tool.metadata === "object"
      ? (tool.metadata as Record<string, unknown>)
      : {};
  return String(metadata.description || tool.description || "").trim();
}

export default function ToolCenterDetailDrawer({
  open,
  detail,
  loading,
  toggling,
  onClose,
  onToggle,
}: ToolCenterDetailDrawerProps) {
  const [activeTab, setActiveTab] = useState<DetailTab>("overview");

  const referenceItems = useMemo(() => {
    return (detail?.references || []).map((item, index) => ({
      id: `${item.config_name}-${item.agent_name}-${item.layer}-${index}`,
      label: `${item.config_name}/${item.agent_name}/${item.layer}`,
    }));
  }, [detail]);

  if (!open || typeof document === "undefined") {
    return null;
  }

  return createPortal(
    <div className="fixed inset-0 z-[70]">
      <div
        className="absolute inset-0 bg-black/35"
        onClick={onClose}
      />
      <div
        id="tool-center-detail-drawer"
        className="absolute inset-y-0 right-0 w-full max-w-[800px] overflow-auto border-l border-zinc-200 bg-zinc-50 p-4 shadow-[0_2px_8px_rgba(0,0,0,0.08)] dark:border-zinc-800 dark:bg-zinc-950"
      >
        <div className="space-y-4">
          <ActionBar
            left={
              <ActionButton onClick={onClose}>
                关闭
              </ActionButton>
            }
            right={
              detail ? (
                <ActionButton
                  variant={detail.enabled === false ? "primary" : "danger"}
                  disabled={toggling || loading}
                  onClick={onToggle}
                >
                  {toggling
                    ? "处理中..."
                    : detail.enabled === false
                      ? "启用技能包"
                      : "停用技能包"}
                </ActionButton>
              ) : null
            }
          />

          {!detail ? (
            <EmptyStatePanel
              title={loading ? "加载中..." : "技能包不存在"}
              description={loading ? "正在获取详情" : "请返回重试"}
            />
          ) : (
            <div id="tool-center-detail" className="space-y-3">
              <div className="rounded border border-zinc-200/60 bg-zinc-50 px-4 py-3 dark:border-zinc-800 dark:bg-zinc-950/60 dark:shadow-zinc-950/30">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex items-center gap-1.5">
                      <div className="text-sm font-semibold text-zinc-900 dark:text-zinc-100">
                        {detail.name || detail.package_id}
                      </div>
                      <StatusBadge size="xs">{normalizeKindLabel(String(detail.kind || ""))}</StatusBadge>
                      <InfoTooltip content="统一展示技能包 front matter、Markdown 主体、工具定义、引用关系和启停状态。" />
                    </div>
                    <div className="mt-1 text-xs text-zinc-500 dark:text-zinc-400">
                      {detail.package_id} · 工具 {getDisplayedToolCount(detail)} · 引用 {referenceItems.length}
                    </div>
                    {detail.description ? (
                      <div className="mt-2 text-sm text-zinc-600 dark:text-zinc-300">
                        {detail.description}
                      </div>
                    ) : null}
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <StatusBadge tone={detail.enabled ? "success" : "warning"}>
                      {detail.enabled ? "已启用" : "已停用"}
                    </StatusBadge>
                    <StatusBadge>{normalizeScopeLabel(String(detail.scope || ""))}</StatusBadge>
                    {detail.owner_config ? <StatusBadge>{detail.owner_config}</StatusBadge> : null}
                    {detail.backend ? <StatusBadge>{detail.backend}</StatusBadge> : null}
                  </div>
                </div>
              </div>

              <div className="flex flex-wrap gap-2">
                {([
                  ["overview", "概览"],
                  ["markdown", "Markdown"],
                  ["tools", "工具"],
                  ["references", "引用"],
                  ["config", "配置"],
                ] as Array<[DetailTab, string]>).map(([key, label]) => (
                  <button
                    key={key}
                    type="button"
                    className={`rounded border px-4 py-1.5 text-sm transition-colors ${
                      activeTab === key
                        ? "border-blue-500 bg-blue-50 text-blue-700 dark:border-blue-600 dark:bg-blue-900/30 dark:text-blue-300"
                        : "border-zinc-300 bg-white text-zinc-600 hover:bg-zinc-100 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300 dark:hover:bg-zinc-800"
                    }`}
                    onClick={() => setActiveTab(key)}
                  >
                    {label}
                  </button>
                ))}
              </div>

              {activeTab === "overview" ? (
                <div className="grid gap-3 md:grid-cols-2">
                  <div className="rounded border border-zinc-200/60 bg-zinc-50 px-4 py-3 text-sm dark:border-zinc-800 dark:bg-zinc-950/60">
                    <div className="mb-2 font-semibold text-zinc-900 dark:text-zinc-100">基础信息</div>
                    <div className="space-y-1.5 text-zinc-600 dark:text-zinc-300">
                      <div><span className="text-zinc-400">名称：</span>{detail.name || detail.package_id}</div>
                      <div><span className="text-zinc-400">类型：</span>{normalizeKindLabel(String(detail.kind || ""))}</div>
                      <div><span className="text-zinc-400">范围：</span>{normalizeScopeLabel(String(detail.scope || ""))}</div>
                      <div><span className="text-zinc-400">归属方案：</span>{detail.owner_config || "全局"}</div>
                      <div><span className="text-zinc-400">后端：</span>{detail.backend || "未指定"}</div>
                      <div><span className="text-zinc-400">来源：</span>{detail.source || "未指定"}</div>
                      <div><span className="text-zinc-400">标签：</span>{(detail.tags || []).join("、") || "无"}</div>
                      <div><span className="text-zinc-400">依赖工具：</span>{(detail.requires_tools || []).join("、") || "无"}</div>
                    </div>
                  </div>
                  <div className="rounded border border-zinc-200/60 bg-zinc-50 px-4 py-3 text-sm dark:border-zinc-800 dark:bg-zinc-950/60">
                    <div className="mb-2 font-semibold text-zinc-900 dark:text-zinc-100">引用与状态</div>
                    <ReferencesBadge items={referenceItems} emptyText="当前无引用" />
                  </div>
                </div>
              ) : null}

              {activeTab === "markdown" ? (
                <div className="rounded border border-zinc-200/60 bg-zinc-50 px-4 py-3 dark:border-zinc-800 dark:bg-zinc-950/60">
                  {loading ? (
                    <div className="text-sm text-zinc-500 dark:text-zinc-400">Markdown 加载中...</div>
                  ) : detail.markdown_body ? (
                    <MarkdownContent content={detail.markdown_body} />
                  ) : (
                    <EmptyStatePanel compact eyebrow="Markdown" title="暂无正文" description="当前技能包没有可展示的 Markdown 正文。" />
                  )}
                </div>
              ) : null}

              {activeTab === "tools" ? (
                <div className="rounded border border-zinc-200/60 bg-zinc-50 px-4 py-3 dark:border-zinc-800 dark:bg-zinc-950/60">
                  {detail.tools.length === 0 ? (
                    <EmptyStatePanel compact eyebrow="工具列表" title="当前无工具" description="该技能包当前没有内嵌工具定义。" />
                  ) : (
                    <div className="grid gap-2 sm:grid-cols-2">
                      {detail.tools.map((tool, index) => {
                        const rawTool = tool as Record<string, unknown>;
                        return (
                          <div key={`${toolNameOf(rawTool)}-${index}`} className="flex flex-col rounded border border-zinc-200 bg-white px-3 py-2 text-xs dark:border-zinc-800 dark:bg-zinc-900">
                            <div className="font-medium text-zinc-900 dark:text-zinc-100 truncate" title={toolNameOf(rawTool)}>
                              {toolNameOf(rawTool) || `tool-${index + 1}`}
                            </div>
                            {toolDescriptionOf(rawTool) ? (
                              <div className="mt-1 line-clamp-2 text-zinc-500 dark:text-zinc-400" title={toolDescriptionOf(rawTool)}>
                                {toolDescriptionOf(rawTool)}
                              </div>
                            ) : null}
                          </div>
                        );
                      })}
                    </div>
                  )}
                </div>
              ) : null}

              {activeTab === "references" ? (
                <div className="rounded border border-zinc-200/60 bg-zinc-50 px-4 py-3 dark:border-zinc-800 dark:bg-zinc-950/60">
                  {detail.references.length === 0 ? (
                    <EmptyStatePanel compact eyebrow="引用关系" title="当前无引用" description="尚未被任何方案或智能体引用。" />
                  ) : (
                    <div className="space-y-2 text-sm">
                      {detail.references.map((item, index) => (
                        <div key={`${item.config_name}-${item.agent_name}-${index}`} className="rounded border border-zinc-200 bg-white px-4 py-3 dark:border-zinc-800 dark:bg-zinc-900">
                          <div className="font-medium text-zinc-900 dark:text-zinc-100">
                            {item.config_name}/{item.agent_name}
                          </div>
                          <div className="mt-1 text-zinc-500 dark:text-zinc-400">
                            层级：{item.layer} · 来源：{item.source}
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              ) : null}

              {activeTab === "config" ? (
                <div className="rounded border border-zinc-200/60 bg-zinc-50 px-4 py-3 dark:border-zinc-800 dark:bg-zinc-950/60">
                  <pre className="overflow-auto rounded border border-zinc-200 bg-white p-3 text-sm text-zinc-700 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-200">
                    {JSON.stringify(detail.config || {}, null, 2)}
                  </pre>
                </div>
              ) : null}
            </div>
          )}
        </div>
      </div>
    </div>,
    document.body,
  );
}
