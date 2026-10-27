'use client'
import PlanStatusBadge from '@/features/flows/PlanStatusBadge'
import { useState } from 'react'
import {
  isDesktopRuntime,
  pickDirectoryPath,
  pickFilePath,
} from '@/lib/config-form-helpers'
import type { ExecutionMode } from '@/types/message'
import type { useFlowComposer } from './useFlowComposer'
import { choiceValues } from './model'
import {
  canAcquire,
  changeSourceMode,
  sourceMode,
  sourcePath,
  updateSourcePath,
  type SourceMode,
} from './acquisition'
import AcquisitionPlanPanel from './AcquisitionPlanPanel'
const field =
  'w-full rounded border border-zinc-300 bg-surface-raised px-2 py-1 text-xs dark:border-zinc-700'
export default function FlowComposer({
  composer,
  disabled,
  mode,
}: {
  composer: ReturnType<typeof useFlowComposer>
  disabled?: boolean
  mode: ExecutionMode
}) {
  const [pathError, setPathError] = useState('')
  const plan = composer.detail?.plan
  const browse = async (key: string, folder: boolean) => {
    try {
      const path = await (folder ? pickDirectoryPath() : pickFilePath())
      if (path)
        composer.setValue(key, updateSourcePath(composer.values[key], path))
      setPathError('')
    } catch (error) {
      setPathError(error instanceof Error ? error.message : '选择路径失败')
    }
  }
  return (
    <div className="mb-2 space-y-2 text-xs">
      <label className="flex items-center gap-2">
        <span className="shrink-0">流程</span>
        <select
          aria-label="选择流程"
          className={field}
          disabled={disabled}
          value={composer.id}
          onChange={(event) => composer.select(event.target.value)}
        >
          <option value="">自动</option>
          {composer.unavailable && (
            <option value={composer.id}>
              {composer.id}（当前未启用或未找到）
            </option>
          )}
          {composer.flows.map((flow) => (
            <option key={flow.id} value={flow.id}>
              {flow.name}
            </option>
          ))}
        </select>
      </label>
      {composer.id && mode === 'plan' && (
        <p className="text-amber-700">
          本次仅预览流程，切换到 Build 后可执行。
        </p>
      )}
      {composer.loading && <p>正在读取流程…</p>}
      {composer.error && (
        <p role="alert" className="text-amber-700">
          {composer.error}{' '}
          <button type="button" onClick={composer.retry}>
            重试
          </button>
        </p>
      )}
      {composer.unavailable && (
        <p role="alert" className="text-amber-700">
          所选流程未在当前方案中启用，发送前请更换流程或选择自动。
        </p>
      )}
      {plan && (
        <PlanStatusBadge
          plan={plan}
          diagnostics={composer.detail!.diagnostics}
        />
      )}
      {!!composer.detail && (
        <div
          className="max-h-80 space-y-3 overflow-y-auto rounded border border-zinc-200 p-2 dark:border-zinc-700"
          aria-label="流程输入"
        >
          {(plan?.inputs.filter((input) => canAcquire(input, plan.auto_acquire))
            .length || 0) >= 2 && (
            <button
              type="button"
              disabled={disabled}
              onClick={composer.acquireMissing}
              className="text-blue-600"
            >
              没有的数据都自动获取
            </button>
          )}
          {plan?.inputs
            .filter((input) => input.type !== 'conversation_text')
            .map((input) => {
              const raw = composer.values[input.key],
                selectedMode = sourceMode(raw),
                automatic = ['auto', 'fill'].includes(selectedMode),
                provided = sourcePath(raw),
                value = automatic ? provided : raw
              const needs =
                composer.acquisitionRequiredInputs.has(input.key) ||
                composer.acquisitionRequiredInputs.has(input.label)
              const label = `${input.label}${input.required ? '（必填）' : ''}`
              const inputProblems = composer.inputProblems.filter(
                (problem) =>
                  problem.label === input.label || problem.label === input.key,
              )
              const missing = inputProblems.filter(
                (problem) => problem.acquirable && problem.level === 'error',
              ).length
              const items = (composer.acquisitionPlan?.items || []).filter(
                (item) =>
                  item.input === input.key || item.input === input.label,
              )
              return (
                <div key={input.key}>
                  {input.acquirable && (
                    <label className="mb-1 flex items-center gap-2">
                      <span className="shrink-0">{input.label}来源</span>
                      <select
                        aria-label={`${input.label}来源`}
                        className={field}
                        disabled={disabled}
                        value={selectedMode}
                        onChange={(event) =>
                          composer.setValue(
                            input.key,
                            changeSourceMode(
                              raw,
                              event.target.value as SourceMode,
                            ),
                          )
                        }
                      >
                        <option value="user">我来提供</option>
                        {input.type === 'folder' && (
                          <option
                            value="fill"
                            disabled={
                              !canAcquire(input, plan.auto_acquire) ||
                              Boolean(
                                input.modes && !input.modes.includes('fill'),
                              )
                            }
                          >
                            提供文件夹并补齐缺项
                          </option>
                        )}
                        <option
                          value="auto"
                          disabled={!canAcquire(input, plan.auto_acquire)}
                        >
                          自动获取
                        </option>
                        {!input.required && (
                          <option value="none">不提供</option>
                        )}
                      </select>
                    </label>
                  )}
                  {needs && (
                    <span className="mb-1 inline-block rounded bg-blue-50 px-1 text-blue-700">
                      自动获取需要
                    </span>
                  )}
                  {selectedMode !== 'auto' &&
                    selectedMode !== 'none' &&
                    (input.type === 'boolean' ? (
                      <label className="flex items-center gap-2">
                        <input
                          type="checkbox"
                          disabled={disabled}
                          checked={Boolean(value)}
                          onChange={(event) =>
                            composer.setValue(input.key, event.target.checked)
                          }
                        />
                        {label}
                      </label>
                    ) : (
                      <label className="block space-y-1">
                        <span>{label}</span>
                        {input.type === 'choice' ? (
                          <select
                            className={field}
                            disabled={disabled}
                            value={String(value ?? '')}
                            onChange={(event) =>
                              composer.setValue(input.key, event.target.value)
                            }
                          >
                            <option value="">请选择</option>
                            {choiceValues(input).map((option) => (
                              <option key={option} value={option}>
                                {option}
                              </option>
                            ))}
                          </select>
                        ) : input.type === 'layer' && composer.layers.length ? (
                          <select
                            className={field}
                            disabled={disabled}
                            value={String(value ?? '')}
                            onChange={(event) =>
                              composer.setValue(input.key, event.target.value)
                            }
                          >
                            <option value="">请选择当前工程图层</option>
                            {value &&
                            !composer.layers.some(
                              (layer) => layer.id === String(value),
                            ) ? (
                              <option value={String(value)}>
                                {String(value)}（当前输入）
                              </option>
                            ) : null}
                            {composer.layers.map((layer) => (
                              <option key={layer.id} value={layer.id}>
                                {layer.name}
                              </option>
                            ))}
                          </select>
                        ) : (
                          <input
                            className={field}
                            type={input.type === 'number' ? 'number' : 'text'}
                            step={input.type === 'number' ? 'any' : undefined}
                            disabled={disabled}
                            value={String(value ?? '')}
                            list={
                              input.type === 'layer'
                                ? `flow-layer-${input.key}`
                                : undefined
                            }
                            onChange={(event) =>
                              composer.setValue(
                                input.key,
                                updateSourcePath(raw, event.target.value),
                              )
                            }
                          />
                        )}
                      </label>
                    ))}
                  {input.type === 'layer' && !composer.layers.length && (
                    <datalist id={`flow-layer-${input.key}`}>
                      {(input.options || []).map((option) => (
                        <option key={option} value={option} />
                      ))}
                    </datalist>
                  )}
                  {['file', 'folder'].includes(input.type) &&
                    ['user', 'fill'].includes(selectedMode) &&
                    isDesktopRuntime() && (
                      <button
                        type="button"
                        disabled={disabled}
                        className="mt-1 text-blue-600"
                        onClick={() =>
                          void browse(input.key, input.type === 'folder')
                        }
                      >
                        选择{input.type === 'folder' ? '文件夹' : '文件'}
                      </button>
                    )}
                  {selectedMode === 'user' &&
                    input.type === 'folder' &&
                    provided &&
                    missing > 0 &&
                    canAcquire(input, plan.auto_acquire) && (
                      <button
                        type="button"
                        disabled={disabled}
                        onClick={() =>
                          composer.setValue(
                            input.key,
                            changeSourceMode(raw, 'fill'),
                          )
                        }
                        className="ml-2 text-blue-600"
                      >
                        缺少 {missing} 项，可自动补齐
                      </button>
                    )}
                  {automatic && (
                    <AcquisitionPlanPanel
                      items={items}
                      value={raw}
                      disabled={disabled}
                      onChange={(value) => composer.setValue(input.key, value)}
                    />
                  )}
                  {input.acquirable && plan.auto_acquire === 'deny' && (
                    <p className="text-zinc-500">当前方案不自动获取数据。</p>
                  )}
                  {inputProblems.map((problem, index) => (
                    <p
                      key={index}
                      role={problem.level === 'error' ? 'alert' : undefined}
                      className={
                        problem.level === 'error'
                          ? 'mt-1 text-red-700'
                          : 'mt-1 text-amber-700'
                      }
                    >
                      {problem.message}
                    </p>
                  ))}
                  {input.description && (
                    <p className="mt-0.5 text-text-muted">
                      {input.description}
                    </p>
                  )}
                </div>
              )
            })}
          {plan?.inputs.every(
            (input) => input.type === 'conversation_text',
          ) && <p className="text-text-muted">输入将自动使用本条对话原文。</p>}
          {composer.detail.diagnostics
            .filter((item) => item.level === 'error')
            .map((item, index) => (
              <p key={index} role="alert" className="text-red-700">
                {item.message}
              </p>
            ))}
        </div>
      )}
      {composer.checking && <p className="text-text-muted">正在校验输入…</p>}
      {composer.inputProblems
        .filter(
          (problem) =>
            !plan?.inputs.some(
              (input) =>
                input.key === problem.label || input.label === problem.label,
            ),
        )
        .map((problem, index) => (
          <p role="alert" key={index}>
            {problem.message}
          </p>
        ))}
      {composer.memoryError && (
        <p className="text-amber-700">{composer.memoryError}</p>
      )}
      {composer.layerError && (
        <p className="text-amber-700">{composer.layerError}</p>
      )}
      {pathError && (
        <p role="alert" className="text-amber-700">
          {pathError}
        </p>
      )}
      {composer.errors.map((error) => (
        <p role="alert" key={error} className="text-red-700">
          {error}
        </p>
      ))}
    </div>
  )
}
