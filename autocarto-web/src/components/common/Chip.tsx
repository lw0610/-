'use client'

import { cn } from '@/lib/utils'

export type ChipTone = 'zinc' | 'blue' | 'amber' | 'emerald' | 'red' | 'violet' | 'cyan'
export type ChipSize = 'xs' | 'sm'
export type ChipShape = 'pill' | 'rounded'

interface ChipProps {
  tone?: ChipTone
  size?: ChipSize
  shape?: ChipShape
  dot?: boolean
  dotAnimate?: boolean
  dotClassName?: string
  className?: string
  children: React.ReactNode
}

const toneStyles: Record<ChipTone, string> = {
  zinc: 'border-zinc-200 bg-white text-zinc-600 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300',
  blue: 'border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900/60 dark:bg-blue-900/20 dark:text-blue-300',
  amber: 'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-900/60 dark:bg-amber-900/20 dark:text-amber-300',
  emerald: 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-900/20 dark:text-emerald-300',
  red: 'border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300',
  violet: 'border-zinc-200 bg-zinc-50 text-zinc-700 dark:border-zinc-800 dark:bg-zinc-950/20 dark:text-zinc-300',
  cyan: 'border-cyan-200 bg-cyan-50 text-cyan-700 dark:border-cyan-800 dark:bg-cyan-950/20 dark:text-cyan-300',
}

const dotToneStyles: Record<ChipTone, string> = {
  zinc: 'bg-zinc-400 dark:bg-zinc-500',
  blue: 'bg-blue-500',
  amber: 'bg-amber-500',
  emerald: 'bg-emerald-500',
  red: 'bg-red-500',
  violet: 'bg-zinc-500',
  cyan: 'bg-cyan-500',
}

const sizeStyles: Record<ChipSize, string> = {
  xs: 'px-1.5 py-0.5 text-[9px]',
  sm: 'px-2 py-0.5 text-[10px]',
}

const shapeStyles: Record<ChipShape, string> = {
  pill: 'rounded-sm',
  rounded: 'rounded-sm',
}

export default function Chip({
  tone = 'zinc',
  size = 'xs',
  shape = 'rounded',
  dot,
  dotAnimate,
  dotClassName,
  className,
  children,
}: ChipProps) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 border',
        toneStyles[tone],
        sizeStyles[size],
        shapeStyles[shape],
        className,
      )}
    >
      {dot && (
        <span
          className={cn(
            'h-1.5 w-1.5 rounded-sm',
            dotClassName || dotToneStyles[tone],
            dotAnimate && 'animate-pulse',
          )}
        />
      )}
      {children}
    </span>
  )
}

export function MetricPill({ label, value }: { label: string; value: string }) {
  return (
    <span className="inline-flex items-center gap-1 rounded-sm border border-zinc-200 bg-zinc-50 px-2 py-1 text-[10px] dark:border-zinc-700 dark:bg-zinc-900">
      <span className="uppercase tracking-wide text-zinc-500 dark:text-zinc-400">{label}</span>
      <span className="font-mono text-[11px] text-zinc-700 dark:text-zinc-200">{value}</span>
    </span>
  )
}

export function StatusDot({ status, className }: { status: string; className?: string }) {
  const dotClass =
    status === 'running' ? 'bg-emerald-500'
      : status === 'stopped' ? 'bg-amber-500'
        : 'bg-red-500'

  return <span className={cn('h-2 w-2 rounded-sm', dotClass, className)} />
}
