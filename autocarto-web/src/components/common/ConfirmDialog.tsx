"use client";

import { useEffect } from "react";
import { createPortal } from "react-dom";
import { cn } from "@/lib/utils";

interface ConfirmDialogProps {
  open: boolean;
  title: string;
  description?: string;
  confirmText?: string;
  cancelText?: string;
  danger?: boolean;
  loading?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

export default function ConfirmDialog({
  open,
  title,
  description,
  confirmText = "确认",
  cancelText = "取消",
  danger = false,
  loading = false,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  useEffect(() => {
    if (!open) {
      return;
    }

    const handleKeydown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !loading) {
        onCancel();
      }
    };

    window.addEventListener("keydown", handleKeydown);
    return () => window.removeEventListener("keydown", handleKeydown);
  }, [loading, onCancel, open]);

  if (!open || typeof document === "undefined") {
    return null;
  }

  return createPortal(
    <div
      className="fixed inset-0 z-[80] flex items-center justify-center bg-zinc-950/60 px-3 backdrop-blur-sm"
      onClick={() => {
        if (!loading) {
          onCancel();
        }
      }}
    >
      <div
        className="w-full max-w-lg rounded border border-zinc-200/80 bg-white/96 p-3 dark:border-zinc-800/80 dark:bg-zinc-950/96"
        role="dialog"
        aria-modal="true"
        aria-label={title}
        onClick={(event) => event.stopPropagation()}
      >
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0 flex-1">
            <div
              className={cn(
                "inline-flex rounded-sm border px-2.5 py-1 text-[10px] font-semibold uppercase tracking-[0.18em]",
                danger
                  ? "border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300"
                  : "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900/60 dark:bg-blue-950/30 dark:text-blue-300",
              )}
            >
              {danger ? "风险操作" : "操作确认"}
            </div>
            <div className="mt-3 text-[13px] font-semibold tracking-tight text-zinc-900 dark:text-zinc-100">
              {title}
            </div>
            {description && (
              <div className="mt-3 rounded border border-zinc-200/80 bg-zinc-50/80 px-3 py-2.5 text-xs leading-6 text-zinc-600 dark:border-zinc-800/80 dark:bg-zinc-900/55 dark:text-zinc-300">
                {description}
              </div>
            )}
          </div>
          <span
            className={cn(
              "shrink-0 rounded-sm border px-2 py-0.5 text-[11px] font-medium",
              loading
                ? "border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-300"
                : "border-zinc-200 bg-zinc-50 text-zinc-500 dark:border-zinc-800 dark:bg-zinc-900/70 dark:text-zinc-400",
            )}
          >
            {loading ? "处理中" : "待确认"}
          </span>
        </div>

        <div className="mt-5 flex items-center justify-end gap-2">
          <button
            className="rounded-sm border border-zinc-300 px-3 py-1.5 text-[11px] text-zinc-700 transition-colors hover:bg-zinc-100 disabled:opacity-50 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800"
            onClick={onCancel}
            disabled={loading}
          >
            {cancelText}
          </button>
          <button
            className={cn(
              "rounded-sm border px-3 py-1.5 text-[11px] transition-colors disabled:opacity-50",
              danger
                ? "border-red-300 bg-red-50 text-red-700 hover:bg-red-100 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300 dark:hover:bg-red-950/50"
                : "border-blue-300 bg-blue-50 text-blue-700 hover:bg-blue-100 dark:border-blue-900/60 dark:bg-blue-950/30 dark:text-blue-300 dark:hover:bg-blue-950/50",
            )}
            onClick={onConfirm}
            disabled={loading}
          >
            {loading ? "处理中..." : confirmText}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}
