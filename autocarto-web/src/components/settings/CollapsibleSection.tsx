"use client";

import React, { useCallback, useEffect, useState } from "react";
import InfoTooltip from "@/components/settings/InfoTooltip";
import { useSettingsViewport } from "@/components/settings/SettingsViewportContext";
import { cn } from "@/lib/utils";

interface CollapsibleSectionProps {
  title: string;
  description?: string;
  defaultOpen?: boolean;
  open?: boolean;
  onOpenChange?: (next: boolean) => void;
  sectionId?: string;
  className?: string;
  children: React.ReactNode;
}

export default function CollapsibleSection({
  title,
  description,
  defaultOpen = false,
  open,
  onOpenChange,
  sectionId,
  className,
  children,
}: CollapsibleSectionProps) {
  const { registerSection } = useSettingsViewport();
  const [internalOpen, setInternalOpen] = useState(defaultOpen);
  const resolvedOpen = open ?? internalOpen;
  const setResolvedOpen = useCallback(
    (next: boolean) => {
      if (open === undefined) {
        setInternalOpen(next);
      }
      onOpenChange?.(next);
    },
    [onOpenChange, open],
  );

  useEffect(() => {
    if (!sectionId) {
      return undefined;
    }
    return registerSection(sectionId, {
      expand: () => setResolvedOpen(true),
    });
  }, [registerSection, sectionId, setResolvedOpen]);

  return (
    <section
      className={cn(
        "rounded border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900",
        className,
      )}
    >
      <button
        type="button"
        className="flex w-full items-center justify-between gap-2 px-3 py-2 text-left"
        onClick={() => setResolvedOpen(!resolvedOpen)}
      >
        <div className="flex min-w-0 items-center gap-1.5">
          <h3 className="text-[11px] font-semibold text-zinc-900 dark:text-zinc-100">
            {title}
          </h3>
          {description ? <InfoTooltip content={description} /> : null}
        </div>
        <svg
          viewBox="0 0 16 16"
          className={cn(
            "h-3 w-3 shrink-0 text-zinc-400 transition-transform dark:text-zinc-500",
            resolvedOpen && "rotate-90",
          )}
          fill="none"
          aria-hidden="true"
        >
          <path
            d="m6 3 5 5-5 5"
            stroke="currentColor"
            strokeWidth="1.5"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>
      </button>

      {resolvedOpen && (
        <div className="border-t border-zinc-200 px-3 py-2 dark:border-zinc-800">
          {children}
        </div>
      )}
    </section>
  );
}
