import { cn } from '@/lib/utils'

interface Props {
  expanded: boolean
  size?: 'sm' | 'md'
  className?: string
}

export default function CollapseIndicator({ expanded, size = 'md', className }: Props) {
  return (
    <svg
      width={size === 'sm' ? '8' : '10'}
      height={size === 'sm' ? '8' : '10'}
      viewBox="0 0 10 10"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className={cn(
        'transition-transform duration-200 ease-[cubic-bezier(0.4,0,0.2,1)]',
        expanded ? 'rotate-90' : '',
        className
      )}
    >
      <path
        d="M3.5 2.5L7.5 5L3.5 7.5V2.5Z"
        fill="currentColor"
        className="text-zinc-400 dark:text-zinc-500"
      />
    </svg>
  )
}
