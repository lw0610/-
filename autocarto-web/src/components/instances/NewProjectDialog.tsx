'use client'

import { useEffect, useMemo, useState } from 'react'
import { createPortal } from 'react-dom'
import { buildProjectFilePath, validateProjectName } from '@/lib/qgisProjects'

interface SubmitPayload {
  directoryPath: string
  projectName: string
}

interface NewProjectDialogProps {
  open: boolean
  loading?: boolean
  onClose: () => void
  onPickDirectory: () => Promise<string | null>
  onSubmit: (payload: SubmitPayload) => Promise<void>
}

export default function NewProjectDialog({
  open,
  loading = false,
  onClose,
  onPickDirectory,
  onSubmit,
}: NewProjectDialogProps) {
  const [directoryPath, setDirectoryPath] = useState('')
  const [projectName, setProjectName] = useState('')
  const [error, setError] = useState('')

  useEffect(() => {
    if (!open) {
      setDirectoryPath('')
      setProjectName('')
      setError('')
      return
    }

    const handleKeydown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !loading) {
        onClose()
      }
    }

    window.addEventListener('keydown', handleKeydown)
    return () => window.removeEventListener('keydown', handleKeydown)
  }, [loading, onClose, open])

  const targetPath = useMemo(() => {
    if (!directoryPath.trim() || !projectName.trim()) {
      return ''
    }
    return buildProjectFilePath(directoryPath, projectName, '.qgz')
  }, [directoryPath, projectName])

  if (!open || typeof document === 'undefined') {
    return null
  }

  const handlePickDirectory = async () => {
    setError('')
    try {
      const selected = await onPickDirectory()
      if (selected) {
        setDirectoryPath(selected)
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : '选择目录失败')
    }
  }

  const handleSubmit = async () => {
    const trimmedDirectoryPath = directoryPath.trim()
    const trimmedProjectName = projectName.trim()
    const projectNameError = validateProjectName(trimmedProjectName)

    if (!trimmedDirectoryPath) {
      setError('请选择工程目录')
      return
    }
    if (projectNameError) {
      setError(projectNameError)
      return
    }

    setError('')
    try {
      await onSubmit({
        directoryPath: trimmedDirectoryPath,
        projectName: trimmedProjectName,
      })
    } catch (e) {
      setError(e instanceof Error ? e.message : '创建工程失败')
    }
  }

  return createPortal(
    <div
      className="fixed inset-0 z-[82] flex items-center justify-center bg-zinc-950/55 px-3 backdrop-blur-sm"
      onClick={() => {
        if (!loading) {
          onClose()
        }
      }}
    >
      <div
        className="w-full max-w-lg rounded border border-zinc-200/80 bg-white/95 p-3 shadow-[0_2px_8px_rgba(0,0,0,0.08)] dark:border-zinc-800 dark:bg-zinc-900/95 dark:shadow-black/30"
        role="dialog"
        aria-modal="true"
        aria-label="新建 QGIS 工程"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="text-xs font-semibold text-zinc-900 dark:text-zinc-100">新建 QGIS 工程</div>
        <div className="mt-1 text-xs text-zinc-600 dark:text-zinc-300">
          选择工程目录并输入工程名称，系统会自动启动新的 GIS 实例并创建工程。
        </div>

        <div className="mt-4 space-y-3">
          <div>
            <div className="mb-1 text-xs font-medium text-zinc-600 dark:text-zinc-300">工程目录</div>
            <div className="flex gap-2">
              <input
                className="min-w-0 flex-1 rounded border border-zinc-200 bg-zinc-50/70 px-2.5 py-2 text-xs text-zinc-800 outline-none transition-colors focus:border-blue-300 focus:bg-white dark:border-zinc-700 dark:bg-zinc-950 dark:text-zinc-100 dark:focus:border-blue-700"
                value={directoryPath}
                onChange={(event) => setDirectoryPath(event.target.value)}
                placeholder="请选择工程目录"
                aria-label="工程目录"
                disabled={loading}
              />
              <button
                type="button"
                className="rounded border border-zinc-300 px-3 py-2 text-xs hover:bg-zinc-100 disabled:opacity-50 dark:border-zinc-700 dark:hover:bg-zinc-800"
                onClick={() => {
                  void handlePickDirectory()
                }}
                disabled={loading}
              >
                选择目录
              </button>
            </div>
          </div>

          <div>
            <div className="mb-1 text-xs font-medium text-zinc-600 dark:text-zinc-300">工程名称</div>
            <input
              className="w-full rounded border border-zinc-300 px-2 py-2 text-xs dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100"
              value={projectName}
              onChange={(event) => setProjectName(event.target.value)}
              placeholder="例如：landuse-analysis"
              aria-label="工程名称"
              disabled={loading}
              autoFocus
              onKeyDown={(event) => {
                if (event.key === 'Enter') {
                  event.preventDefault()
                  void handleSubmit()
                }
              }}
            />
          </div>

          {targetPath && (
            <div className="rounded border border-zinc-200 bg-zinc-50 px-3 py-2 text-xs text-zinc-600 dark:border-zinc-700 dark:bg-zinc-950 dark:text-zinc-300">
              目标文件：{targetPath}
            </div>
          )}

          {error && (
            <div className="rounded border border-red-300 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">
              {error}
            </div>
          )}
        </div>

        <div className="mt-4 flex items-center justify-end gap-2">
          <button
            className="rounded border border-zinc-300 px-3 py-1.5 text-xs text-zinc-700 hover:bg-zinc-100 disabled:opacity-50 dark:border-zinc-600 dark:text-zinc-200 dark:hover:bg-zinc-800"
            onClick={onClose}
            disabled={loading}
          >
            取消
          </button>
          <button
            className="rounded border border-blue-300 px-3 py-1.5 text-xs text-blue-600 hover:bg-blue-50 disabled:opacity-50 dark:border-blue-900/60 dark:text-blue-300 dark:hover:bg-blue-900/30"
            onClick={() => {
              void handleSubmit()
            }}
            disabled={loading}
          >
            {loading ? '创建中...' : '创建并打开'}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
