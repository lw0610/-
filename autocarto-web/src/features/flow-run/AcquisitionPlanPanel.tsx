'use client'
import { useState } from 'react'
import Link from 'next/link'
import type { AcquisitionItem } from '@/features/flows/types'
import { isDesktopRuntime, pickFilePath } from '@/lib/config-form-helpers'
import { acquisitionChoice, changeItemChoice } from './acquisition'
export default function AcquisitionPlanPanel({
  items,
  value,
  onChange,
  disabled,
}: {
  items: AcquisitionItem[]
  value?: unknown
  onChange?: (value: unknown) => void
  disabled?: boolean
}) {
  const [error, setError] = useState('')
  const choice = acquisitionChoice(value),
    first = items[0],
    estimated = items.reduce(
      (sum, item) =>
        sum + (item.chosen?.cache_hit ? 0 : item.chosen?.estimated_mb || 0),
      0,
    )
  const selectFile = async (item: AcquisitionItem) => {
    try {
      const path = await pickFilePath()
      if (path) onChange?.(changeItemChoice(value, item, 'file', path))
    } catch (error) {
      setError(error instanceof Error ? error.message : String(error))
    }
  }
  if (!items.length)
    return <p className="text-xs text-zinc-500">暂未生成获取计划。</p>
  return (
    <section
      aria-label="数据获取计划"
      className="mt-2 space-y-2 rounded border border-blue-200 bg-blue-50/50 p-2 text-xs dark:border-blue-900 dark:bg-blue-950/20"
    >
      <div className="text-zinc-600 dark:text-zinc-300">
        范围：
        {first.extent?.pending
          ? `${first.extent.dependency_step || '前置步骤'}完成后确定范围`
          : first.extent?.region ||
            first.extent?.bbox?.join(', ') ||
            first.extent?.path ||
            '按需求范围'}
        {first.scale_denominator > 0
          ? ` · 比例尺约 1:${first.scale_denominator.toLocaleString()}`
          : ''}
      </div>
      {items.map((item) => {
        const selected =
            choice.sources?.[item.entry || item.key] ||
            choice.source ||
            item.chosen?.source_id ||
            '',
          provided = choice.files?.[item.entry || item.key] || ''
        return (
          <div
            key={item.key}
            className="space-y-1 rounded border border-zinc-200 bg-white p-2 dark:border-zinc-800 dark:bg-zinc-950"
          >
            <div className="flex justify-between gap-2">
              <strong className="font-medium">
                {item.entry || item.requirement || item.key}
              </strong>
              <span className="text-zinc-500">
                {item.required ? '必需' : '可选'}
              </span>
            </div>
            <p>
              {item.mode === 'user' || provided
                ? '使用已提供文件'
                : item.chosen?.name || '暂不能自动获取'}
              {item.chosen?.license ? `（${item.chosen.license}）` : ''}
            </p>
            {item.chosen?.cache_hit && (
              <p className="text-emerald-700">
                将复用
                {item.chosen.acquired_at
                  ? `${new Date(item.chosen.acquired_at).toLocaleDateString('zh-CN')}获取的`
                  : ''}
                数据仓数据
              </p>
            )}
            {item.chosen?.needs_login && (
              <p className="text-amber-700">需要你登录来源网站</p>
            )}
            {item.chosen?.derived_from && (
              <p className="text-amber-700">
                根据来源数据派生，注意与原始数据的差别。
              </p>
            )}
            {item.chosen?.attribution && (
              <p className="text-zinc-500">署名：{item.chosen.attribution}</p>
            )}
            {(item.notes || []).map((note, index) => (
              <p key={index} className="text-amber-700">
                {note}
              </p>
            ))}
            {onChange && (
              <>
                <label className="flex flex-wrap items-center gap-2">
                  换来源
                  <select
                    aria-label={`${item.entry || item.requirement}的来源`}
                    disabled={disabled || !item.acquirable}
                    value={selected}
                    onChange={(event) =>
                      onChange(
                        changeItemChoice(
                          value,
                          item,
                          'source',
                          event.target.value,
                        ),
                      )
                    }
                    className="min-w-0 max-w-full flex-1 rounded border bg-transparent p-1"
                  >
                    <option value="">自动选择</option>
                    {[
                      ...(item.chosen ? [item.chosen] : []),
                      ...(item.alternatives || []),
                    ].map((source) => (
                      <option key={source.source_id} value={source.source_id}>
                        {source.name} · 优先级 {source.tier} · {source.license}
                        {source.estimated_mb
                          ? ` · 约${source.estimated_mb} MB`
                          : ''}
                      </option>
                    ))}
                    {(item.excluded || []).map((source) => (
                      <option
                        key={source.source_id}
                        value={source.source_id}
                        disabled
                      >
                        {source.name} · {source.reasons.join('；')}
                      </option>
                    ))}
                  </select>
                </label>
                <div className="flex items-center gap-2">
                  <input
                    aria-label={`提供${item.entry || item.requirement}文件`}
                    placeholder="也可填写本机文件路径"
                    className="min-w-0 flex-1 rounded border bg-transparent p-1"
                    disabled={disabled}
                    value={provided}
                    onChange={(event) =>
                      onChange(
                        changeItemChoice(
                          value,
                          item,
                          'file',
                          event.target.value,
                        ),
                      )
                    }
                  />
                  {isDesktopRuntime() && (
                    <button
                      disabled={disabled}
                      className="text-blue-600"
                      onClick={() => void selectFile(item)}
                    >
                      我来提供
                    </button>
                  )}
                </div>
              </>
            )}
            {!item.available && !provided && (
              <div className="text-amber-700">
                暂不能自动获取：
                {(item.reasons || []).join('；') || '没有可用来源'}{' '}
                <Link className="underline" href="/settings/acquisition">
                  启用扩展服务 / 配置密钥
                </Link>
                {!item.acquirable && (
                  <p>该行只能由用户提供；填写类别后可自动获取。</p>
                )}
              </div>
            )}
          </div>
        )
      })}
      <p className="text-zinc-500">
        预计下载：约 {estimated.toFixed(1)} MB（估计值，以实际来源为准）
      </p>
      {error && (
        <p role="alert" className="text-red-600">
          {error}
        </p>
      )}
    </section>
  )
}
