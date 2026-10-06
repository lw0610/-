"use client";

import InfoTooltip from "@/components/settings/InfoTooltip";
import { cn } from "@/lib/utils";

interface FormSectionProps {
  title: string;
  description?: string;
  className?: string;
  children: React.ReactNode;
}

export default function FormSection({
  title,
  description,
  className,
  children,
}: FormSectionProps) {
  return (
    <section
      className={cn(
        "rounded border border-zinc-200 bg-white px-4 py-3 dark:border-zinc-800 dark:bg-zinc-900",
        className,
      )}
    >
      <div className="flex items-center gap-1.5">
        <h3 className="text-[13px] font-semibold text-zinc-900 dark:text-zinc-100">
          {title}
        </h3>
        {description ? <InfoTooltip content={description} /> : null}
      </div>
      <div className="mt-2.5">{children}</div>
    </section>
  );
}
