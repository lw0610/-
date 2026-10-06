"use client";

import { cn } from "@/lib/utils";
import { useSettingsViewport } from "@/components/settings/SettingsViewportContext";

export interface SectionNavItem {
  id: string;
  label: string;
}

interface SectionNavProps {
  items: SectionNavItem[];
  className?: string;
}

export default function SectionNav({ items, className }: SectionNavProps) {
  const { activeSectionId, scrollToSection } = useSettingsViewport();

  if (items.length <= 1) {
    return null;
  }

  return (
    <nav
      className={cn(
        "flex items-center gap-1 rounded border border-zinc-200 bg-white p-1 dark:border-zinc-800 dark:bg-zinc-900",
        className,
      )}
    >
      {items.map((item) => {
        const active = activeSectionId === item.id;
        return (
          <button
            key={item.id}
            type="button"
            className={cn(
              "rounded-sm px-3 py-1.5 text-xs font-medium transition-colors",
              active
                ? "bg-zinc-100 text-zinc-900 dark:bg-zinc-800 dark:text-zinc-100"
                : "text-zinc-500 hover:bg-zinc-50 hover:text-zinc-700 dark:text-zinc-400 dark:hover:bg-zinc-800/50 dark:hover:text-zinc-300",
            )}
            onClick={() => scrollToSection(item.id)}
          >
            {item.label}
          </button>
        );
      })}
    </nav>
  );
}
