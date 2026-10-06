"use client";

import React from "react";
import StatusBadge from "@/components/settings/StatusBadge";
import { cn } from "@/lib/utils";

interface ActionBarProps {
  left?: React.ReactNode;
  right?: React.ReactNode;
  dirty?: boolean;
  status?: {
    label: React.ReactNode;
    tone?: "neutral" | "accent" | "success" | "warning" | "danger";
    evidence_id?: string;
    correlation_id?: string;
  } | null;
  className?: string;
}

interface ActionButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "primary" | "default" | "danger";
}

export const ActionButton = React.forwardRef<HTMLButtonElement, ActionButtonProps>(
  ({ className, variant = "default", type = "button", ...props }, ref) => {
    return (
      <button
        ref={ref}
        type={type}
        className={cn(
          "inline-flex h-8 min-w-[80px] items-center justify-center gap-1.5 rounded-sm border px-3.5 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50",
          variant === "primary" &&
            "border-blue-600 bg-blue-600 text-white hover:bg-blue-700 dark:border-blue-500 dark:bg-blue-500 dark:hover:bg-blue-600",
          variant === "danger" &&
            "border-red-300 bg-red-50 text-red-700 hover:bg-red-100 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300 dark:hover:bg-red-950/60",
          variant === "default" &&
            "border-zinc-300 bg-white text-zinc-700 hover:bg-zinc-100 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-200 dark:hover:bg-zinc-800",
          className,
        )}
        {...props}
      />
    );
  }
);
ActionButton.displayName = "ActionButton";

export default function ActionBar({
  left,
  right,
  dirty,
  status,
  className,
}: ActionBarProps) {
  return (
    <div
      className={cn(
        "settings-action-bar flex flex-wrap items-center justify-between gap-3 py-1.5",
        className,
      )}
    >
      <div className="flex flex-wrap items-center gap-2.5">{left}</div>
      <div className="flex flex-wrap items-center justify-end gap-2.5">
        {status && (
          <span
            className={cn(
              "inline-flex items-center gap-1 text-xs font-medium",
              status.tone === "warning" &&
                "text-amber-700 dark:text-amber-300",
              status.tone === "success" &&
                "text-emerald-700 dark:text-emerald-300",
              status.tone === "danger" && "text-red-700 dark:text-red-300",
              status.tone === "accent" && "text-blue-700 dark:text-blue-300",
              (!status.tone || status.tone === "neutral") &&
                "text-zinc-500 dark:text-zinc-400",
            )}
          >
            <span className="inline-block h-1.5 w-1.5 rounded-full bg-current" />
            <span>{status.label}</span>
            {status.evidence_id && (
              <span className="ml-1 opacity-70" title="Evidence ID">
                [EVIDENCE-{status.evidence_id.replace('EVIDENCE-', '')}]
              </span>
            )}
            {status.correlation_id && (
              <span className="ml-1 opacity-70" title="Correlation ID">
                [{status.correlation_id}]
              </span>
            )}
          </span>
        )}
        {dirty && (
          <StatusBadge tone="warning" className="h-8 px-3 text-xs">
            有未保存的更改
          </StatusBadge>
        )}
        {right}
      </div>
    </div>
  );
}
