'use client'

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'
import {
  describeSettingsMutation,
  extractMutationFromError,
  getApiErrorMessage,
  type SettingsMutationResult,
} from '@/lib/api'
import {
  confirmDiscardChanges,
  useUnsavedChangesGuard,
} from '@/hooks/useUnsavedChangesGuard'

interface UseListDetailFormOptions<TItem, TDetail> {
  loadList: () => Promise<TItem[]>
  loadDetail: (id: string) => Promise<TDetail>
  save: (id: string, detail: TDetail) => Promise<void | SettingsMutationResult>
  getItemId: (item: TItem) => string
  getItemLabel: (item: TItem) => string
  getItemSearchText?: (item: TItem) => string
  sortItems?: (items: TItem[]) => TItem[]
  serializeDetail?: (detail: TDetail) => string
  onSaveFeedback?: (feedback: { message: string; warning?: string }) => void
  saveSuccessMessage?: string | ((detail: TDetail, id: string) => string)
  loadErrorMessage?: string
  saveErrorMessage?: string
  discardMessage?: string
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

function defaultSerializeDetail<TDetail>(detail: TDetail) {
  return JSON.stringify(detail)
}

export function useListDetailForm<TItem, TDetail>({
  loadList,
  loadDetail,
  save,
  getItemId,
  getItemLabel,
  getItemSearchText,
  sortItems,
  serializeDetail = defaultSerializeDetail,
  onSaveFeedback,
  saveSuccessMessage = '内容已保存',
  loadErrorMessage = '加载失败',
  saveErrorMessage = '保存失败',
  discardMessage = '当前页面有未保存更改，确定丢弃吗？',
}: UseListDetailFormOptions<TItem, TDetail>) {
  const [items, setItems] = useState<TItem[]>([])
  const [selectedId, setSelectedId] = useState('')
  const [detail, setDetail] = useState<TDetail | null>(null)
  const [originalDetail, setOriginalDetail] = useState<TDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')

  const loadListRef = useRef(loadList)
  const loadDetailRef = useRef(loadDetail)
  const saveRef = useRef(save)
  const selectedIdRef = useRef(selectedId)

  loadListRef.current = loadList
  loadDetailRef.current = loadDetail
  saveRef.current = save
  selectedIdRef.current = selectedId

  const isDirty = useMemo(() => {
    if (!detail || !originalDetail) {
      return false
    }
    return serializeDetail(detail) !== serializeDetail(originalDetail)
  }, [detail, originalDetail, serializeDetail])

  useUnsavedChangesGuard(isDirty, discardMessage)

  const loadCurrentDetail = useCallback(
    async (id: string) => {
      const normalizedId = id.trim()
      if (!normalizedId) {
        setSelectedId('')
        setDetail(null)
        setOriginalDetail(null)
        return null
      }

      const nextDetail = await loadDetailRef.current(normalizedId)
      setSelectedId(normalizedId)
      setDetail(nextDetail)
      setOriginalDetail(nextDetail)
      return nextDetail
    },
    [],
  )

  const reload = useCallback(
    async (preferredId?: string, force = false) => {
      if (!force && isDirty && !confirmDiscardChanges(discardMessage)) {
        return false
      }

      setLoading(true)
      try {
        const nextItems = await loadListRef.current()
        setItems(nextItems)

        const normalizedPreferred = String(preferredId || '').trim()
        const normalizedCurrent = String(selectedIdRef.current || '').trim()
        const candidateId = normalizedPreferred || normalizedCurrent

        const fallbackItem = nextItems[0]
        const targetId = nextItems.find((item) => getItemId(item) === candidateId)
          ? candidateId
          : fallbackItem
            ? getItemId(fallbackItem)
            : ''

        if (!targetId) {
          setSelectedId('')
          setDetail(null)
          setOriginalDetail(null)
          return true
        }

        await loadCurrentDetail(targetId)
        return true
      } catch (error) {
        toast.error(readErrorMessage(error, loadErrorMessage))
        return false
      } finally {
        setLoading(false)
      }
    },
    [discardMessage, getItemId, isDirty, loadCurrentDetail, loadErrorMessage],
  )

  const selectItem = useCallback(
    async (id: string, force = false) => {
      const normalizedId = String(id || '').trim()
      if (!normalizedId) {
        return false
      }
      if (normalizedId === selectedIdRef.current) {
        return true
      }
      if (!force && isDirty && !confirmDiscardChanges(discardMessage)) {
        return false
      }

      setLoading(true)
      try {
        await loadCurrentDetail(normalizedId)
        return true
      } catch (error) {
        toast.error(readErrorMessage(error, loadErrorMessage))
        return false
      } finally {
        setLoading(false)
      }
    },
    [discardMessage, isDirty, loadCurrentDetail, loadErrorMessage],
  )

  const handleSave = useCallback(async () => {
    const normalizedId = String(selectedIdRef.current || '').trim()
    if (!normalizedId || !detail) {
      return false
    }

    setSaving(true)
    try {
      const result = await saveRef.current(normalizedId, detail)
      const feedback = describeSettingsMutation(
        result ?? undefined,
        typeof saveSuccessMessage === 'function'
          ? saveSuccessMessage(detail, normalizedId)
          : saveSuccessMessage,
      )
      toast.success(feedback.message)
      if (feedback.warning) {
        toast.warning(feedback.warning)
      }
      onSaveFeedback?.({
        message: feedback.message,
        warning: feedback.warning,
      })
      await reload(normalizedId, true)
      return true
    } catch (error) {
      toast.error(readErrorMessage(error, saveErrorMessage))
      return false
    } finally {
      setSaving(false)
    }
  }, [detail, onSaveFeedback, reload, saveErrorMessage, saveSuccessMessage])

  const filteredItems = useMemo(() => {
    const query = searchQuery.trim().toLowerCase()
    const source = sortItems ? sortItems([...items]) : [...items]
    if (!query) {
      return source
    }
    return source.filter((item) => {
      const text = (
        getItemSearchText ? getItemSearchText(item) : getItemLabel(item)
      ).toLowerCase()
      return text.includes(query)
    })
  }, [getItemLabel, getItemSearchText, items, searchQuery, sortItems])

  useEffect(() => {
    void reload(undefined, true)
  }, [reload])

  return {
    items,
    selectedId,
    detail,
    originalDetail,
    loading,
    saving,
    searchQuery,
    setSearchQuery,
    filteredItems,
    isDirty,
    setDetail,
    setOriginalDetail,
    reload,
    selectItem,
    handleSave,
  }
}
