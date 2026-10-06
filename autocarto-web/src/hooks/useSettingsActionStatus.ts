'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'

import {
  buildSettingsActionStatus,
  type SettingsActionStatus,
} from '@/lib/settingsWorkbench'

export function useSettingsActionStatus(timeoutMs = 5000) {
  const [actionStatus, setActionStatus] = useState<SettingsActionStatus & { evidence_id?: string; correlation_id?: string } | null>(null)
  const timeoutRef = useRef<number | null>(null)

  const clearActionStatus = useCallback(() => {
    if (timeoutRef.current !== null && typeof window !== 'undefined') {
      window.clearTimeout(timeoutRef.current)
      timeoutRef.current = null
    }
    setActionStatus(null)
  }, [])

  const scheduleClear = useCallback(() => {
    if (typeof window === 'undefined') {
      return
    }
    if (timeoutRef.current !== null) {
      window.clearTimeout(timeoutRef.current)
    }
    timeoutRef.current = window.setTimeout(() => {
      setActionStatus(null)
      timeoutRef.current = null
    }, timeoutMs)
  }, [timeoutMs])

  const publishActionStatus = useCallback(
    (message: string, warning?: string, emitToast = true, evidence_id?: string, correlation_id?: string) => {
      const nextStatus = buildSettingsActionStatus(message, warning)
      const enhancedStatus = { ...nextStatus, evidence_id, correlation_id }
      if (emitToast) {
        toast.success(message)
        if (warning) {
          toast.warning(warning)
        }
      }
      setActionStatus(enhancedStatus)
      scheduleClear()
      return enhancedStatus
    },
    [scheduleClear],
  )

  const showActionStatus = useCallback(
    (message: string, warning?: string, evidence_id?: string, correlation_id?: string) =>
      publishActionStatus(message, warning, true, evidence_id, correlation_id),
    [publishActionStatus],
  )

  useEffect(() => clearActionStatus, [clearActionStatus])

  return {
    actionStatus,
    setActionStatus,
    clearActionStatus,
    publishActionStatus,
    showActionStatus,
  }
}
