"use client";

import { createPortal } from "react-dom";

interface FullScreenSettingsDrawerProps {
  open: boolean;
  title: string;
  onClose: () => void;
  children: React.ReactNode;
}

export default function FullScreenSettingsDrawer({
  open,
  title,
  onClose,
  children,
}: FullScreenSettingsDrawerProps) {
  if (!open || typeof document === "undefined") {
    return null;
  }

  return createPortal(
    <div className="fixed inset-0 z-[72]">
      <div className="absolute inset-0 bg-black/35" onClick={onClose} />
      <div className="absolute inset-y-0 right-0 w-full max-w-[1440px] overflow-auto border-l border-zinc-200 bg-zinc-50 p-3 shadow-[0_2px_8px_rgba(0,0,0,0.08)] dark:border-zinc-800 dark:bg-zinc-950 lg:p-3">
        <div className="rounded border border-zinc-200 bg-white px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-900">
          <div className="flex items-center justify-between gap-3">
            <div className="text-[13px] font-semibold text-zinc-900 dark:text-zinc-100">{title}</div>
            <button
              type="button"
              onClick={onClose}
              className="rounded-sm border border-zinc-300 px-3 py-1.5 text-xs text-zinc-700 transition-colors hover:bg-zinc-100 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800"
            >
              关闭
            </button>
          </div>
        </div>
        <div className="mt-3">{children}</div>
      </div>
    </div>,
    document.body,
  );
}
