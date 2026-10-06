"use client";

import React from "react";
import { cn } from "@/lib/utils";

interface EmptyStatePanelProps {
  title: string;
  description: React.ReactNode;
  eyebrow?: string;
  className?: string;
  compact?: boolean;
}

export default function EmptyStatePanel({
  title,
  description,
  eyebrow = "空状态",
  className,
  compact = false,
}: EmptyStatePanelProps) {
  return (
    <div
      className={cn(
        "rounded border border-dashed border-zinc-300/80 bg-zinc-50/70 text-left dark:border-zinc-700/80 dark:bg-zinc-900/35",
        compact ? "px-3 py-2.5" : "px-3.5 py-2",
        className,
      )}
    >
      <div className="text-[10px] font-semibold uppercase tracking-[0.16em] text-zinc-500 dark:text-zinc-400">
        {eyebrow}
      </div>
      <div className="mt-1 text-xs font-semibold text-zinc-900 dark:text-zinc-100">
        {title}
      </div>
      <div className="mt-1 text-[11px] leading-4 text-zinc-500 dark:text-zinc-400">
        {description}
      </div>
    </div>
  );
}
