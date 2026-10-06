"use client";

import { useEffect } from "react";
import AppShell from "@/components/layout/AppShell";
import WorkspaceShell from "@/components/layout/WorkspaceShell";
import ActionBar, { ActionButton } from "@/components/settings/ActionBar";
import SettingSection from "@/components/settings/SettingSection";
import { buildTopBarServiceStatusItems, getAggregateStatus } from "@/lib/serviceStatus";
import { useConnectionStore } from "@/stores/connectionStore";
import { useRuntimeSnapshotStore } from "@/stores/runtimeSnapshotStore";

export default function ServicesSettingsPage() {
  const connected = useConnectionStore((s) => s.connected);
  const snapshot = useRuntimeSnapshotStore((s) => s.snapshot);
  const loading = useRuntimeSnapshotStore((s) => s.loading);
  const fetchSnapshot = useRuntimeSnapshotStore((s) => s.fetchSnapshot);

  useEffect(() => {
    void fetchSnapshot().catch(() => undefined);
  }, [fetchSnapshot]);

  const items = buildTopBarServiceStatusItems({
    snapshot,
    websocketConnected: connected,
    projectConnected: Boolean(snapshot.active_instance_id),
    projectStatusText: snapshot.active_instance_id ? "项目已连接" : "等待项目连接",
  });
  const aggregate = getAggregateStatus(items);
  const runtimeItems = items.filter((item) => item.kind === "service");
  const connectionItems = items.filter((item) => item.kind === "connection");

  return (
    <AppShell>
      <WorkspaceShell
        actionBar={
          <ActionBar
            left={
              <ActionButton onClick={() => void fetchSnapshot()} disabled={loading}>
                {loading ? "刷新中..." : "刷新状态"}
              </ActionButton>
            }
          />
        }
      >
        <div className="space-y-3">
          <div className="rounded border border-zinc-200 bg-zinc-50 px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-950/55">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <div className="text-[13px] font-semibold text-zinc-900 dark:text-zinc-100">服务状态</div>
                <div className="mt-1 text-xs text-zinc-500 dark:text-zinc-400">查看运行态服务、桥接连接和当前项目实例健康度。</div>
              </div>
              <span className={`rounded px-2 py-1 text-[11px] ${aggregate.badgeClassName}`}>{aggregate.label}</span>
            </div>
          </div>

          <SettingSection
            title="运行态服务"
            description="智能体系统、QGIS 桥接与服务健康度。"
            impact="read_only"
          >
            <div className="space-y-2">
              {runtimeItems.map((item) => (
                <div key={item.id} className="rounded border border-zinc-200 bg-white px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-900/70">
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex min-w-0 items-center gap-2">
                      <span className={`h-2.5 w-2.5 shrink-0 rounded-full ${item.dotClassName}`} />
                      <span className="truncate text-sm font-medium text-zinc-900 dark:text-zinc-100">{item.label}</span>
                    </div>
                    <span className={`shrink-0 rounded px-1.5 py-0.5 text-[10px] ${item.badgeClassName}`}>{item.statusLabel}</span>
                  </div>
                  {item.meta ? <div className="mt-1 text-xs text-zinc-500 dark:text-zinc-400">{item.meta}</div> : null}
                  {item.lastError ? <div className="mt-1 text-xs text-red-600 dark:text-red-300">{item.lastError}</div> : null}
                </div>
              ))}
            </div>
          </SettingSection>

          <SettingSection title="工作区连接" description="WebSocket 与当前项目实例绑定状态。">
            <div className="space-y-2">
              {connectionItems.map((item) => (
                <div key={item.id} className="rounded border border-zinc-200 bg-white px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-900/70">
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex min-w-0 items-center gap-2">
                      <span className={`h-2.5 w-2.5 shrink-0 rounded-full ${item.dotClassName}`} />
                      <span className="truncate text-sm font-medium text-zinc-900 dark:text-zinc-100">{item.label}</span>
                    </div>
                    <span className={`shrink-0 rounded px-1.5 py-0.5 text-[10px] ${item.badgeClassName}`}>{item.statusLabel}</span>
                  </div>
                  {item.meta ? <div className="mt-1 text-xs text-zinc-500 dark:text-zinc-400">{item.meta}</div> : null}
                </div>
              ))}
            </div>
          </SettingSection>
        </div>
      </WorkspaceShell>
    </AppShell>
  );
}
