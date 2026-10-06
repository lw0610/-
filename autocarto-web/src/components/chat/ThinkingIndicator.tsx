'use client'

interface ThinkingIndicatorProps {
  agentName?: string
}

export default function ThinkingIndicator({ agentName }: ThinkingIndicatorProps) {
  return (
    <div className="flex items-center gap-3 px-3 py-2" role="status" aria-live="polite" aria-label={agentName ? `${agentName} 思考中` : '思考中'}>
      <div className="flex h-7 w-7 items-center justify-center rounded border border-zinc-200 bg-zinc-50 dark:border-zinc-700 dark:bg-zinc-900">
        <svg viewBox="0 0 20 20" className="h-4 w-4 text-zinc-400 dark:text-zinc-500 animate-pulse-subtle" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden>
          <circle cx="10" cy="10" r="7" />
          <path d="M10 6v4l2.5 1.5" />
        </svg>
      </div>
      <div className="flex flex-col gap-1">
        <span className="text-[11px] text-zinc-600 dark:text-zinc-300">
          {agentName ? `${agentName} 思考中...` : '思考中...'}
        </span>
        <div className="h-1.5 w-32 rounded-sm bg-zinc-100 dark:bg-zinc-800 overflow-hidden">
          <div className="h-full w-full animate-shimmer rounded-sm" />
        </div>
      </div>
    </div>
  )
}

