'use client'

import { useEffect } from 'react'

const DEFAULT_UNSAVED_CHANGES_MESSAGE = '当前页面有未保存更改，确定离开吗？'

export function confirmDiscardChanges(
  message = DEFAULT_UNSAVED_CHANGES_MESSAGE,
) {
  if (typeof window === 'undefined') {
    return true
  }
  return window.confirm(message)
}

export function useUnsavedChangesGuard(
  isDirty: boolean,
  message = DEFAULT_UNSAVED_CHANGES_MESSAGE,
) {
  useEffect(() => {
    if (typeof window === 'undefined') {
      return
    }

    const handleBeforeUnload = (event: BeforeUnloadEvent) => {
      if (!isDirty) {
        return
      }
      event.preventDefault()
      event.returnValue = ''
    }

    window.addEventListener('beforeunload', handleBeforeUnload)
    return () => {
      window.removeEventListener('beforeunload', handleBeforeUnload)
    }
  }, [isDirty])

  useEffect(() => {
    if (!isDirty || typeof document === 'undefined') {
      return
    }

    const clickHandler = (event: MouseEvent) => {
      const target = event.target as HTMLElement | null
      const anchor = target?.closest('a[href]') as HTMLAnchorElement | null
      if (!anchor) {
        return
      }

      const href = anchor.getAttribute('href')
      if (!href || href.startsWith('#')) {
        return
      }

      if (anchor.target === '_blank' || anchor.hasAttribute('download')) {
        return
      }

      const nextURL = new URL(anchor.href, window.location.href)
      const current =
        window.location.pathname + window.location.search + window.location.hash
      const next = nextURL.pathname + nextURL.search + nextURL.hash
      if (next === current) {
        return
      }

      const ok = confirmDiscardChanges(message)
      if (!ok) {
        event.preventDefault()
        event.stopPropagation()
      }
    }

    document.addEventListener('click', clickHandler, true)
    return () => {
      document.removeEventListener('click', clickHandler, true)
    }
  }, [isDirty, message])
}

export const UNSAVED_CHANGES_MESSAGE = DEFAULT_UNSAVED_CHANGES_MESSAGE
