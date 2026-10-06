'use client'

import { cn } from '@/lib/utils'

interface StateBlockProps {
  title: string
  description: string
  loading?: boolean
  icon?: React.ReactNode
  className?: string
}

function DefaultIcon() {
  return (
    <svg viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden>
      <circle cx="12" cy="12" r="9" />
      <circle cx="12" cy="12" r="4" strokeOpacity="0.4" />
    </svg>
  )
}

function LoadingSpinner() {
  return <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-current border-t-transparent" />
}

export default function StateBlock({
  title,
  description,
  loading = false,
  icon,
  className,
}: StateBlockProps) {
  return (
    <div className={cn(
      'rounded border border-zinc-200/80 bg-white/90 px-3 py-5 text-center dark:border-zinc-800 dark:bg-zinc-900/70',
      className,
    )}>
      <div className="mx-auto flex h-7 w-7 items-center justify-center rounded-sm border border-zinc-200 bg-zinc-50 text-zinc-400 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-500">
        {loading ? <LoadingSpinner /> : (icon || <DefaultIcon />)}
      </div>
      <div className="mt-2 text-xs font-medium text-zinc-700 dark:text-zinc-200">{title}</div>
      <div className="mt-1 text-[11px] leading-5 text-zinc-500 dark:text-zinc-400">{description}</div>
    </div>
  )
}
