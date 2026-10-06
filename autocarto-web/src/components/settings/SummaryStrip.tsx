"use client";

import { cn } from "@/lib/utils";

export interface SummaryItem {
  label: string;
  value: React.ReactNode;
  hint?: React.ReactNode;
  tone?: "default" | "accent" | "success" | "warning" | "danger";
}

interface SummaryStripProps {
  items: SummaryItem[];
  className?: string;
}

function getToneDot(tone: SummaryItem["tone"]) {
  if (tone === "accent") return "bg-blue-500";
  if (tone === "success") return "bg-emerald-500";
  if (tone === "warning") return "bg-amber-500";
  if (tone === "danger") return "bg-red-500";
  return "bg-zinc-400";
}

export default function SummaryStrip({ items, className }: SummaryStripProps) {
  if (items.length === 0) {
    return null;
  }

  return (
    <div
      className={cn(
        "flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-zinc-500 dark:text-zinc-400",
        className,
      )}
    >
      {items.map((item) => (
        <span key={item.label} className="inline-flex items-center gap-1.5">
          <span
            className={cn("inline-block h-1.5 w-1.5 rounded-full", getToneDot(item.tone))}
          />
          <span>{item.label}:</span>
          <span className="font-semibold text-zinc-900 dark:text-zinc-100">
            {item.value}
          </span>
        </span>
      ))}
    </div>
  );
}
