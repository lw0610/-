"use client";

import { cn } from "@/lib/utils";

type InlineActionVariant = "default" | "danger";

interface InlineActionButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: InlineActionVariant;
}

export default function InlineActionButton({
  className,
  variant = "default",
  type = "button",
  ...props
}: InlineActionButtonProps) {
  return (
    <button
      type={type}
      className={cn(
        "inline-flex h-7 items-center rounded-sm border px-2.5 text-[11px] font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50",
        variant === "danger"
          ? "border-red-200 bg-red-50/80 text-red-700 hover:bg-red-100 dark:border-red-900/70 dark:bg-red-950/30 dark:text-red-300 dark:hover:bg-red-950/50"
          : "border-zinc-200 bg-white/85 text-zinc-600 hover:bg-zinc-100 dark:border-zinc-800 dark:bg-zinc-950/70 dark:text-zinc-300 dark:hover:bg-zinc-800",
        className,
      )}
      {...props}
    />
  );
}
