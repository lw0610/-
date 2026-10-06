"use client";

import StatusBadge from "@/components/settings/StatusBadge";
import { cn } from "@/lib/utils";

export interface TabBarItem {
  key: string;
  label: string;
  badge?: string;
  hint?: string;
}

interface TabBarProps {
  tabs: TabBarItem[];
  activeKey: string;
  onChange: (key: string) => void;
  className?: string;
}

export default function TabBar({
  tabs,
  activeKey,
  onChange,
  className,
}: TabBarProps) {
  return (
    <div
      className={cn(
        "flex flex-wrap items-stretch gap-1 rounded border border-zinc-200/80 bg-zinc-50/80 p-1 dark:border-zinc-800/80 dark:bg-zinc-900/60",
        className,
      )}
    >
      {tabs.map((tab) => {
        const active = tab.key === activeKey;
        return (
          <button
            key={tab.key}
            type="button"
            className={cn(
              "flex min-w-[120px] flex-1 items-center justify-between gap-2 rounded px-2.5 py-1.5 text-left transition-colors",
              active
                ? "bg-blue-600 text-white dark:bg-blue-500"
                : "text-zinc-600 hover:bg-zinc-100 dark:text-zinc-300 dark:hover:bg-zinc-800",
            )}
            onClick={() => onChange(tab.key)}
          >
            <div className="min-w-0">
              <div className="text-xs font-medium">{tab.label}</div>
              {tab.hint && (
                <div
                  className={cn(
                    "mt-0.5 truncate text-[11px]",
                    active
                      ? "text-blue-100/90"
                      : "text-zinc-500 dark:text-zinc-400",
                  )}
                >
                  {tab.hint}
                </div>
              )}
            </div>
            {tab.badge && (
              <StatusBadge
                tone="custom"
                className={cn(
                  "shrink-0",
                  active
                    ? "border-white/25 bg-white/10 text-white"
                    : "border-zinc-200 bg-white/80 text-zinc-600 dark:border-zinc-700 dark:bg-zinc-950/70 dark:text-zinc-300",
                )}
              >
                {tab.badge}
              </StatusBadge>
            )}
          </button>
        );
      })}
    </div>
  );
}
