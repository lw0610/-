"use client";

import { useState } from "react";
import InfoTooltip from "@/components/settings/InfoTooltip";
import { cn } from "@/lib/utils";

interface AdvancedSectionProps {
  title?: string;
  description?: string;
  defaultOpen?: boolean;
  className?: string;
  children: React.ReactNode;
}

export default function AdvancedSection({
  title = "高级选项",
  description,
  defaultOpen = false,
  className,
  children,
}: AdvancedSectionProps) {
  const [open, setOpen] = useState(defaultOpen);

  return (
    <div
      className={cn(
        "rounded border border-zinc-200/60 bg-zinc-50 dark:border-zinc-800 dark:bg-zinc-950/60 dark:shadow-zinc-950/30",
        className,
      )}
    >
      <button
        type="button"
        className="flex w-full items-center justify-between gap-3 px-3 py-2.5 text-left"
        onClick={() => setOpen((value) => !value)}
      >
        <div>
          <div className="flex items-center gap-1.5">
            <div className="text-xs font-semibold uppercase tracking-[0.12em] text-zinc-700 dark:text-zinc-200">
              {title}
            </div>
            {description ? <InfoTooltip content={description} /> : null}
          </div>
        </div>
        <span className="text-[11px] font-medium uppercase tracking-[0.14em] text-zinc-500 dark:text-zinc-400">
          {open ? "收起" : "展开"}
        </span>
      </button>
      {open && (
        <div className="border-t border-zinc-200/60 px-3 py-2.5 dark:border-zinc-800">
          {children}
        </div>
      )}
    </div>
  );
}
