'use client'
import { useEffect, useRef, useState } from 'react'
import { flowApi } from './api'
import type { RoleMappingResponse, ToolCapability } from './types'
import { buttonClass, fieldClass } from './PreviewPanels'

import { roleSourceLabel } from './roleLabels'
export default function RoleMappingPanel({
  id,
  scheme,
  capabilities = [],
  disabled,
  onSaved,
}: {
  id: string
  scheme: string
  capabilities?: ToolCapability[]
  disabled?: boolean
  onSaved: () => Promise<void>
}) {
  const [data, setData] = useState<RoleMappingResponse | null>(null),
    [mapping, setMapping] = useState<Record<string, string>>({}),
    [error, setError] = useState(''),
    [notice, setNotice] = useState(''),
    [loading, setLoading] = useState(true),
    [saving, setSaving] = useState(false)
  const generation = useRef(0)
  useEffect(() => {
    const lifecycle = generation
    const ticket = ++generation.current
    setData(null)
    setLoading(true)
    setError('')
    setNotice('')
    void flowApi
      .roles(id, scheme)
      .then((value) => {
        if (ticket === generation.current) {
          setData(value)
          setMapping(value.role_mapping || {})
        }
      })
      .catch((error) => {
        if (ticket === generation.current) setError(String(error))
      })
      .finally(() => {
        if (ticket === generation.current) setLoading(false)
      })
    return () => {
      lifecycle.current++
    }
  }, [id, scheme])
  const packages = new Map(
    capabilities
      .filter((item) => item.package_id)
      .map((item) => [
        item.package_id!,
        item.package_display_name || item.package_id!,
      ]),
  )
  const save = async (next: Record<string, string>) => {
    const ticket = generation.current
    setSaving(true)
    setError('')
    setNotice('')
    try {
      const result = await flowApi.setRoles(id, scheme, next)
      if (ticket !== generation.current) return
      setData(result)
      setMapping(result.role_mapping || {})
      setNotice('角色对应已保存。')
      await onSaved()
    } catch (error) {
      if (ticket === generation.current)
        setError(`保存或刷新失败：${String(error)}`)
    } finally {
      if (ticket === generation.current) setSaving(false)
    }
  }
  if (loading) return <p className="text-xs">正在读取角色对应…</p>
  return (
    <div className="space-y-3 text-xs">
      <p className="text-zinc-500">
        当前方案：{scheme}。角色对应保存到此方案，内置流程也可以指定对应。
      </p>
      {error && (
        <p role="alert" className="text-red-600">
          {error}
        </p>
      )}
      {notice && (
        <p role="status" className="text-emerald-700">
          {notice}
        </p>
      )}
      {data?.role_slots.map((slot) => (
        <div
          key={slot.name}
          className="space-y-2 rounded border border-zinc-200 p-3 dark:border-zinc-800"
        >
          <strong>{slot.name}</strong>
          <p>{slot.description}</p>
          <p>
            所需工具包：
            {slot.packages.map((id) => packages.get(id) || id).join('、') ||
              '无'}
            {slot.needs_vision ? ' · 需要读图' : ''}
          </p>
          <p>
            当前对应：
            {slot.resolution.role_label || slot.resolution.role_id || '未对应'}{' '}
            · {roleSourceLabel(slot.resolution, packages)}
          </p>
          {slot.resolution.note && (
            <p className="text-zinc-500">{slot.resolution.note}</p>
          )}
          <label className="block">
            {slot.name}对应
            <select
              className={fieldClass}
              disabled={disabled || saving}
              value={mapping[slot.name] || ''}
              onChange={(event) =>
                setMapping((previous) => ({
                  ...previous,
                  [slot.name]: event.target.value,
                }))
              }
            >
              <option value="">自动</option>
              <optgroup label="方案角色">
                {data.scheme_roles.map((role) => (
                  <option
                    key={role.id}
                    value={role.id}
                    disabled={!role.enabled}
                  >
                    {role.display_name || role.id}
                    {!role.enabled ? '（已禁用）' : ''}
                  </option>
                ))}
              </optgroup>
              <option value="flow_role">流程自带角色</option>
              {mapping[slot.name] &&
                mapping[slot.name] !== 'flow_role' &&
                !data.scheme_roles.some(
                  (role) => role.id === mapping[slot.name],
                ) && (
                  <option value={mapping[slot.name]}>
                    已失效的角色：{mapping[slot.name]}
                  </option>
                )}
            </select>
          </label>
        </div>
      ))}
      {data && !data.role_slots.length && <p>此流程没有需要对应的参与角色。</p>}
      {disabled && (
        <p className="text-zinc-500">请先保存流程草稿，再修改角色对应。</p>
      )}
      {data && (
        <div className="flex gap-2">
          <button
            className={buttonClass}
            disabled={disabled || saving}
            onClick={() => void save(mapping)}
          >
            {saving ? '保存中…' : '保存角色对应'}
          </button>
          <button
            className={buttonClass}
            disabled={disabled || saving}
            onClick={() => void save({})}
          >
            恢复全部自动
          </button>
        </div>
      )}
    </div>
  )
}
