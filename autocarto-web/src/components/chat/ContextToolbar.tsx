'use client'

import { Fragment, useMemo } from 'react'
import { Menu, Transition } from '@headlessui/react'
import CustomSelect from '@/components/common/CustomSelect'
import { resolvePlanUnavailableReason } from '@/lib/planModeAvailability'
import { cn } from '@/lib/utils'
import type { LLMProviderConfig } from '@/types/config'
import type { ExecutionMode } from '@/types/message'

type RuntimeLLMModel = Pick<
  LLMProviderConfig,
  | 'name'
  | 'model'
  | 'provider'
  | 'supports_thinking'
  | 'supports_plan_execute'
  | 'supports_forced_tool_choice'
  | 'ready_for_chat'
  | 'ready_for_plan'
  | 'ready_for_auto_title'
  | 'readiness_reason'
  | 'max_context_tokens'
  | 'auto_compress_threshold'
>

interface Props {
  executionMode: ExecutionMode
  thinkingEnabled: boolean
  activeModelName: string
  activeAgentConfig: string
  models: RuntimeLLMModel[]
  agentConfigs: string[]
  messageCount: number
  estimatedTokens: number
  totalChars: number
  planEnabled?: boolean
  planIssue?: string
  onExecutionModeChange: (mode: ExecutionMode) => void
  onThinkingToggle: (enabled: boolean) => void
  onModelSwitch: (modelName: string) => void
  onAgentConfigSwitch: (configName: string) => void
  onCompress: () => void
  onCreateSnapshot: () => void
  onRestoreSnapshot: (snapshotId: string) => void
  recentSnapshots: Array<{ snapshot_id: string; created_at: string; reason?: string }>
  modelLoadError?: string
  agentConfigLoadError?: string
  onReloadRuntimeConfig?: () => void
  disabled?: boolean
  running?: boolean
}

function formatSnapshotTime(value?: string) {
  if (!value) {
    return '-'
  }
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) {
    return value
  }
  return parsed.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function formatCompactNumber(value: number) {
  const num = Math.max(0, Number(value || 0))
  if (num >= 1000000) {
    return `${(num / 1000000).toFixed(1)}m`
  }
  if (num >= 1000) {
    return `${(num / 1000).toFixed(1)}k`
  }
  return String(num)
}

export default function ContextToolbar({
  executionMode,
  thinkingEnabled,
  activeModelName,
  activeAgentConfig,
  models,
  agentConfigs,
  messageCount,
  estimatedTokens,
  totalChars,
  planEnabled = true,
  planIssue,
  onExecutionModeChange,
  onThinkingToggle,
  onModelSwitch,
  onAgentConfigSwitch,
  onCompress,
  onCreateSnapshot,
  onRestoreSnapshot,
  recentSnapshots,
  modelLoadError,
  agentConfigLoadError,
  onReloadRuntimeConfig,
  disabled,
  running = false,
}: Props) {
  const activeModel = models.find((item) => item.name === activeModelName)
  const thinkSupported = Boolean(activeModel?.supports_thinking)
  const locked = Boolean(disabled || running)
  const planUnavailableReason = useMemo(
    () => resolvePlanUnavailableReason(planEnabled, planIssue),
    [planEnabled, planIssue],
  )
  const activeModelWarning = useMemo(() => {
    if (!activeModel) {
      return ''
    }
    if (planUnavailableReason) {
      return planUnavailableReason
    }
    if (activeModel.ready_for_chat === false) {
      return String(activeModel.readiness_reason || '当前模型尚未满足聊天运行条件').trim()
    }
    return ''
  }, [activeModel, planUnavailableReason])

  const executionModeOptions = useMemo(
    () => [
      {
        value: 'plan',
        label: 'PLAN',
        hint: planUnavailableReason ? '当前不可切换' : '只读分析，不执行操作',
        disabled: Boolean(planUnavailableReason),
        disabledReason: planUnavailableReason,
      },
      { value: 'build', label: 'BUILD', hint: '直接执行，全部工具可用' },
    ],
    [planUnavailableReason],
  )

  const executionModeButtonClassName = useMemo(
    () => cn(
      'min-w-[60px] h-6 rounded-sm border text-[8px] font-semibold tracking-wide transition-colors',
      executionMode === 'build'
        ? 'border-emerald-300 bg-emerald-50 text-emerald-700 hover:bg-emerald-100 dark:border-emerald-900/60 dark:bg-emerald-900/20 dark:text-emerald-300 dark:hover:bg-emerald-900/30'
        : 'border-amber-300 bg-amber-50 text-amber-700 hover:bg-amber-100 dark:border-amber-900/60 dark:bg-amber-900/20 dark:text-amber-300 dark:hover:bg-amber-900/30',
    ),
    [executionMode],
  )

  const modelOptions = useMemo(
    () =>
      models.map((item) => ({
        value: item.name,
        label: item.name,
        hint: `${item.provider} · ${item.model} · ${
          String(item.provider || '').trim().toLowerCase() === 'ollama'
            ? '工具适配'
            : item.ready_for_plan
              ? 'Plan 就绪'
              : item.supports_plan_execute
                ? 'Plan 待配置'
                : 'Plan 不支持'
        }`,
      })),
    [models],
  )

  const agentOptions = useMemo(
    () =>
      agentConfigs.map((item) => ({
        value: item,
        label: item,
      })),
    [agentConfigs],
  )

  const contextHealth = useMemo(() => {
    const maxTokens = Math.max(1, Number(activeModel?.max_context_tokens || 128000))
    const threshold = Math.max(1, Number(activeModel?.auto_compress_threshold || 85))
    const ratio = estimatedTokens / maxTokens
    if (ratio >= threshold / 100) {
      return {
        label: '压缩',
        className: 'border-red-300 bg-red-50 text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300',
      }
    }
    if (ratio >= 0.7) {
      return {
        label: '临界',
        className: 'border-amber-300 bg-amber-50 text-amber-700 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-300',
      }
    }
    return {
      label: '健康',
      className: 'border-emerald-300 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-900/20 dark:text-emerald-300',
    }
  }, [activeModel, estimatedTokens])

  return (
    <div
      className={cn(
        'relative z-20 flex h-7 items-center justify-between border-t border-border-default px-2.5 font-mono text-[9px] backdrop-blur transition-colors duration-200',
        executionMode === 'build' ? 'bg-emerald-50/50 dark:bg-emerald-950/15' : 'bg-surface-sunken',
      )}
      role="toolbar"
      aria-label="对话控制栏"
    >
      <div className="flex min-w-0 items-center gap-1.5">
        <CustomSelect
          label=""
          value={executionMode}
          options={executionModeOptions}
          onChange={(value) => onExecutionModeChange(value as ExecutionMode)}
          disabled={locked}
          compact
          placement="top"
          buttonClassName={executionModeButtonClassName}
        />

        <CustomSelect
          label=""
          value={activeModelName}
          options={modelOptions}
          onChange={onModelSwitch}
          disabled={locked || models.length === 0}
          compact
          placement="top"
          buttonClassName="min-w-[100px] h-6 text-[9px] rounded-sm"
        />

        <CustomSelect
          label=""
          value={activeAgentConfig}
          options={agentOptions}
          onChange={onAgentConfigSwitch}
          disabled={locked || agentConfigs.length === 0}
          compact
          placement="top"
          buttonClassName="min-w-[80px] h-6 text-[9px] rounded-sm"
        />

        <span className="text-border-default">│</span>

        <label
          className="inline-flex h-6 cursor-pointer items-center gap-1 rounded-sm border border-border-default bg-surface-raised px-1.5 text-[9px] text-text-secondary transition-colors hover:bg-surface-sunken"
          title={thinkSupported ? "深度思考" : "当前模型不支持深度思考"}
        >
          <input
            type="checkbox"
            checked={thinkingEnabled}
            onChange={(e) => onThinkingToggle(e.target.checked)}
            disabled={disabled || !thinkSupported}
            className="h-2 w-2 rounded-sm"
          />
          <span>💭</span>
        </label>

        {(modelLoadError || agentConfigLoadError) && (
          <div className="flex items-center gap-1 text-[9px] text-chip-amber-text">
            <span className="truncate max-w-[100px]">{modelLoadError || agentConfigLoadError}</span>
            <button
              className="rounded-sm border border-chip-amber-border px-1 py-0.5 hover:bg-chip-amber-bg disabled:opacity-60"
              onClick={onReloadRuntimeConfig}
              disabled={locked || !onReloadRuntimeConfig}
            >
              重试
            </button>
          </div>
        )}

        {activeModelWarning && !modelLoadError && (
          <span className="truncate max-w-[100px] text-[9px] text-chip-amber-text" title={activeModelWarning}>
            ⚠️ {activeModelWarning}
          </span>
        )}
      </div>

      <div className="flex shrink-0 items-center gap-1.5 pl-2">
        <span
          className={cn(
            'rounded-sm border px-1.5 py-0.5 text-[8px] font-medium',
            executionMode === 'build'
              ? 'border-emerald-300 bg-emerald-50 text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-900/20 dark:text-emerald-300'
              : 'border-chip-amber-border bg-chip-amber-bg text-chip-amber-text',
          )}
          title={executionMode === 'build' ? 'BUILD 模式：将直接执行工具操作' : 'PLAN 模式：仅分析不执行'}
        >
          {executionMode === 'build' ? '⚡ BUILD' : '🧭 PLAN'}
        </span>

        <div className="text-[9px] text-text-faint" title={`总字符数: ${formatCompactNumber(totalChars)}`}>
          {messageCount}msgs · {formatCompactNumber(estimatedTokens)}tok
        </div>

        <span className={cn('rounded-sm border px-1.5 py-0.5 text-[8px]', contextHealth.className)}>
          {contextHealth.label}
        </span>

        <span className="text-border-default">│</span>

        <button
          className={cn(
            'h-6 rounded-sm text-[9px] transition-colors hover:opacity-80 disabled:opacity-60',
            executionMode === 'build'
              ? 'text-emerald-700 dark:text-emerald-300'
              : 'text-accent',
          )}
          onClick={onCompress}
          disabled={locked}
          title="压缩上下文"
        >
          压缩
        </button>

        <Menu as="div" className="relative inline-block text-left">
          <Menu.Button
            className={cn(
              'h-6 rounded-sm text-[9px] transition-colors hover:opacity-80 disabled:opacity-60',
              executionMode === 'build'
                ? 'text-emerald-700 dark:text-emerald-300'
                : 'text-accent',
            )}
            disabled={locked}
          >
            快照 ▾
          </Menu.Button>

          <Transition
            as={Fragment}
            enter="transition ease-out duration-100"
            enterFrom="transform opacity-0 scale-95"
            enterTo="transform opacity-100 scale-100"
            leave="transition ease-in duration-75"
            leaveFrom="transform opacity-100 scale-100"
            leaveTo="transform opacity-0 scale-95"
          >
            <Menu.Items className="absolute bottom-full right-0 z-30 mb-1 min-w-[240px] origin-bottom-right rounded border border-border-default bg-surface-overlay p-1.5 shadow-lg focus:outline-none">
              <Menu.Item>
                {({ active }) => (
                  <button
                    className={cn(
                      'block w-full rounded-sm px-2.5 py-2 text-left text-[11px] transition-colors',
                      active
                        ? 'bg-surface-sunken text-text-primary'
                        : 'text-text-secondary',
                    )}
                    onClick={onCreateSnapshot}
                  >
                    创建快照
                  </button>
                )}
              </Menu.Item>

              <div className="mx-1 my-1 border-t border-border-subtle" />
              <div className="px-2 py-1 text-[11px] text-text-muted">恢复最近快照</div>

              {recentSnapshots.length === 0 ? (
                <div className="px-2 py-2 text-[11px] text-text-muted">暂无可恢复快照</div>
              ) : (
                recentSnapshots.map((snapshot) => (
                  <Menu.Item key={snapshot.snapshot_id}>
                    {({ active }) => (
                      <button
                        className={cn(
                          'block w-full rounded px-2.5 py-2 text-left transition-colors',
                          active
                            ? 'bg-surface-sunken'
                            : 'bg-transparent',
                        )}
                        onClick={() => onRestoreSnapshot(snapshot.snapshot_id)}
                      >
                        <div className="text-[11px] font-medium text-text-primary">
                          {snapshot.snapshot_id}
                        </div>
                        <div className="text-[10px] text-text-muted">
                          {formatSnapshotTime(snapshot.created_at)}
                          {snapshot.reason ? ` · ${snapshot.reason}` : ''}
                        </div>
                      </button>
                    )}
                  </Menu.Item>
                ))
              )}
            </Menu.Items>
          </Transition>
        </Menu>
      </div>
    </div>
  )
}
