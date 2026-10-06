"use client";

import React from "react";
import InfoTooltip from "@/components/settings/InfoTooltip";
import { formatInlineHeaderStats } from "@/lib/settingsWorkbench";
import { cn } from "@/lib/utils";

interface HeaderStat {
  label: string;
  value: React.ReactNode;
}

interface ListPanelHeaderProps {
  title: string;
  description?: React.ReactNode;
  eyebrow?: string;
  stats?: HeaderStat[];
  className?: string;
  children?: React.ReactNode;
}

export default function ListPanelHeader({
  title,
  description,
  eyebrow = "列表面板",
  stats = [],
  className,
  children,
}: ListPanelHeaderProps) {
  const compactStats = formatInlineHeaderStats(stats);

  return (
    <div
      className={cn(
        "rounded border border-zinc-200 bg-zinc-50 px-3 py-2 dark:border-zinc-800 dark:bg-zinc-950/55",
        className,
      )}
    >
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <div className="text-[10px] font-medium uppercase tracking-[0.16em] text-zinc-400 dark:text-zinc-500">
            {eyebrow}
          </div>
          <div className="mt-0.5 flex items-center gap-1.5">
            <div className="text-[11px] font-semibold leading-5 text-zinc-900 dark:text-zinc-100">
              {title}
            </div>
            {description ? <InfoTooltip content={description} /> : null}
          </div>
        </div>
        {compactStats.length > 0 && (
          <dl className="flex shrink-0 flex-wrap items-center justify-end gap-x-2 gap-y-1 text-[11px] text-zinc-500 dark:text-zinc-400">
            {compactStats.map((stat) => (
              <div
                key={stat}
                className="inline-flex items-center gap-1 rounded-sm border border-zinc-200 bg-white px-2 py-0.5 dark:border-zinc-800 dark:bg-zinc-900/80"
              >
                <dd className="font-medium text-zinc-900 dark:text-zinc-100">{stat}</dd>
              </div>
            ))}
          </dl>
        )}
      </div>
      {children && <div className="mt-2 space-y-1.5">{children}</div>}
    </div>
  );
}
