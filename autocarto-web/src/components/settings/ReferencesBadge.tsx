"use client";

import { useState } from "react";
import StatusBadge from "@/components/settings/StatusBadge";
import { cn } from "@/lib/utils";

export interface ReferenceItem {
  id: string;
  label: string;
}

interface ReferencesBadgeProps {
  items: ReferenceItem[];
  emptyText?: string;
  className?: string;
}

export default function ReferencesBadge({
  items,
  emptyText = "未被智能体引用",
  className,
}: ReferencesBadgeProps) {
  const [expanded, setExpanded] = useState(false);
  const count = items.length;
  const hasRefs = count > 0;

  return (
    <div className={cn("text-xs", className)}>
      <button
        type="button"
        className={cn(
          "inline-flex h-7 items-center gap-1.5 rounded border px-2.5 transition-colors",
          hasRefs
            ? "border-amber-300 bg-amber-50 text-amber-700 hover:bg-amber-100 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-300"
            : "border-zinc-300 bg-white/80 text-zinc-600 hover:bg-zinc-100 dark:border-zinc-700 dark:bg-zinc-950/70 dark:text-zinc-300 dark:hover:bg-zinc-800",
        )}
        onClick={() => setExpanded((prev) => !prev)}
      >
        <span
          className={cn(
            "h-1.5 w-1.5 rounded-sm",
            hasRefs ? "bg-amber-500" : "bg-zinc-400",
          )}
        />
        <span>{hasRefs ? `被 ${count} 个智能体引用` : emptyText}</span>
        {hasRefs && (
          <StatusBadge
            tone="custom"
            size="xs"
            className="border-current/20 bg-transparent text-current"
          >
            {expanded ? "收起" : "展开"}
          </StatusBadge>
        )}
      </button>
      {expanded && hasRefs && (
        <div className="mt-2 space-y-1 rounded border border-zinc-200/60 bg-zinc-50 px-2.5 py-2 text-zinc-600 dark:border-zinc-800 dark:bg-zinc-950/60 dark:text-zinc-300">
          {items.map((item) => (
            <div
              key={item.id}
              className="rounded border border-zinc-200 bg-white px-2 py-1.5 dark:border-zinc-800 dark:bg-zinc-900/80"
            >
              {item.label}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
