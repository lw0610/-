'use client'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import {
  isDesktopRuntime,
  pickDirectoryPath,
  pickFilePath,
} from '@/lib/config-form-helpers'
import {
  Dialog,
  DialogBackdrop,
  DialogPanel,
  DialogTitle,
} from '@headlessui/react'
import { buttonClass, fieldClass } from './PreviewPanels'
export interface DialogField {
  key: string
  label: string
  initial?: string
  optional?: boolean
  picker?: 'file' | 'directory' | 'both'
  options?: { value: string; label: string }[]
}
export default function PathDialog({
  title,
  fields,
  checks = [],
  pendingMessage,
  description,
  submitLabel = '确定',
  submitDisabled = false,
  onClose,
  onSubmit,
}: {
  title: string
  fields: DialogField[]
  checks?: { key: string; label: string; initial?: boolean }[]
  pendingMessage?: string
  description?: ReactNode
  submitLabel?: string
  submitDisabled?: boolean
  onClose: () => void
  onSubmit: (
    values: Record<string, string>,
    checks: Record<string, boolean>,
  ) => Promise<void>
}) {
  const [values, setValues] = useState<Record<string, string>>(
    Object.fromEntries(fields.map((item) => [item.key, item.initial || ''])),
  )
  const [checked, setChecked] = useState<Record<string, boolean>>(
    Object.fromEntries(checks.map(item => [item.key, Boolean(item.initial)])),
  )
  const [error, setError] = useState(''),
    [busy, setBusy] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const dialogRef = useRef<HTMLFormElement>(null)
  useEffect(() => {
    dialogRef.current?.querySelector<HTMLElement>('input, select')?.focus()
  }, [])
  const browse = async (key: string, directory: boolean) => {
    setBusy(true)
    try {
      const value = await (directory ? pickDirectoryPath() : pickFilePath())
      if (value) setValues((current) => ({ ...current, [key]: value }))
    } catch (err) {
      setError(err instanceof Error ? err.message : '选择路径失败')
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog
      open
      onClose={() => {
        if (!busy) onClose()
      }}
      className="relative z-50"
    >
      <DialogBackdrop className="fixed inset-0 bg-black/30" />
      <div className="fixed inset-0 flex items-center justify-center p-4">
        <DialogPanel
          as="form"
          ref={dialogRef}
          className="w-full max-w-lg rounded-lg border border-zinc-200 bg-white p-5 shadow-xl text-text-primary dark:border-zinc-700 dark:bg-zinc-900"
          onSubmit={async (event) => {
            event.preventDefault()
            if (busy || submitDisabled) return
            setBusy(true)
            setSubmitting(true)
            setError('')
            try {
              await onSubmit(values, checked)
              onClose()
            } catch (err) {
              setError(err instanceof Error ? err.message : '操作失败')
            } finally {
              setBusy(false)
              setSubmitting(false)
            }
          }}
          onKeyDown={(event) => {
            if (event.key === 'Escape' && !busy) onClose()
          }}
        >
          <DialogTitle className="text-base font-semibold">{title}</DialogTitle>
          {description && <div className="mt-3 text-sm">{description}</div>}
          {fields.map((item) => (
            <div key={item.key} className="mt-4">
              <label className="text-sm">
                {item.label}
                {item.options ? <select
                  name={item.key}
                  required={!item.optional}
                  disabled={busy}
                  className={`${fieldClass} mt-1`}
                  value={values[item.key]}
                  onChange={event => setValues({ ...values, [item.key]: event.target.value })}
                >
                  <option value="">请选择</option>
                  {item.options.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}
                </select> : <input
                  name={item.key}
                  required={!item.optional}
                  disabled={busy}
                  className={`${fieldClass} mt-1`}
                  value={values[item.key]}
                  onChange={(event) =>
                    setValues({ ...values, [item.key]: event.target.value })
                  }
                />}
              </label>
              {item.picker && isDesktopRuntime() && (
                <div className="mt-2 flex gap-2">
                  {item.picker !== 'directory' && (
                    <button
                      type="button"
                      disabled={busy}
                      className={buttonClass}
                      onClick={() => void browse(item.key, false)}
                    >
                      选择文件
                    </button>
                  )}
                  {item.picker !== 'file' && (
                    <button
                      type="button"
                      disabled={busy}
                      className={buttonClass}
                      onClick={() => void browse(item.key, true)}
                    >
                      选择文件夹
                    </button>
                  )}
                </div>
              )}
            </div>
          ))}
          {checks.map((item) => (
            <label
              key={item.key}
              className="mt-3 flex items-center gap-2 text-sm"
            >
              <input
                type="checkbox"
                disabled={busy}
                checked={checked[item.key] || false}
                onChange={(event) =>
                  setChecked({ ...checked, [item.key]: event.target.checked })
                }
              />
              {item.label}
            </label>
          ))}
          {submitting && pendingMessage && (
            <p role="status" className="mt-3 text-sm text-zinc-500">
              {pendingMessage}
            </p>
          )}
          {error && (
            <p role="alert" className="mt-3 text-sm text-red-600">
              {error}
            </p>
          )}
          <div className="mt-5 flex justify-end gap-2">
            <button
              type="button"
              disabled={busy}
              className={buttonClass}
              onClick={onClose}
            >
              取消
            </button>
            <button type="submit" disabled={busy || submitDisabled} className={buttonClass}>
              {busy ? '处理中…' : submitLabel}
            </button>
          </div>
        </DialogPanel>
      </div>
    </Dialog>
  )
}
