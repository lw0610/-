"use client";

import React from "react";
import InfoTooltip from "@/components/settings/InfoTooltip";

import { cn } from "@/lib/utils";

export interface DetailListItem {
  label: string;
  value: React.ReactNode;
  hint?: React.ReactNode;
  mono?: boolean;
}

interface DetailListProps {
  items: DetailListItem[];
  columns?: 1 | 2;
  className?: string;
}

export default function DetailList({
  items,
  columns = 1,
  className,
}: DetailListProps) {
  if (items.length === 0) {
    return null;
  }

  return (
    <dl
      className={cn(
        "grid gap-2",
        columns === 2 && "sm:grid-cols-2",
        className,
      )}
    >
      {items.map((item) => (
        <div
          key={item.label}
          className="rounded border border-zinc-200/80 bg-white/80 px-3 py-2 dark:border-zinc-800/80 dark:bg-zinc-950/60"
        >
          <dt className="flex items-center gap-1 text-[11px] text-zinc-500 dark:text-zinc-400">
            <span>{item.label}</span>
            {item.hint ? <InfoTooltip content={item.hint} /> : null}
          </dt>
          <dd
            className={cn(
              "mt-0.5 text-xs font-medium text-zinc-900 dark:text-zinc-100",
              item.mono && "break-all font-mono text-[12px]",
            )}
          >
            {item.value}
          </dd>
        </div>
      ))}
    </dl>
  );
}
