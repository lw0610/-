import type { AcquisitionState } from './types'
import FlowOutputs from './FlowOutputs'
const labels: Record<string, string> = {
  pending: '等待',
  running: '获取中',
  waiting_extent: '等待范围',
  waiting_user: '等待加载确认',
  downloading: '下载中',
  download: '下载中',
  normalizing: '规范化',
  normalize: '规范化',
  verifying: '核验',
  verify: '核验',
  inspecting: '核验',
  cached: '已复用',
  reused: '已复用',
  user_provided: '已提供',
  completed: '已完成',
  done: '已完成',
  failed: '失败',
  skipped: '已跳过',
  retrying: '切换来源重试',
  saving: '正在保存',
  partial: '部分完成',
  cancelled: '已取消',
  waiting: '等待响应',
  preparing: '准备获取',
}
export default function AcquisitionProgress({
  state,
  requirement,
  finished = false,
}: {
  state: AcquisitionState
  requirement?: string
  finished?: boolean
}) {
  const planned = (state.plan?.items || []).filter(
    (item) => !requirement || item.requirement === requirement,
  )
  const keys = new Set(planned.map((item) => item.key))
  const items = state.items.filter((item) => !requirement || keys.has(item.key))
  if (!planned.length && !items.length) return null
  return (
    <details
      open
      className="mt-2 rounded border border-blue-100 p-2 text-xs dark:border-blue-900"
    >
      <summary className="cursor-pointer font-medium">
        {finished ? '自动获取的数据' : '数据获取进度'}
      </summary>
      <div className="mt-2 space-y-2">
        {[
          ...new Set([
            ...planned.map((item) => item.key),
            ...items.map((item) => item.key),
          ]),
        ].map((key) => {
          const plan = planned.find((item) => item.key === key),
            item = items.find((item) => item.key === key),
            status =
              item?.status ||
              (plan?.extent?.pending ? 'waiting_extent' : 'pending'),
            percent =
              item?.total && item.total > 0
                ? Math.min(
                    100,
                    Math.max(
                      0,
                      Math.round(((item.bytes || 0) / item.total) * 100),
                    ),
                  )
                : undefined
          return (
            <div key={key} className="rounded bg-zinc-50 p-2 dark:bg-zinc-900">
              <div className="flex justify-between gap-2">
                <strong className="font-medium">
                  {plan?.entry || plan?.requirement || key}
                </strong>
                <span>
                  {labels[status] || '处理中'}
                  {percent != null ? ` ${percent}%` : ''}
                </span>
              </div>
              {(item?.source_id || plan?.chosen?.name) && <p className="mt-1 text-zinc-500">
                来源：
                {plan?.chosen?.source_id === item?.source_id
                  ? plan?.chosen?.name
                  : item?.source_id || plan?.chosen?.name || '等待确定'}
              </p>}
              {item?.message && <p className="mt-1">{item.message}</p>}
              {!!item?.bytes && <p className="mt-1">已接收 {(item.bytes / 1024 / 1024).toFixed(2)} MB{!item.total && ' · 总大小未知，无法计算百分比'}{typeof item.bytes_per_second === 'number' && ` · ${(item.bytes_per_second / 1024).toFixed(1)} KB/s`}</p>}
              {item?.error && (
                <p role="alert" className="mt-1 text-amber-700">
                  {item.error}
                </p>
              )}
              {item?.notes?.map((note, index) => (
                <p key={index} className="mt-1 text-amber-700">
                  {note}
                </p>
              ))}
              {finished && item?.output?.paths?.length && (
                <FlowOutputs outputs={{ 文件: item.output.paths }} />
              )}
            </div>
          )
        })}
      </div>
    </details>
  )
}
