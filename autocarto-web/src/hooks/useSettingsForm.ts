'use client'

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'
import {
  describeSettingsMutation,
  extractMutationFromError,
  getApiErrorMessage,
  type SettingsMutationResult,
} from '@/lib/api'
import { useUnsavedChangesGuard } from '@/hooks/useUnsavedChangesGuard'

interface UseSettingsFormOptions<T> {
  defaultValues: T
  load: () => Promise<T>
  save: (values: T) => Promise<void | SettingsMutationResult>
  validate?: (values: T) => string | null
  onSaveFeedback?: (feedback: {
    message: string
    warning?: string
    evidence_id?: string
    correlation_id?: string
  }) => void
  saveSuccessMessage?: string
  loadErrorMessage?: string
  saveErrorMessage?: string
}

function readErrorMessage(error: unknown, fallback: string) {
  const mutation = extractMutationFromError(error)
  if (mutation?.required_action === 'stop_active_runs') {
    const blockingParts = [
      (mutation.blocking_runs || []).length > 0
        ? `阻塞会话：${mutation.blocking_runs?.join('、')}`
        : '',
      (mutation.blocking_instances || []).length > 0
        ? `关联实例：${mutation.blocking_instances?.join('、')}`
        : '',
      mutation.next_step || '',
    ].filter(Boolean)
    return [getApiErrorMessage(error, fallback), ...blockingParts].filter(Boolean).join('；')
  }
  return getApiErrorMessage(error, fallback)
}

export function useSettingsForm<T>({
  defaultValues,
  load,
  save,
  validate,
  onSaveFeedback,
  saveSuccessMessage = '配置已保存',
  loadErrorMessage = '加载失败',
  saveErrorMessage = '保存失败',
}: UseSettingsFormOptions<T>) {
  const [form, setForm] = useState<T>(defaultValues)
  const [originalForm, setOriginalForm] = useState<T>(defaultValues)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const loadRef = useRef(load)

  loadRef.current = load

  const isDirty = useMemo(
    () => JSON.stringify(form) !== JSON.stringify(originalForm),
    [form, originalForm],
  )

  useUnsavedChangesGuard(isDirty)

  const reload = useCallback(async () => {
    setLoading(true)
    try {
      const next = await loadRef.current()
      setForm(next)
      setOriginalForm(next)
    } catch (error) {
      toast.error(readErrorMessage(error, loadErrorMessage))
    } finally {
      setLoading(false)
    }
  }, [loadErrorMessage])

  const handleSave = useCallback(async () => {
    const message = validate ? validate(form) : null
    if (message) {
      toast.error(message)
      return false
    }

    setSaving(true)
    try {
      const saveResult = await save(form)
      setOriginalForm(form)
      const feedback = describeSettingsMutation(
        saveResult ?? undefined,
        saveSuccessMessage,
      )
      toast.success(feedback.message)
      if (feedback.warning) {
        toast.warning(feedback.warning)
      }
      onSaveFeedback?.({
        message: feedback.message,
        warning: feedback.warning,
      })
      return true
    } catch (error) {
      const mutation = extractMutationFromError(error)
      const message = readErrorMessage(error, saveErrorMessage)
      toast.error(message)
      onSaveFeedback?.({
        message,
        evidence_id: mutation?.evidence_id,
        correlation_id: mutation?.correlation_id,
      })
      return false
    } finally {
      setSaving(false)
    }
  }, [form, onSaveFeedback, save, saveErrorMessage, saveSuccessMessage, validate])

  const resetToDefault = useCallback(() => {
    setForm(defaultValues)
  }, [defaultValues])

  useEffect(() => {
    void reload()
  }, [reload])

  return {
    form,
    setForm,
    originalForm,
    setOriginalForm,
    isDirty,
    loading,
    saving,
    reload,
    handleSave,
    resetToDefault,
  }
}

export type UseSettingsFormReturn<T> = ReturnType<typeof useSettingsForm<T>>
