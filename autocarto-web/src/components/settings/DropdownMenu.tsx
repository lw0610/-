"use client";

import { useEffect, useRef, useState } from "react";
import { cn } from "@/lib/utils";

export type DropdownItem =
  | { key: string; label: string; disabled?: boolean; onClick: () => void }
  | { key: string; separator: true };

interface DropdownMenuProps {
  label: string;
  items: DropdownItem[];
  className?: string;
}

export default function DropdownMenu({
  label,
  items,
  className,
}: DropdownMenuProps) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!open) {
      return;
    }
    const handleClick = (event: MouseEvent) => {
      if (!rootRef.current) {
        return;
      }
      if (!rootRef.current.contains(event.target as Node)) {
        setOpen(false);
      }
    };
    window.addEventListener("click", handleClick);
    return () => window.removeEventListener("click", handleClick);
  }, [open]);

  return (
    <div ref={rootRef} className={cn("relative", className)}>
      <button
        type="button"
        className="rounded-sm border border-zinc-300 px-2.5 py-1.5 text-xs text-zinc-700 transition-colors hover:bg-zinc-100 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800"
        onClick={() => setOpen((prev) => !prev)}
      >
        {label}
      </button>

      {open && (
        <div className="absolute right-0 z-20 mt-1 min-w-44 rounded border border-zinc-200/80 bg-white/95 p-1.5 shadow-[0_2px_8px_rgba(0,0,0,0.08)] dark:border-zinc-800/80 dark:bg-zinc-900/95">
          {items.map((item) => {
            if ("separator" in item) {
              return (
                <div
                  key={item.key}
                  className="my-1 border-t border-zinc-200 dark:border-zinc-800"
                />
              );
            }
            const action = item;
            return (
              <button
                key={action.key}
                type="button"
                disabled={action.disabled}
                className="block w-full rounded px-2.5 py-2 text-left text-xs text-zinc-700 transition-colors hover:bg-zinc-100 disabled:opacity-50 dark:text-zinc-200 dark:hover:bg-zinc-800"
                onClick={() => {
                  if (action.disabled) {
                    return;
                  }
                  setOpen(false);
                  action.onClick();
                }}
              >
                {action.label}
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
