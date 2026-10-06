"use client";

import InfoTooltip from "@/components/settings/InfoTooltip";
import { cn } from "@/lib/utils";

const inputBaseClassName =
  "h-8 w-full rounded-sm border border-zinc-300/90 bg-zinc-100 px-2.5 text-xs text-zinc-800 outline-none transition-colors placeholder:text-zinc-400 focus:bg-white focus:border-blue-500 focus:ring-2 focus:ring-blue-500/10 dark:border-zinc-700/90 dark:bg-zinc-900 dark:text-zinc-100 dark:placeholder:text-zinc-500";

interface FieldProps {
  label: string;
  description?: string;
  className?: string;
  htmlFor?: string;
  children: React.ReactNode;
}

export function Field({
  label,
  description,
  className,
  htmlFor,
  children,
}: FieldProps) {
  return (
    <label className={cn("flex flex-col gap-1.5", className)} htmlFor={htmlFor}>
      <span className="flex items-center gap-1.5 text-[11px] font-semibold text-zinc-700 dark:text-zinc-300">
        <span>{label}</span>
        {description ? <InfoTooltip content={description} /> : null}
      </span>
      {children}
    </label>
  );
}

export function TextInput(props: React.InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input {...props} className={cn(inputBaseClassName, props.className)} />
  );
}

export function NumberInput(
  props: React.InputHTMLAttributes<HTMLInputElement>,
) {
  return (
    <input
      type="number"
      {...props}
      className={cn(inputBaseClassName, "tabular-nums", props.className)}
    />
  );
}

export function SelectInput(
  props: React.SelectHTMLAttributes<HTMLSelectElement>,
) {
  return (
    <div className="relative">
      <select
        {...props}
        className={cn(inputBaseClassName, "appearance-none pr-8", props.className)}
      />
      <svg
        className="pointer-events-none absolute right-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-zinc-400 dark:text-zinc-500"
        viewBox="0 0 16 16"
        fill="none"
        aria-hidden="true"
      >
        <path d="m4 6 4 4 4-4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    </div>
  );
}

export function TextAreaInput(
  props: React.TextareaHTMLAttributes<HTMLTextAreaElement>,
) {
  return (
    <textarea
      {...props}
      className={cn(
        inputBaseClassName,
        "min-h-[120px] !h-auto py-2.5 font-mono text-xs leading-5",
        props.className,
      )}
    />
  );
}

interface PathInputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  onBrowse?: () => void;
  browseLabel?: string;
}

export function PathInput({
  onBrowse,
  browseLabel = "浏览…",
  className,
  ...props
}: PathInputProps) {
  return (
    <div className={cn("flex gap-2", className)}>
      <input
        {...props}
        className={cn(inputBaseClassName, "min-w-0 flex-1 font-mono")}
      />
      {onBrowse && (
        <button
          type="button"
          onClick={onBrowse}
          className="inline-flex h-8 shrink-0 items-center rounded-sm border border-zinc-300 bg-white px-3 text-xs font-medium text-zinc-700 transition-colors hover:bg-zinc-100 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-200 dark:hover:bg-zinc-800"
        >
          {browseLabel}
        </button>
      )}
    </div>
  );
}

interface CheckboxFieldProps {
  label: string;
  description?: string;
  className?: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (next: boolean) => void;
}

export function CheckboxField({
  label,
  description,
  className,
  checked,
  disabled,
  onChange,
}: CheckboxFieldProps) {
  return (
    <label
      className={cn(
        "flex items-center gap-2.5 rounded-sm border px-3 py-2.5 text-xs transition-colors",
        checked
          ? "border-blue-200 bg-blue-50/80 text-blue-900 dark:border-blue-900/60 dark:bg-blue-950/30 dark:text-blue-100"
          : "border-zinc-200 bg-zinc-50 text-zinc-700 dark:border-zinc-800 dark:bg-zinc-950/60 dark:text-zinc-200",
        disabled && "cursor-not-allowed opacity-60",
        className,
      )}
    >
      <input
        type="checkbox"
        className="h-4 w-4 shrink-0 accent-blue-600"
        checked={checked}
        disabled={disabled}
        onChange={(event) => onChange(event.target.checked)}
      />
      <span className="min-w-0 flex items-center gap-1.5">
        <span className="font-medium leading-tight">{label}</span>
        {description ? (
          <InfoTooltip content={description} side="right" />
        ) : null}
      </span>
    </label>
  );
}
