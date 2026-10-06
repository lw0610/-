"use client";

import InfoTooltip from "@/components/settings/InfoTooltip";
import RuntimeImpactBadge from "@/components/settings/RuntimeImpactBadge";
import type { RuntimeImpact } from "@/lib/settingsRegistry";
import { cn } from "@/lib/utils";

interface SettingSectionProps {
  id?: string;
  title: string;
  description?: string;
  impact?: RuntimeImpact;
  rightSlot?: React.ReactNode;
  className?: string;
  children: React.ReactNode;
}

export default function SettingSection({
  id,
  title,
  description,
  impact,
  rightSlot,
  className,
  children,
}: SettingSectionProps) {
  return (
    <section
      id={id}
      className={cn(
        "rounded border border-zinc-200 bg-white px-3 py-2 dark:border-zinc-800 dark:bg-zinc-900",
        className,
      )}
    >
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-[11px] font-semibold text-zinc-900 dark:text-zinc-100">
              {title}
            </h3>
            {description ? <InfoTooltip content={description} /> : null}
            {impact && <RuntimeImpactBadge impact={impact} />}
          </div>
        </div>
        {rightSlot}
      </div>
      <div className="mt-1.5">{children}</div>
    </section>
  );
}
