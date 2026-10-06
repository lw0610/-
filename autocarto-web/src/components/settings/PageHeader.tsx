"use client";

import React from "react";
import InfoTooltip from "@/components/settings/InfoTooltip";
import { cn } from "@/lib/utils";

interface PageHeaderProps {
  title: string;
  description?: string;
  className?: string;
  rightSlot?: React.ReactNode;
  eyebrow?: string;
}

export default function PageHeader({
  title,
  description,
  className,
  rightSlot,
  eyebrow = "设置控制台",
}: PageHeaderProps) {
  return (
    <div className={cn("px-0 py-0", className)}>
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <div className="text-[10px] font-medium uppercase tracking-[0.18em] text-zinc-400 dark:text-zinc-500">
            {eyebrow}
          </div>
          <div className="mt-0.5 flex items-center gap-1.5">
            <h2 className="text-[13px] font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">
              {title}
            </h2>
            {description ? <InfoTooltip content={description} /> : null}
          </div>
        </div>
        {rightSlot}
      </div>
    </div>
  );
}
