import type { SVGProps } from 'react'

export type AppIconProps = SVGProps<SVGSVGElement>

function BaseIcon({ children, ...props }: AppIconProps) {
  return (
    <svg
      viewBox="0 0 20 20"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      {...props}
    >
      {children}
    </svg>
  )
}

export function InstancesIcon(props: AppIconProps) {
  return (
    <BaseIcon {...props}>
      <rect x="2.75" y="3" width="14.5" height="14" rx="2.25" />
      <path d="M7.75 3v14" />
      <path d="M10.75 7h3.75" />
      <path d="M10.75 10h3.75" />
      <path d="M10.75 13h3.75" />
    </BaseIcon>
  )
}

export function SessionsIcon(props: AppIconProps) {
  return (
    <BaseIcon {...props}>
      <path d="M5.5 5.5h7.25a2 2 0 0 1 2 2v6.75a2 2 0 0 1-2 2H7.25a2 2 0 0 1-2-2V5.5Z" />
      <path d="M8.25 3.75h4.5a2 2 0 0 1 2 2v4.5" />
      <path d="M7.75 9h4.75" />
      <path d="M7.75 12h3.25" />
    </BaseIcon>
  )
}

export function ChatIcon(props: AppIconProps) {
  return (
    <BaseIcon {...props}>
      <path d="M4.5 5.25h11a2 2 0 0 1 2 2V12a2 2 0 0 1-2 2H9l-3.5 2.75V14H4.5a2 2 0 0 1-2-2V7.25a2 2 0 0 1 2-2Z" />
      <path d="M6.75 8.75h6.5" />
      <path d="M6.75 11.25h4.25" />
    </BaseIcon>
  )
}

export function SettingsIcon(props: AppIconProps) {
  return (
    <BaseIcon {...props}>
      <circle cx="10" cy="10" r="2.5" />
      <path d="M10 2.75v2" />
      <path d="M10 15.25v2" />
      <path d="M17.25 10h-2" />
      <path d="M4.75 10h-2" />
      <path d="M15.13 4.87 13.7 6.3" />
      <path d="M6.3 13.7l-1.43 1.43" />
      <path d="M15.13 15.13 13.7 13.7" />
      <path d="M6.3 6.3 4.87 4.87" />
    </BaseIcon>
  )
}

export function ArchiveIcon(props: AppIconProps) {
  return (
    <BaseIcon {...props}>
      <path d="M3 5.5h14" />
      <path d="M4.25 5.5h11.5v8.25A2.25 2.25 0 0 1 13.5 16H6.5a2.25 2.25 0 0 1-2.25-2.25V5.5Z" />
      <path d="M5.25 3.75h9.5A1.25 1.25 0 0 1 16 5v.5H4V5a1.25 1.25 0 0 1 1.25-1.25Z" />
      <path d="M8 9.5h4" />
    </BaseIcon>
  )
}

export function TrashIcon(props: AppIconProps) {
  return (
    <BaseIcon {...props}>
      <path d="M5.75 6.25v8.5A1.75 1.75 0 0 0 7.5 16.5h5a1.75 1.75 0 0 0 1.75-1.75v-8.5" />
      <path d="M4 5.25h12" />
      <path d="M7.5 5.25V4a1 1 0 0 1 1-1h3a1 1 0 0 1 1 1v1.25" />
      <path d="M8.25 8.5v5" />
      <path d="M11.75 8.5v5" />
    </BaseIcon>
  )
}

export function ServicesIcon(props: AppIconProps) {
  return (
    <BaseIcon {...props}>
      <rect x="3" y="3.25" width="14" height="4" rx="1.5" />
      <rect x="3" y="8" width="14" height="4" rx="1.5" />
      <rect x="3" y="12.75" width="14" height="4" rx="1.5" />
      <path d="M6 5.25h.01" />
      <path d="M6 10h.01" />
      <path d="M6 14.75h.01" />
      <path d="M9 5.25h4.5" />
      <path d="M9 10h4.5" />
      <path d="M9 14.75h4.5" />
    </BaseIcon>
  )
}

export function PlusIcon(props: AppIconProps) {
  return (
    <BaseIcon {...props}>
      <path d="M10 4.25v11.5" />
      <path d="M4.25 10h11.5" />
    </BaseIcon>
  )
}

export function CloseIcon(props: AppIconProps) {
  return (
    <BaseIcon {...props}>
      <path d="m5.5 5.5 9 9" />
      <path d="m14.5 5.5-9 9" />
    </BaseIcon>
  )
}
