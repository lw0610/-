"use client";

import { useMemo, useState } from "react";
import StatusBadge from "@/components/settings/StatusBadge";
import { cn } from "@/lib/utils";

export interface TagOption {
  value: string;
  label: string;
  description?: string;
  badge?: string;
  disabled?: boolean;
  disabledReason?: string;
  title?: string;
  itemClassName?: string;
  locked?: boolean;
}

interface TagSelectorProps {
  options: TagOption[];
  value: string[];
  onChange: (next: string[]) => void;
  placeholder?: string;
  className?: string;
  showFilters?: boolean;
}

export default function TagSelector({
  options,
  value,
  onChange,
  placeholder = "搜索...",
  className,
  showFilters = true,
}: TagSelectorProps) {
  const [query, setQuery] = useState("");
  const [filterMode, setFilterMode] = useState<
    "all" | "selected" | "unselected"
  >("all");
  const normalizedOptions = useMemo(() => {
    const deduped: TagOption[] = [];
    const seen = new Set<string>();
    options.forEach((rawItem) => {
      const normalizedValue = String(rawItem.value || "").trim();
      if (!normalizedValue || seen.has(normalizedValue)) {
        return;
      }
      seen.add(normalizedValue);
      deduped.push({
        ...rawItem,
        value: normalizedValue,
      });
    });
    return deduped;
  }, [options]);
  const selectedSet = useMemo(() => new Set(value), [value]);
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return normalizedOptions.filter((item) => {
      if (
        q &&
        !(
          item.label.toLowerCase().includes(q) ||
          item.value.toLowerCase().includes(q) ||
          String(item.description || "")
            .toLowerCase()
            .includes(q)
        )
      ) {
        return false;
      }

      const isSelected = selectedSet.has(item.value);
      if (filterMode === "selected" && !isSelected) {
        return false;
      }
      if (filterMode === "unselected" && isSelected) {
        return false;
      }

      return true;
    });
  }, [normalizedOptions, query, selectedSet, filterMode]);

  const toggle = (nextValue: string) => {
    if (selectedSet.has(nextValue)) {
      onChange(value.filter((item) => item !== nextValue));
      return;
    }
    onChange([...value, nextValue]);
  };

  return (
    <div className={cn("space-y-2", className)}>
      <div className="rounded border border-zinc-200/60 bg-zinc-50 p-2.5 dark:border-zinc-800 dark:bg-zinc-950/60 dark:shadow-zinc-950/30">
        <div className="flex items-center gap-2">
          <input
            className="h-7 w-full rounded border border-zinc-300/90 bg-white/95 px-3 text-[11px] text-zinc-700 outline-none transition-colors placeholder:text-zinc-400 focus:border-blue-500 dark:border-zinc-700/90 dark:bg-zinc-900 dark:text-zinc-100"
            placeholder={placeholder}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
          <StatusBadge className="whitespace-nowrap">
            已选 {value.length}
          </StatusBadge>
        </div>
        {showFilters && (
          <div className="mt-2 flex flex-wrap items-center gap-1.5 text-xs">
            {[
              { key: "all", label: "全部" },
              { key: "selected", label: "已选" },
              { key: "unselected", label: "未选" },
            ].map((item) => (
              <button
                key={item.key}
                type="button"
                className={cn(
                  "inline-flex h-7 items-center rounded border px-2.5 transition-colors",
                  filterMode === item.key
                    ? "border-blue-500 bg-blue-50 text-blue-700 dark:border-blue-600 dark:bg-blue-900/30 dark:text-blue-300"
                    : "border-zinc-300 bg-white/80 text-zinc-600 hover:bg-zinc-100 dark:border-zinc-700 dark:bg-zinc-950/70 dark:text-zinc-300 dark:hover:bg-zinc-800",
                )}
                onClick={() =>
                  setFilterMode(item.key as "all" | "selected" | "unselected")
                }
              >
                {item.label}
              </button>
            ))}
          </div>
        )}
      </div>

      <div className="max-h-56 space-y-1 overflow-auto rounded border border-zinc-200 bg-white p-1.5 dark:border-zinc-800 dark:bg-zinc-900">
        {filtered.length === 0 && (
          <div className="px-2 py-1 text-xs text-zinc-500">无匹配项</div>
        )}
        {filtered.map((item) => {
          const checked = selectedSet.has(item.value);
          const title = String(
            item.title || item.description || item.label || item.value,
          ).trim();
          const disabledReason = String(item.disabledReason || "").trim();
          return (
            <label
              key={item.value}
              className={cn(
                "flex items-center gap-2 rounded border px-2 py-1 text-xs transition-colors",
                checked
                  ? "border-blue-200 bg-blue-50/70 dark:border-blue-900/60 dark:bg-blue-950/20"
                  : "border-transparent hover:border-zinc-200 hover:bg-zinc-50 dark:hover:border-zinc-800 dark:hover:bg-zinc-900/70",
                !item.disabled && "cursor-pointer",
                item.itemClassName,
                item.disabled && "cursor-not-allowed opacity-50",
              )}
              title={title || undefined}
              onClick={() => {
                if (!item.disabled) {
                  toggle(item.value)
                }
              }}
            >
              <input
                type="checkbox"
                checked={checked}
                disabled={item.disabled}
                onChange={() => toggle(item.value)}
                onClick={(event) => event.stopPropagation()}
                className="h-3.5 w-3.5 shrink-0 accent-blue-600"
                title={
                  item.disabled
                    ? disabledReason || title || undefined
                    : title || undefined
                }
              />
              <span className="flex min-w-0 flex-1 items-center gap-1.5">
                <span className="flex-shrink-0 text-zinc-700 dark:text-zinc-100">
                  {item.locked && (
                    <StatusBadge className="mr-1" tone="accent" size="xs">
                      锁定
                    </StatusBadge>
                  )}
                  {item.label}
                  {item.badge && (
                    <StatusBadge className="ml-1" size="xs">
                      {item.badge}
                    </StatusBadge>
                  )}
                </span>
                {item.description && (
                  <span className="truncate text-[10px] text-zinc-500 dark:text-zinc-400">
                    {item.description}
                  </span>
                )}
                {item.disabled && disabledReason && (
                  <span className="truncate text-[10px] text-amber-600 dark:text-amber-300">
                    ({disabledReason})
                  </span>
                )}
              </span>
            </label>
          );
        })}
      </div>
    </div>
  );
}
