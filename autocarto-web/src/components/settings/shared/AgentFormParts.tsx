import React from "react";
import InfoTooltip from "@/components/settings/InfoTooltip";
import MetricTile from "@/components/settings/MetricTile";

export function countUnique(items: string[]) {
  return new Set(items.map((item) => String(item || "").trim()).filter(Boolean))
    .size;
}

export function countPromptLines(text: string) {
  const value = String(text || "");
  if (!value) {
    return 0;
  }
  return value.split(/\r?\n/).length;
}

export function SummaryMetric({
  label,
  value,
  hint,
}: {
  label: string;
  value: string | number;
  hint?: string;
}) {
  return <MetricTile label={label} value={value} hint={hint} />;
}

export function EditorSection({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="rounded border border-zinc-200 bg-white px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-900">
      <div className="mb-2">
        <div className="flex items-center gap-1.5">
          <div className="text-xs font-semibold uppercase tracking-[0.12em] text-zinc-700 dark:text-zinc-200">
            {title}
          </div>
          {description ? <InfoTooltip content={description} /> : null}
        </div>
      </div>
      {children}
    </div>
  );
}
