"use client";

import React from "react";
import InfoTooltip from "@/components/settings/InfoTooltip";
import { cn } from "@/lib/utils";

type MetricSurface = "soft" | "contrast";
type MetricAlign = "left" | "right";

interface MetricTileProps {
  label: string;
  value: React.ReactNode;
  hint?: React.ReactNode;
  className?: string;
  compact?: boolean;
  surface?: MetricSurface;
  align?: MetricAlign;
}

const surfaceClassMap: Record<MetricSurface, string> = {
  soft: "border-zinc-200/80 bg-zinc-50/80 dark:border-zinc-800/80 dark:bg-zinc-900/40",
  contrast:
    "border-zinc-200/80 bg-white/80 dark:border-zinc-800/80 dark:bg-zinc-950/60",
};

export default function MetricTile({
  label,
  value,
  hint,
  className,
  compact = false,
  surface = "contrast",
  align = "left",
}: MetricTileProps) {
  return (
    <div
      className={cn(
        "rounded border",
        compact ? "px-2 py-1.5" : "px-3 py-2",
        align === "right" ? "text-right" : "text-left",
        surfaceClassMap[surface],
        className,
      )}
    >
      <div className="flex items-center gap-1 text-[11px] text-zinc-500 dark:text-zinc-400">
        <span>{label}</span>
        {hint ? <InfoTooltip content={hint} /> : null}
      </div>
      <div
        className={cn(
          compact ? "mt-0.5 text-[11px]" : "mt-0.5 text-xs",
          "font-semibold text-zinc-900 dark:text-zinc-100",
        )}
      >
        {value}
      </div>
    </div>
  );
}
