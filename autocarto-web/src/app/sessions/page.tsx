'use client'

import { type ChangeEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ArchiveIcon } from '@/components/common/AppIcon'
import AppShell from '@/components/layout/AppShell'
import StatusBadge from '@/components/settings/StatusBadge'
import Chip from '@/components/common/Chip'
import { ActionButton } from '@/components/settings/ActionBar'
import DropdownMenu from '@/components/settings/DropdownMenu'
import EmptyStatePanel from '@/components/settings/EmptyStatePanel'
import StateBlock from '@/components/common/StateBlock'
import api, { SessionRecord } from '@/lib/api'
import { parseExecutionMode } from '@/lib/runIntent'
import { useSessionStore } from '@/stores/sessionStore'
import { useInstanceStore } from '@/stores/instanceStore'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import { deriveProjectName } from '@/lib/qgisProjects'
import {
  asInt,
  asRecord,
	loadSessionsForProject,
  parseSnapshots,
  sanitizeFileName,
} from '@/lib/session-utils'

function formatTime(value?: string) {
  if (!value) {
    return '-'
  }
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) {
    return value
  }
  return parsed.toLocaleString('zh-CN')
}

function formatExecutionMode(value: unknown) {
  const raw = String(value || '').trim().toLowerCase()
  const parsed = parseExecutionMode(raw)
  if (parsed) {
    return parsed.toUpperCase()
  }
  if (raw) {
    return `${raw}（无效）`
  }
  return '未设置'
}

function normalizeTags(input: string[]) {
  const deduped = new Set<string>()
  input.forEach((item) => {
    const tag = item.trim()
    if (!tag) {
      return
    }
    deduped.add(tag)
  })
  return Array.from(deduped)
}

interface ConfirmDialogState {
  open: boolean
  title: string
  description: string
  confirmText: string
  danger: boolean
  action: (() => Promise<void> | void) | null
}

const emptyConfirmDialogState: ConfirmDialogState = {
  open: false,
  title: '',
  description: '',
  confirmText: '确认',
  danger: false,
  action: null,
}

export default function SessionsPage() {
  const { sessions, activeSessionId, setSessions, setActiveSession, removeSession, upsertSession, notifySessionMutated } = useSessionStore()
  const { instances, activeInstanceId, fetchInstances } = useInstanceStore()
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const [newSessionName, setNewSessionName] = useState('')
  const [snapshotReason, setSnapshotReason] = useState('manual')
  const [selectedSnapshotId, setSelectedSnapshotId] = useState('')
  const [sessionSearch, setSessionSearch] = useState('')
  const [sessionFilter, setSessionFilter] = useState<'active' | 'archived'>('active')
  const [nameEditing, setNameEditing] = useState(false)
  const [renameValue, setRenameValue] = useState('')
  const [tagInput, setTagInput] = useState('')
  const [descriptionDraft, setDescriptionDraft] = useState('')
  const [maxSnapshotsPerSession, setMaxSnapshotsPerSession] = useState(10)
  const [batchMode, setBatchMode] = useState(false)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [messagesExpanded, setMessagesExpanded] = useState(false)
  const [batchProgress, setBatchProgress] = useState('')
  const [confirmDialog, setConfirmDialog] = useState<ConfirmDialogState>(emptyConfirmDialogState)
  const [confirmingAction, setConfirmingAction] = useState(false)
  const importInputRef = useRef<HTMLInputElement | null>(null)

  const openConfirmDialog = useCallback((payload: {
    title: string
    description: string
    confirmText?: string
    danger?: boolean
    action: () => Promise<void> | void
  }) => {
    setConfirmDialog({
      open: true,
      title: payload.title,
      description: payload.description,
      confirmText: payload.confirmText || '确认',
      danger: Boolean(payload.danger),
      action: payload.action,
    })
  }, [])

  const closeConfirmDialog = useCallback(() => {
    if (confirmingAction) {
      return
    }
    setConfirmDialog(emptyConfirmDialogState)
  }, [confirmingAction])

  const runConfirmDialogAction = async () => {
    if (!confirmDialog.action) {
      closeConfirmDialog()
      return
    }
    setConfirmingAction(true)
    try {
      await confirmDialog.action()
    } finally {
      setConfirmingAction(false)
      setConfirmDialog(emptyConfirmDialogState)
    }
  }

  useEffect(() => {
    fetchInstances()
  }, [fetchInstances])

  useEffect(() => {
    let cancelled = false
    api.getConfig().then((cfg) => {
      if (cancelled) {
        return
      }
      const sessionCfg = asRecord(cfg.session)
      setMaxSnapshotsPerSession(asInt(sessionCfg.max_snapshots_per_session, 10))
    }).catch(() => {
      if (!cancelled) {
        setMaxSnapshotsPerSession(10)
      }
    })

    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    if (!error) {
      return
    }
    const timer = setTimeout(() => setError(''), 3000)
    return () => clearTimeout(timer)
  }, [error])

  useEffect(() => {
    if (!success) {
      return
    }
    const timer = setTimeout(() => setSuccess(''), 3000)
    return () => clearTimeout(timer)
  }, [success])

  const activeInstance = useMemo(
    () => instances.find((item) => item.id === activeInstanceId) || null,
    [instances, activeInstanceId],
  )
  const projectPath = activeInstance?.project_path || ''
  const currentProjectName = useMemo(
    () => (projectPath ? deriveProjectName(projectPath) : ''),
    [projectPath],
  )

  const loadSessions = useCallback(async () => {
    setLoading(true)
    setError('')
    setSuccess('')
    try {
		const list = await loadSessionsForProject((path) => api.getSessions(path), projectPath)
      setSessions(list)
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败')
    } finally {
      setLoading(false)
    }
  }, [projectPath, setSessions])

  useEffect(() => {
    loadSessions()
  }, [loadSessions])

  const filteredSessions = useMemo(() => {
    const keyword = sessionSearch.trim().toLowerCase()
    return sessions.filter((session) => {
      if (sessionFilter === 'archived') {
        if (!session.archived) {
          return false
        }
      } else if (session.archived) {
        return false
      }

      if (!keyword) {
        return true
      }
      const name = String(session.name || '').toLowerCase()
      const preview = String(session.last_message_preview || '').toLowerCase()
      return name.includes(keyword) || preview.includes(keyword)
    })
  }, [sessionFilter, sessionSearch, sessions])

  const filteredSessionIds = useMemo(
    () => filteredSessions.map((item) => item.session_id),
    [filteredSessions],
  )

  const allFilteredSelected = filteredSessionIds.length > 0 && filteredSessionIds.every((id) => selectedIds.has(id))

  const selectedSession = sessions.find((item) => item.session_id === activeSessionId) || null
  const snapshots = parseSnapshots(selectedSession?.snapshots)

  useEffect(() => {
    if (!batchMode && selectedIds.size > 0) {
      setSelectedIds(new Set())
    }
  }, [batchMode, selectedIds.size])

  useEffect(() => {
    setSelectedIds((prev) => {
      if (prev.size === 0) {
        return prev
      }

      const validIds = new Set(sessions.map((item) => item.session_id))
      let changed = false
      const next = new Set<string>()
      prev.forEach((id) => {
        if (validIds.has(id)) {
          next.add(id)
        } else {
          changed = true
        }
      })
      return changed ? next : prev
    })
  }, [sessions])

  useEffect(() => {
    if (!selectedSession) {
      setSelectedSnapshotId('')
      return
    }
    if (!snapshots.some((snapshot) => snapshot.snapshot_id === selectedSnapshotId)) {
      setSelectedSnapshotId(snapshots[0]?.snapshot_id || '')
    }
  }, [selectedSession, selectedSnapshotId, snapshots])

  useEffect(() => {
    setNameEditing(false)
    setRenameValue(selectedSession?.name || '')
    setDescriptionDraft(selectedSession?.description || '')
    setTagInput('')
  }, [selectedSession?.session_id, selectedSession?.name, selectedSession?.description])

  useEffect(() => {
    setMessagesExpanded(false)
  }, [selectedSession?.session_id])

  const createSession = async () => {
    setSaving(true)
    setError('')
    setSuccess('')
    try {
      const created = await api.createSession(
        {
          name: newSessionName.trim() || undefined,
          project_path: projectPath,
        },
        projectPath,
      )
      upsertSession(created)
      setActiveSession(created.session_id)
      setNewSessionName('')
      setSuccess('会话已创建')
    } catch (e) {
      setError(e instanceof Error ? e.message : '创建失败')
    } finally {
      setSaving(false)
    }
  }

  const deleteSelected = () => {
    if (!selectedSession) {
      return
    }
    const target = selectedSession
    openConfirmDialog({
      title: '确认删除会话',
      description: `确定删除会话 ${target.name || target.session_id} 吗？聊天记录及会话内下载、生成的文件将永久删除；另存到工程或其他目录的文件不会删除。此操作不可撤销。`,
      confirmText: '确认删除',
      danger: true,
      action: async () => {
        setSaving(true)
        setError('')
        setSuccess('')
        try {
          await api.deleteSession(target.session_id, projectPath)
          removeSession(target.session_id)
          setSuccess('会话已删除')
        } catch (e) {
          setError(e instanceof Error ? e.message : '删除失败')
        } finally {
          setSaving(false)
        }
      },
    })
  }

  const patchSelected = async (patch: Partial<Pick<SessionRecord, 'name' | 'archived' | 'description' | 'tags'>>) => {
    if (!selectedSession) {
      return
    }
    setSaving(true)
    setError('')
    setSuccess('')
    try {
      const updated = await api.patchSession(selectedSession.session_id, patch, projectPath)
      upsertSession(updated)
      setSuccess('会话已更新')
    } catch (e) {
      setError(e instanceof Error ? e.message : '更新失败')
    } finally {
      setSaving(false)
    }
  }

  const startRename = () => {
    if (!selectedSession) {
      return
    }
    setRenameValue(selectedSession.name || '')
    setNameEditing(true)
  }

  const cancelRename = () => {
    setNameEditing(false)
    setRenameValue(selectedSession?.name || '')
  }

  const saveRename = async () => {
    if (!selectedSession) {
      return
    }
    const next = renameValue.trim()
    if (!next || next === selectedSession.name) {
      setNameEditing(false)
      return
    }
    await patchSelected({ name: next })
    setNameEditing(false)
  }

  const addTag = async () => {
    if (!selectedSession) {
      return
    }
    const nextTag = tagInput.trim()
    if (!nextTag) {
      return
    }
    const current = Array.isArray(selectedSession.tags) ? selectedSession.tags : []
    const tags = normalizeTags([...current, nextTag])
    if (tags.length === current.length) {
      setTagInput('')
      return
    }
    await patchSelected({ tags })
    setTagInput('')
  }

  const removeTag = async (tag: string) => {
    if (!selectedSession) {
      return
    }
    const current = Array.isArray(selectedSession.tags) ? selectedSession.tags : []
    const tags = current.filter((item) => item !== tag)
    await patchSelected({ tags })
  }

  const saveDescription = async () => {
    if (!selectedSession) {
      return
    }
    const next = descriptionDraft.trim()
    const current = String(selectedSession.description || '').trim()
    if (next === current) {
      return
    }
    await patchSelected({ description: next })
  }

  const toggleSelectAllFiltered = () => {
    if (filteredSessionIds.length === 0) {
      return
    }
    setSelectedIds((prev) => {
      const next = new Set(prev)
      const shouldUnselect = filteredSessionIds.every((id) => next.has(id))
      if (shouldUnselect) {
        filteredSessionIds.forEach((id) => next.delete(id))
      } else {
        filteredSessionIds.forEach((id) => next.add(id))
      }
      return next
    })
  }

  const toggleSessionSelected = (sessionId: string, checked: boolean) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (checked) {
        next.add(sessionId)
      } else {
        next.delete(sessionId)
      }
      return next
    })
  }

  const batchArchiveSelected = async () => {
    const ids = Array.from(selectedIds)
    if (ids.length === 0) {
      return
    }

    setSaving(true)
    setError('')
    setSuccess('')
    setBatchProgress('')
    let successCount = 0
    let failedCount = 0

    try {
      for (const [index, sessionId] of ids.entries()) {
        setBatchProgress(`正在归档 ${index + 1}/${ids.length}...`)
        try { const updated = await api.patchSession(sessionId, { archived: true }, projectPath)
        upsertSession(updated)
        successCount += 1 } catch (error) { void error; failedCount += 1 }
      }
    } finally {
      setSaving(false)
    }

    setSelectedIds(new Set())
    setBatchProgress('')
    if (failedCount > 0) {
      setError(`批量归档完成：成功 ${successCount}，失败 ${failedCount}`)
    } else {
      setSuccess(`已归档 ${successCount} 个会话`)
    }
  }

  const batchUnarchiveSelected = async () => {
    const ids = Array.from(selectedIds)
    if (ids.length === 0) {
      return
    }

    setSaving(true)
    setError('')
    setSuccess('')
    setBatchProgress('')
    let successCount = 0
    let failedCount = 0

    try {
      for (const [index, sessionId] of ids.entries()) {
        setBatchProgress(`正在取消归档 ${index + 1}/${ids.length}...`)
        try { const updated = await api.patchSession(sessionId, { archived: false }, projectPath)
        upsertSession(updated)
        successCount += 1 } catch (error) { void error; failedCount += 1 }
      }
    } finally {
      setSaving(false)
    }

    setSelectedIds(new Set())
    setBatchProgress('')
    if (failedCount > 0) {
      setError(`批量取消归档完成：成功 ${successCount}，失败 ${failedCount}`)
    } else {
      setSuccess(`已取消归档 ${successCount} 个会话`)
    }
  }

  const batchDeleteSelected = () => {
    const ids = Array.from(selectedIds)
    if (ids.length === 0) {
      return
    }
    openConfirmDialog({
      title: '确认批量删除',
      description: `确定删除选中的 ${ids.length} 个会话吗？聊天记录及会话内下载、生成的文件将永久删除；另存到工程或其他目录的文件不会删除。此操作不可撤销。`,
      confirmText: '确认删除',
      danger: true,
      action: async () => {
        setSaving(true)
        setError('')
        setSuccess('')
        setBatchProgress('')
        let successCount = 0
        let failedCount = 0

        try {
          for (const [index, sessionId] of ids.entries()) {
            setBatchProgress(`正在删除 ${index + 1}/${ids.length}...`)
            try { await api.deleteSession(sessionId, projectPath)
            removeSession(sessionId)
            successCount += 1 } catch (error) { void error; failedCount += 1 }
          }
        } finally {
          setSaving(false)
        }

        setSelectedIds(new Set())
        setBatchProgress('')
        if (failedCount > 0) {
          setError(`批量删除完成：成功 ${successCount}，失败 ${failedCount}`)
        } else {
          setSuccess(`已删除 ${successCount} 个会话`)
        }
      },
    })
  }

  const exportSelectedSession = async () => {
    if (!selectedSession) {
      return
    }

    setSaving(true)
    setError('')
    setSuccess('')

    try {
      const exported = await api.exportSession(selectedSession.session_id, projectPath)
      const text = JSON.stringify(exported, null, 2)
      const blob = new Blob([text], { type: 'application/json;charset=utf-8' })
      const stamp = new Date().toISOString().replace(/[:.]/g, '-').replace('T', '_')
      const baseName = sanitizeFileName(selectedSession.name || selectedSession.session_id)
      const fileName = `${baseName}-${stamp}.json`

      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = fileName
      document.body.appendChild(link)
      link.click()
      document.body.removeChild(link)
      URL.revokeObjectURL(url)

      setSuccess('会话导出成功')
    } catch (e) {
      setError(e instanceof Error ? e.message : '导出失败')
    } finally {
      setSaving(false)
    }
  }

  const triggerImport = () => {
    importInputRef.current?.click()
  }

  const handleImportFile = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    if (!file) {
      return
    }

    setSaving(true)
    setError('')
    setSuccess('')

    try {
      const text = await file.text()
      const parsed: unknown = JSON.parse(text)
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        throw new Error('导入文件格式错误')
      }

      const imported = await api.importSession(parsed as Record<string, unknown>, projectPath)
      upsertSession(imported)
      setActiveSession(imported.session_id)
      await loadSessions()
      setSuccess(`会话已导入：${imported.name || imported.session_id}`)
    } catch (e) {
      setError(e instanceof Error ? e.message : '导入失败')
    } finally {
      setSaving(false)
      event.target.value = ''
    }
  }

  const createSnapshot = async () => {
    if (!selectedSession) {
      return
    }
    setSaving(true)
    setError('')
    setSuccess('')
    try {
      const updated = await api.runSessionOperation(
        selectedSession.session_id,
        {
          type: 'create_snapshot',
          reason: snapshotReason || 'manual',
        },
        projectPath,
      )
      upsertSession(updated)
      notifySessionMutated(updated.session_id)
      const latestSnapshotId = parseSnapshots(updated.snapshots)[0]?.snapshot_id || ''
      setSelectedSnapshotId(latestSnapshotId)
      setSuccess('快照已创建')
    } catch (e) {
      setError(e instanceof Error ? e.message : '创建快照失败')
    } finally {
      setSaving(false)
    }
  }

  const restoreSnapshot = () => {
    if (!selectedSession || !selectedSnapshotId) {
      return
    }
    const sessionId = selectedSession.session_id
    const snapshotId = selectedSnapshotId
    openConfirmDialog({
      title: '确认恢复快照',
      description: `恢复快照 ${snapshotId} 将覆盖当前会话消息。`,
      confirmText: '确认恢复',
      danger: true,
      action: async () => {
        setSaving(true)
        setError('')
        setSuccess('')
        try {
          const updated = await api.runSessionOperation(
            sessionId,
            {
              type: 'restore_snapshot',
              snapshot_id: snapshotId,
              mode: 'strict',
            },
            projectPath,
          )
          upsertSession(updated)
          notifySessionMutated(updated.session_id)
          setSuccess('快照已恢复')
        } catch (e) {
          setError(e instanceof Error ? e.message : '恢复快照失败')
        } finally {
          setSaving(false)
        }
      },
    })
  }

  const deleteSnapshot = () => {
    if (!selectedSession || !selectedSnapshotId) {
      return
    }
    const target = selectedSession
    const snapshotId = selectedSnapshotId
    openConfirmDialog({
      title: '确认删除快照',
      description: `确定删除快照 ${snapshotId} 吗？`,
      confirmText: '确认删除',
      danger: true,
      action: async () => {
        setSaving(true)
        setError('')
        setSuccess('')
        try {
          const remaining = snapshots.filter((snapshot) => snapshot.snapshot_id !== snapshotId)
          const updated: SessionRecord = {
            ...target,
            snapshots: remaining,
            updated_at: new Date().toISOString(),
          }
          await api.saveSession(target.session_id, updated, projectPath)
          upsertSession(updated)
          notifySessionMutated(updated.session_id)
          setSelectedSnapshotId(remaining[0]?.snapshot_id || '')
          setSuccess('快照已删除')
        } catch (e) {
          setError(e instanceof Error ? e.message : '删除快照失败')
        } finally {
          setSaving(false)
        }
      },
    })
  }

  const clearHistory = () => {
    if (!selectedSession) {
      return
    }
    const target = selectedSession
    openConfirmDialog({
      title: '确认清空历史',
      description: '确定清空当前会话历史吗？快照会保留。',
      confirmText: '确认清空',
      danger: true,
      action: async () => {
        setSaving(true)
        setError('')
        setSuccess('')
        try {
          const cleared = {
            ...target,
            messages: [],
            tool_calls: [],
            updated_at: new Date().toISOString(),
          }
          const updated = await api.runSessionOperation(
            target.session_id,
            {
              type: 'clear_history',
            },
            projectPath,
            cleared,
          )
          upsertSession(updated)
          notifySessionMutated(updated.session_id)
          setSuccess('会话历史已清空')
        } catch (e) {
          setError(e instanceof Error ? e.message : '清空历史失败')
        } finally {
          setSaving(false)
        }
      },
    })
  }

  return (
    <AppShell>
      <div className="flex h-full min-h-0 flex-col text-zinc-900 dark:text-zinc-100">
        <div className="flex-1 overflow-y-auto">
          <div className="mx-auto flex min-h-full max-w-6xl flex-col p-3">
            <div className="mb-4 flex items-center justify-between gap-3">
              <div>
                <h2 className="text-[13px] font-bold">会话管理</h2>
                <div className="mt-2 flex flex-wrap items-center gap-2 text-xs">
                  <Chip tone="zinc" size="sm">
                    {activeInstance ? `当前实例：${activeInstance.id}` : '当前未选择实例'}
                  </Chip>
                  <Chip tone="zinc" size="sm">
                    {projectPath ? `当前工程：${currentProjectName}` : '当前未打开工程'}
                  </Chip>
                </div>
                <p className="mt-2 break-all text-xs text-zinc-500 dark:text-zinc-400">
                  {projectPath || '会话管理仅展示当前实例绑定工程的会话。服务连接状态统一查看右上角状态入口。'}
                </p>
              </div>
              <ActionButton
                onClick={loadSessions}
                disabled={loading || saving}
              >
                {loading ? '加载中...' : '刷新'}
              </ActionButton>
            </div>

        {error && (
          <div className="mb-3 rounded border border-red-300 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">
            {error}
          </div>
        )}
        {success && (
          <div className="mb-3 rounded border border-green-300 bg-green-50 px-3 py-2 text-xs text-green-700 dark:border-green-900/60 dark:bg-green-950/30 dark:text-green-300">
            {success}
          </div>
        )}
        {batchProgress && (
          <div className="mb-3 rounded border border-blue-300 bg-blue-50 px-3 py-2 text-xs text-blue-700 dark:border-blue-900/60 dark:bg-blue-950/30 dark:text-blue-300">
            {batchProgress}
          </div>
        )}

        <div className="mb-4 flex flex-wrap items-center gap-2">
          <input
            className="w-72 rounded border border-zinc-300 px-2 py-1 text-xs dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100"
            placeholder="新会话名称（可选）"
            aria-label="新会话名称"
            value={newSessionName}
            onChange={(e) => setNewSessionName(e.target.value)}
          />
          <ActionButton
            onClick={createSession}
            disabled={saving}
          >
            新建会话
          </ActionButton>
          <ActionButton
            variant="danger"
            onClick={deleteSelected}
            disabled={!selectedSession || saving}
          >
            删除
          </ActionButton>
          <DropdownMenu
            label="更多"
            items={[
              { key: 'rename', label: '重命名', disabled: !selectedSession || saving || nameEditing, onClick: startRename },
              { key: 'archive', label: selectedSession?.archived ? '取消归档' : '归档', disabled: !selectedSession || saving, onClick: () => patchSelected({ archived: !Boolean(selectedSession?.archived) }) },
              { key: 'clear', label: '清空历史', disabled: !selectedSession || saving, onClick: clearHistory },
              { key: 'export', label: '导出', disabled: !selectedSession || saving, onClick: exportSelectedSession },
              { key: 'import', label: '导入', disabled: saving, onClick: triggerImport },
              { key: 'sep1', separator: true },
              { key: 'batch', label: batchMode ? '退出批量' : '批量模式', disabled: saving, onClick: () => setBatchMode((prev) => !prev) },
            ]}
          />
          <input
            ref={importInputRef}
            type="file"
            className="hidden"
            accept="application/json,.json"
            onChange={handleImportFile}
          />

          {batchMode && (
            <>
              <ActionButton
                onClick={toggleSelectAllFiltered}
                disabled={saving || filteredSessionIds.length === 0}
              >
                {allFilteredSelected ? '取消全选' : '全选当前筛选'}
              </ActionButton>
              <ActionButton
                onClick={batchArchiveSelected}
                disabled={saving || selectedIds.size === 0}
              >
                批量归档（{selectedIds.size}）
              </ActionButton>
              <ActionButton
                onClick={batchUnarchiveSelected}
                disabled={saving || selectedIds.size === 0}
              >
                批量取消归档（{selectedIds.size}）
              </ActionButton>
              <ActionButton
                variant="danger"
                onClick={batchDeleteSelected}
                disabled={saving || selectedIds.size === 0}
              >
                批量删除（{selectedIds.size}）
              </ActionButton>
            </>
          )}
        </div>

        <div className="grid min-h-0 flex-1 grid-cols-1 gap-2 xl:grid-cols-[320px,minmax(0,1fr)]">
          <div className="flex min-h-[320px] flex-col rounded border border-zinc-200 bg-white px-4 py-3 dark:border-zinc-800 dark:bg-zinc-900 xl:min-h-0">
            <div className="mb-2 space-y-2 px-1">
              <input
                className="w-full rounded border border-zinc-300 px-2 py-1 text-xs dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100"
                placeholder="搜索会话名称或预览"
                aria-label="搜索会话"
                value={sessionSearch}
                onChange={(e) => setSessionSearch(e.target.value)}
              />
              <div className="inline-flex w-full rounded border border-zinc-300 p-0.5 text-[11px] dark:border-zinc-700">
                {([
                  { key: 'active', label: '活跃' },
                  { key: 'archived', label: '归档' },
                ] as const).map((item) => (
                  <button
                    key={item.key}
                    className={`flex-1 rounded px-1.5 py-1 transition-colors ${
                      sessionFilter === item.key
                        ? 'bg-zinc-200 text-zinc-800 dark:bg-zinc-700 dark:text-zinc-200'
                        : 'text-zinc-500 hover:bg-zinc-100 dark:text-zinc-400 dark:hover:bg-zinc-800'
                    }`}
                    aria-pressed={sessionFilter === item.key}
                    onClick={() => setSessionFilter(item.key)}
                  >
                    {item.label}
                  </button>
                ))}
              </div>
            </div>

            <div className="min-h-0 flex-1 overflow-y-auto">
              {sessions.length === 0 && !loading && (
                <div className="p-2">
                  <EmptyStatePanel title="暂无会话" description="当前工程下没有会话记录" compact />
                </div>
              )}
              {sessions.length > 0 && filteredSessions.length === 0 && !loading && (
                <div className="p-2">
                  <EmptyStatePanel title="无匹配会话" description="没有找到符合搜索条件的会话" compact />
                </div>
              )}
              {filteredSessions.map((session) => {
                const checked = selectedIds.has(session.session_id)
                return (
                  <div
                    key={session.session_id}
                    className={`mb-0.5 flex items-start gap-2 rounded-sm border-l-2 px-2 py-1.5 text-left text-xs transition-colors ${
                      session.session_id === activeSessionId
                        ? 'border-l-blue-500 bg-zinc-100 text-zinc-900 dark:border-l-blue-400 dark:bg-zinc-800 dark:text-zinc-100'
                        : 'border-l-transparent hover:bg-zinc-100 dark:hover:bg-zinc-800'
                    }`}
                  >
                  {batchMode && (
                    <input
                      type="checkbox"
                      className="mt-1"
                      aria-label={`选择会话 ${session.name || session.session_id}`}
                      checked={checked}
                      onChange={(e) => {
                        toggleSessionSelected(session.session_id, e.target.checked)
                      }}
                      onClick={(e) => e.stopPropagation()}
                    />
                  )}

                  <button
                    className="min-w-0 flex-1 text-left"
                    onClick={() => setActiveSession(session.session_id)}
                  >
                    <div className="flex items-center gap-1">
                      <span className="font-medium truncate">{session.name || session.session_id}</span>
                      {session.archived && <ArchiveIcon className="h-3 w-3" />}
                    </div>
                    <div className="truncate text-[11px] text-zinc-500 dark:text-zinc-400">
                      {String(session.last_message_preview || '').trim() || '暂无消息'}
                    </div>
                    <div className="text-[11px] text-zinc-500 dark:text-zinc-400">{formatTime(session.updated_at)}</div>
                  </button>
                  </div>
                )
              })}
            </div>
          </div>

          <div className="min-h-[360px] overflow-y-auto rounded border border-zinc-200 bg-white px-4 py-3 dark:border-zinc-800 dark:bg-zinc-900 xl:min-h-0">
            {!selectedSession && (
              <StateBlock 
                title="未选择会话" 
                description="请在左侧列表中选择一个会话以查看详情" 
                className="mt-10"
              />
            )}
            {selectedSession && (
              <>
                <div className="space-y-3 text-xs">
                  <div className="flex flex-wrap items-center gap-2">
                    {!nameEditing && (
                      <>
                        <div className="text-[12px] font-medium">{selectedSession.name}</div>
                        <ActionButton
                          onClick={startRename}
                          disabled={saving}
                        >
                          编辑名称
                        </ActionButton>
                      </>
                    )}
                    {nameEditing && (
                      <>
                        <input
                          className="w-72 rounded border border-zinc-300 px-2 py-1 text-xs dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100"
                          aria-label="重命名会话"
                          value={renameValue}
                          onChange={(e) => setRenameValue(e.target.value)}
                          onKeyDown={(e) => {
                            if (e.key === 'Enter') {
                              e.preventDefault()
                              void saveRename()
                            }
                            if (e.key === 'Escape') {
                              e.preventDefault()
                              cancelRename()
                            }
                          }}
                          autoFocus
                        />
                        <ActionButton
                          onClick={cancelRename}
                          disabled={saving}
                        >
                          取消
                        </ActionButton>
                        <ActionButton
                          variant="primary"
                          onClick={saveRename}
                          disabled={saving}
                        >
                          保存
                        </ActionButton>
                      </>
                    )}
                  </div>

                  <div className="grid grid-cols-1 gap-x-4 gap-y-1 text-xs text-zinc-500 dark:text-zinc-400 md:grid-cols-2 xl:grid-cols-3">
                    <div>会话ID：<span className="text-zinc-800 dark:text-zinc-200">{selectedSession.session_id}</span></div>
                    <div className="flex items-center gap-1.5">状态：<StatusBadge tone={selectedSession.archived ? "warning" : "success"} size="xs">{selectedSession.archived ? '已归档' : '正常'}</StatusBadge></div>
                    <div className="flex items-center gap-1.5">模式：<StatusBadge tone="accent" size="xs">{formatExecutionMode(selectedSession.execution_mode)}</StatusBadge></div>
                    <div>创建：<span className="text-zinc-800 dark:text-zinc-200">{formatTime(selectedSession.created_at)}</span></div>
                    <div>更新：<span className="text-zinc-800 dark:text-zinc-200">{formatTime(selectedSession.updated_at)}</span></div>
                  </div>

                  <div className="flex flex-wrap gap-2">
                    <Chip tone="zinc" size="sm">Token {Number(selectedSession.total_tokens_used || 0)}</Chip>
                    <Chip tone="zinc" size="sm">消息 {Array.isArray(selectedSession.messages) ? selectedSession.messages.length : 0}</Chip>
                    <Chip tone="zinc" size="sm">工具 {Array.isArray(selectedSession.tool_calls) ? selectedSession.tool_calls.length : 0}</Chip>
                    <Chip tone="zinc" size="sm">压缩 {Array.isArray(selectedSession.compression_history) ? selectedSession.compression_history.length : 0}</Chip>
                    <Chip tone="zinc" size="sm">快照 {snapshots.length}</Chip>
                  </div>
                </div>

                <div className="border-t border-zinc-200 pt-3 dark:border-zinc-800">
                  <button
                    className="flex w-full items-center justify-between text-left"
                    onClick={() => setMessagesExpanded((prev) => !prev)}
                  >
                    <span className="font-medium text-xs">消息预览</span>
                    <span className="text-xs text-zinc-500 dark:text-zinc-400">{messagesExpanded ? '收起' : '展开'}</span>
                  </button>

                  {messagesExpanded && (
                    <div className="mt-2 max-h-[400px] space-y-1 overflow-auto text-xs">
                      {(Array.isArray(selectedSession.messages) ? selectedSession.messages : []).length === 0 && (
                        <EmptyStatePanel title="暂无消息" description="该会话还没有任何消息记录" compact />
                      )}

                      {(Array.isArray(selectedSession.messages) ? selectedSession.messages : [])
                        .slice(-50)
                        .map((message, index) => {
                          const payload = asRecord(message)
                          const role = String(payload.role || 'system')
                          const content = String(payload.content || '')
                          const timestamp = String(payload.timestamp || '')
                          const preview = content.length > 200 ? `${content.slice(0, 200)}...` : content
                          return (
                            <div key={`${timestamp}-${index}`} className="rounded border border-zinc-100 px-2 py-1 dark:border-zinc-800">
                              <div className="mb-1 flex items-center justify-between gap-2">
                                <span className={`font-medium ${
                                  role === 'user'
                                    ? 'text-blue-600 dark:text-blue-400'
                                    : role === 'assistant'
                                      ? 'text-green-600 dark:text-green-400'
                                      : 'text-zinc-500 dark:text-zinc-400'
                                }`}
                                >
                                  [{role}]
                                </span>
                                <span className="text-[10px] text-zinc-400 dark:text-zinc-500">{timestamp ? formatTime(timestamp) : '-'}</span>
                              </div>
                              <div className="break-words text-zinc-700 dark:text-zinc-300">{preview || '（空消息）'}</div>
                            </div>
                          )
                        })}
                    </div>
                  )}
                </div>

                <div className="border-t border-zinc-200 pt-3 dark:border-zinc-800">
                  <div className="mb-2 font-medium text-xs">标签</div>
                  <div className="mb-2 flex flex-wrap items-center gap-1">
                    {(Array.isArray(selectedSession.tags) ? selectedSession.tags : []).length === 0 && (
                      <span className="text-xs text-zinc-500 dark:text-zinc-400">暂无标签</span>
                    )}
                    {(Array.isArray(selectedSession.tags) ? selectedSession.tags : []).map((tag) => (
                      <Chip key={tag} tone="zinc" size="sm">
                        <span>{tag}</span>
                        <button
                          className="text-zinc-500 hover:text-red-600 disabled:opacity-50 dark:text-zinc-400 dark:hover:text-red-400"
                          onClick={() => {
                            void removeTag(tag)
                          }}
                          disabled={saving}
                          title="删除标签"
                        >
                          ×
                        </button>
                      </Chip>
                    ))}
                  </div>
                  <div className="flex items-center gap-2">
                    <input
                      className="w-64 rounded border border-zinc-300 px-2 py-1 text-xs dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100"
                      placeholder="输入标签后回车"
                      aria-label="添加标签"
                      value={tagInput}
                      onChange={(e) => setTagInput(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.preventDefault()
                          void addTag()
                        }
                      }}
                    />
                    <ActionButton
                      onClick={addTag}
                      disabled={saving || !tagInput.trim()}
                    >
                      添加
                    </ActionButton>
                  </div>
                </div>

                <div className="border-t border-zinc-200 pt-3 dark:border-zinc-800">
                  <div className="mb-2 font-medium text-xs">描述</div>
                  <textarea
                    className="w-full rounded border border-zinc-300 px-2 py-1 text-xs dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100"
                    rows={4}
                    aria-label="会话描述"
                    value={descriptionDraft}
                    onChange={(e) => setDescriptionDraft(e.target.value)}
                    placeholder="输入会话描述"
                  />
                  <div className="mt-2 flex items-center justify-end gap-2">
                    <ActionButton
                      onClick={() => setDescriptionDraft(selectedSession.description || '')}
                      disabled={saving}
                    >
                      重置
                    </ActionButton>
                    <ActionButton
                      variant="primary"
                      onClick={saveDescription}
                      disabled={saving || descriptionDraft.trim() === String(selectedSession.description || '').trim()}
                    >
                      保存描述
                    </ActionButton>
                  </div>
                </div>

                <div className="border-t border-zinc-200 pt-3 dark:border-zinc-800">
                  <div className="mb-2 flex items-center justify-between">
                    <div className="font-medium text-xs">快照管理</div>
                    <div className="text-xs text-zinc-500 dark:text-zinc-400">共 {snapshots.length} 个快照（上限 {maxSnapshotsPerSession}）</div>
                  </div>

                  <div className="mb-3 flex items-center gap-2 text-xs">
                    <select
                      className="rounded border border-zinc-300 px-2 py-1 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100"
                      value={snapshotReason}
                      onChange={(e) => setSnapshotReason(e.target.value)}
                      disabled={saving}
                    >
                      <option value="manual">manual</option>
                      <option value="auto_compress">auto_compress</option>
                      <option value="clear">clear</option>
                    </select>
                    <ActionButton
                      onClick={createSnapshot}
                      disabled={saving}
                    >
                      创建快照
                    </ActionButton>
                    <ActionButton
                      onClick={restoreSnapshot}
                      disabled={saving || !selectedSnapshotId}
                    >
                      恢复快照
                    </ActionButton>
                    <ActionButton
                      variant="danger"
                      onClick={deleteSnapshot}
                      disabled={saving || !selectedSnapshotId}
                    >
                      删除快照
                    </ActionButton>
                  </div>

                  {snapshots.length === 0 ? (
                    <EmptyStatePanel title="暂无快照" description="该会话还没有创建任何快照" compact />
                  ) : (
                    <div className="max-h-[280px] overflow-auto space-y-1 text-xs">
                      {snapshots.map((snapshot) => {
                        const active = snapshot.snapshot_id === selectedSnapshotId
                        return (
                          <button
                            key={snapshot.snapshot_id}
                            className={`w-full rounded border px-2 py-1 text-left ${
                              active
                                ? 'border-blue-400 bg-blue-50 text-blue-700 dark:border-blue-600 dark:bg-blue-900/30 dark:text-blue-300'
                                : 'border-zinc-200 hover:bg-zinc-50 dark:border-zinc-700 dark:hover:bg-zinc-800'
                            }`}
                            onClick={() => setSelectedSnapshotId(snapshot.snapshot_id)}
                          >
                            <div className="font-medium">{snapshot.snapshot_id}</div>
                            <div className="text-zinc-500 dark:text-zinc-400">
                              {formatTime(snapshot.created_at)}
                              {snapshot.reason ? ` · ${snapshot.reason}` : ''}
                              {typeof snapshot.messages_count === 'number' ? ` · 消息 ${snapshot.messages_count}` : ''}
                            </div>
                          </button>
                        )
                      })}
                    </div>
                  )}
                </div>
              </>
            )}
          </div>
        </div>

        <ConfirmDialog
          open={confirmDialog.open}
          title={confirmDialog.title}
          description={confirmDialog.description}
          confirmText={confirmDialog.confirmText}
          danger={confirmDialog.danger}
          loading={confirmingAction}
          onConfirm={() => {
            void runConfirmDialogAction()
          }}
          onCancel={closeConfirmDialog}
        />
          </div>
        </div>
      </div>
    </AppShell>
  )
}
