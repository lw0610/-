"use client";

import { cn } from "@/lib/utils";

type AlertTone = "info" | "success" | "warning" | "error";

interface AlertPanelProps {
  tone?: AlertTone;
  title?: string;
  message: React.ReactNode;
  evidence_id?: string;
  correlation_id?: string;
  className?: string;
  compact?: boolean;
}

const toneMap: Record<
  AlertTone,
  {
    eyebrow: string;
    badge: string;
    container: string;
    badgeClassName: string;
    titleClassName: string;
    messageClassName: string;
  }
> = {
  info: {
    eyebrow: "系统提示",
    badge: "INFO",
    container:
      "border-blue-200/80 bg-blue-50/80 dark:border-blue-900/60 dark:bg-blue-950/25",
    badgeClassName:
      "border-blue-200 bg-white/80 text-blue-700 dark:border-blue-800 dark:bg-blue-950/60 dark:text-blue-300",
    titleClassName: "text-blue-900 dark:text-blue-100",
    messageClassName: "text-blue-700 dark:text-blue-300",
  },
  success: {
    eyebrow: "执行回执",
    badge: "SUCCESS",
    container:
      "border-emerald-200/80 bg-emerald-50/80 dark:border-emerald-900/60 dark:bg-emerald-950/25",
    badgeClassName:
      "border-emerald-200 bg-white/80 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-300",
    titleClassName: "text-emerald-900 dark:text-emerald-100",
    messageClassName: "text-emerald-700 dark:text-emerald-300",
  },
  warning: {
    eyebrow: "运行提醒",
    badge: "WARNING",
    container:
      "border-amber-200/80 bg-amber-50/80 dark:border-amber-900/60 dark:bg-amber-950/25",
    badgeClassName:
      "border-amber-200 bg-white/80 text-amber-700 dark:border-amber-800 dark:bg-amber-950/60 dark:text-amber-300",
    titleClassName: "text-amber-900 dark:text-amber-100",
    messageClassName: "text-amber-700 dark:text-amber-300",
  },
  error: {
    eyebrow: "异常告警",
    badge: "ERROR",
    container:
      "border-red-200/80 bg-red-50/80 dark:border-red-900/60 dark:bg-red-950/25",
    badgeClassName:
      "border-red-200 bg-white/80 text-red-700 dark:border-red-800 dark:bg-red-950/60 dark:text-red-300",
    titleClassName: "text-red-900 dark:text-red-100",
    messageClassName: "text-red-700 dark:text-red-300",
  },
};

export default function AlertPanel({
  tone = "info",
  title,
  message,
  evidence_id,
  correlation_id,
  className,
  compact = false,
}: AlertPanelProps) {
  const config = toneMap[tone];
  const resolvedTitle =
    title ||
    (tone === "error"
      ? "操作失败"
      : tone === "success"
        ? "操作完成"
        : tone === "warning"
          ? "请注意"
          : "系统提示");

  return (
    <div
      role={tone === "error" ? "alert" : "status"}
      className={cn(
        "rounded border backdrop-blur-sm",
        compact ? "px-2.5 py-1.5" : "px-3 py-2",
        config.container,
        className,
      )}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <div className="text-[10px] font-semibold uppercase tracking-[0.18em] text-zinc-500 dark:text-zinc-400">
            {config.eyebrow}
          </div>
          <div
            className={cn(
              compact
                ? "mt-0.5 text-[12px] font-semibold"
                : "mt-1 text-xs font-semibold",
              config.titleClassName,
            )}
          >
            {resolvedTitle}
          </div>
          <div
            className={cn(
              compact
                ? "mt-0.5 text-[12px] leading-4"
                : "mt-1 text-[11px] leading-5",
              config.messageClassName,
            )}
          >
            {message}
          </div>
          {(evidence_id || correlation_id) && (
            <div className="mt-1.5 flex flex-wrap gap-2 text-[10px] font-mono opacity-80">
              {evidence_id && <div>EVIDENCE-{evidence_id.replace('EVIDENCE-', '')}</div>}
              {correlation_id && <div>Correlation: {correlation_id}</div>}
            </div>
          )}
        </div>
        <span
          className={cn(
            "shrink-0 rounded-sm border font-semibold uppercase tracking-[0.12em]",
            compact ? "px-1.5 py-0.5 text-[9px]" : "px-2 py-0.5 text-[10px]",
            config.badgeClassName,
          )}
        >
          {config.badge}
        </span>
      </div>
    </div>
  );
}
