"use client";

import React from "react";
import { cn } from "@/lib/utils";

type StatusTone =
  | "neutral"
  | "accent"
  | "success"
  | "warning"
  | "danger"
  | "custom";

type StatusSize = "xs" | "sm";

interface StatusBadgeProps {
  children: React.ReactNode;
  tone?: StatusTone;
  size?: StatusSize;
  className?: string;
}

const toneClassMap: Record<StatusTone, string> = {
  neutral:
    "border-zinc-200 bg-zinc-50 text-zinc-600 dark:border-zinc-800 dark:bg-zinc-900/70 dark:text-zinc-300",
  accent:
    "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900/70 dark:bg-blue-950/40 dark:text-blue-300",
  success:
    "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/70 dark:bg-emerald-950/40 dark:text-emerald-300",
  warning:
    "border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-900/70 dark:bg-amber-950/40 dark:text-amber-300",
  danger:
    "border-red-200 bg-red-50 text-red-700 dark:border-red-900/70 dark:bg-red-950/40 dark:text-red-300",
  custom: "",
};

const sizeClassMap: Record<StatusSize, string> = {
  xs: "px-1.5 py-0.5 text-[10px]",
  sm: "px-2.5 py-1 text-[11px]",
};

export default function StatusBadge({
  children,
  tone = "neutral",
  size = "sm",
  className,
}: StatusBadgeProps) {
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-sm border font-medium leading-none tracking-[0.02em]",
        sizeClassMap[size],
        toneClassMap[tone],
        className,
      )}
    >
      {children}
    </span>
  );
}
