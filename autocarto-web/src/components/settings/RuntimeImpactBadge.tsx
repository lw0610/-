"use client";

import { cn } from "@/lib/utils";
import type { RuntimeImpact } from "@/lib/settingsRegistry";

const IMPACT_META: Record<
  RuntimeImpact,
  { label: string; className: string }
> = {
  instant: {
    label: "立即生效",
    className:
      "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/70 dark:bg-emerald-950/30 dark:text-emerald-300",
  },
  runtime_rebuild: {
    label: "触发运行时重建",
    className:
      "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900/70 dark:bg-blue-950/30 dark:text-blue-300",
  },
  desktop_restart: {
    label: "需重启桌面端",
    className:
      "border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-900/70 dark:bg-amber-950/30 dark:text-amber-300",
  },
  read_only: {
    label: "只读",
    className:
      "border-zinc-200 bg-zinc-50 text-zinc-600 dark:border-zinc-800 dark:bg-zinc-900/60 dark:text-zinc-300",
  },
};

interface RuntimeImpactBadgeProps {
  impact: RuntimeImpact;
  className?: string;
}

export default function RuntimeImpactBadge({
  impact,
  className,
}: RuntimeImpactBadgeProps) {
  const meta = IMPACT_META[impact];
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-sm border px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.12em]",
        meta.className,
        className,
      )}
    >
      {meta.label}
    </span>
  );
}
