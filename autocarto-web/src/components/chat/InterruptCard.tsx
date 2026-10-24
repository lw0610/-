'use client'

import { useEffect, useMemo, useRef, useState } from 'react'
import {
  DecisionSelectMode,
  InterruptContext,
  InterruptOption,
  InterruptRequestData,
} from '@/types/message'
import { cn } from '@/lib/utils'

const CUSTOM_INPUT_VALUE = '__custom_input__'
const CUSTOM_INPUT_LABEL = '自定义回答'
const PRIMARY_CONFIRM_YES_VALUE = '__confirm_yes__'
const PRIMARY_CONFIRM_NO_VALUE = '__confirm_no__'

interface DecisionCardItem {
  id: string
  title: string
  description: string
  selectMode: DecisionSelectMode
  options: InterruptOption[]
  defaultValue: string
  defaultValues: string[]
  required: boolean
  category: string
}

interface ParsedInterruptInfo {
  type: string
  title: string
  description: string
  options: InterruptOption[]
  selectMode: DecisionSelectMode
  defaultValue: string
  defaultValues: string[]
  plan: Record<string, unknown>
  decisions: DecisionCardItem[]
  required: boolean
  category: string
}

interface DecisionAnswerState {
  selected: string[]
  text: string
  note: string
  confirmed: boolean | null
}

type DecisionAnswerPayload = {
  selected?: string[]
  text?: string
  note?: string
  confirmed?: boolean
}

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object') {
    return {}
  }
  return value as Record<string, unknown>
}

function parseInfoFromContexts(contexts: InterruptContext[]) {
  for (const ctx of contexts) {
    if (!ctx?.is_root_cause) {
      continue
    }
    if (typeof ctx.info === 'string') {
      try { return asRecord(JSON.parse(ctx.info)) } catch (error) { void error; return {
        description: ctx.info,
      } }
    }
    return asRecord(ctx.info)
  }
  return {}
}

function normalizeSelectMode(raw: unknown, fallback: DecisionSelectMode): DecisionSelectMode {
  const mode = String(raw || '').trim().toLowerCase()
  if (mode === 'single' || mode === 'multiple' || mode === 'confirm' || mode === 'text') {
    return mode
  }
  return fallback
}

function normalizeStringArray(raw: unknown): string[] {
  if (!Array.isArray(raw)) {
    return []
  }
  const seen = new Set<string>()
  const values: string[] = []
  raw.forEach((item) => {
    const value = String(item || '').trim()
    if (!value || seen.has(value)) {
      return
    }
    seen.add(value)
    values.push(value)
  })
  return values
}

function parseOptions(raw: unknown): InterruptOption[] {
  if (!Array.isArray(raw)) {
    return []
  }
  return raw
    .filter((item) => item && typeof item === 'object')
    .map((item) => item as InterruptOption)
    .map((item) => {
      const value = String(item.value || '').trim()
      const label = String(item.label || value).trim()
      const reason = String(item.reason || '').trim()
      return {
        value: value || label,
        label: label || value,
        reason,
      } satisfies InterruptOption
    })
    .filter((item) => String(item.value || '').trim())
}

function appendCustomOption(options: InterruptOption[]): InterruptOption[] {
  const hasCustomOption = options.some((item) => String(item.value || '').trim() === CUSTOM_INPUT_VALUE)
  if (hasCustomOption) {
    return options
  }
  return [
    ...options,
    {
      value: CUSTOM_INPUT_VALUE,
      label: CUSTOM_INPUT_LABEL,
      reason: '',
    },
  ]
}

function hasCustomSelection(selected: string[]): boolean {
  return selected.includes(CUSTOM_INPUT_VALUE)
}

function parseDecisionItems(raw: unknown): DecisionCardItem[] {
  if (!Array.isArray(raw)) {
    return []
  }

  return raw
    .filter((item) => item && typeof item === 'object')
    .map((item, idx) => {
      const payload = asRecord(item)
      const options = parseOptions(payload.options)
      const selectMode = normalizeSelectMode(payload.select_mode, 'single')
      const defaultValue = String(payload.default || '').trim()
      let defaultValues = normalizeStringArray(payload.defaults)
      if (selectMode === 'multiple' && defaultValues.length === 0 && defaultValue) {
        defaultValues = [defaultValue]
      }

      return {
        id: String(payload.id || `decision_${idx + 1}`).trim() || `decision_${idx + 1}`,
        title: String(payload.title || `决策项 ${idx + 1}`).trim() || `决策项 ${idx + 1}`,
        description: String(payload.description || '').trim(),
        selectMode,
        options,
        defaultValue,
        defaultValues,
        required: Boolean(payload.required),
        category: String(payload.category || '').trim(),
      } satisfies DecisionCardItem
    })
}

function buildInitialDecisionAnswers(decisions: DecisionCardItem[]) {
  const state: Record<string, DecisionAnswerState> = {}
  decisions.forEach((decision) => {
    const selected = decision.selectMode === 'multiple'
      ? [...decision.defaultValues]
      : decision.selectMode === 'single' && decision.defaultValue
        ? [decision.defaultValue]
        : []
    const customSelected = hasCustomSelection(selected)
    const answer: DecisionAnswerState = {
      selected,
      text: decision.selectMode === 'text' || customSelected ? decision.defaultValue : '',
      note: '',
      confirmed: decision.selectMode === 'confirm' ? normalizeConfirmDefault(decision.defaultValue) : null,
    }

    state[decision.id] = answer
  })
  return state
}

function normalizeConfirmDefault(raw: string): boolean | null {
  const value = String(raw || '').trim().toLowerCase()
  if (!value) {
    return null
  }
  if (['yes', 'y', 'true', 'approve', 'approved', 'confirm', 'confirmed', '1', PRIMARY_CONFIRM_YES_VALUE].includes(value)) {
    return true
  }
  if (['no', 'n', 'false', 'reject', 'rejected', 'deny', 'denied', '0', PRIMARY_CONFIRM_NO_VALUE].includes(value)) {
    return false
  }
  return null
}

function buildInitialPrimaryState(info: ParsedInterruptInfo) {
  if (info.selectMode === 'multiple') {
    return {
      selectedOption: '',
      selectedOptions: [...info.defaultValues],
      primaryTextInput: '',
    }
  }

  if (info.selectMode === 'text') {
    return {
      selectedOption: '',
      selectedOptions: [],
      primaryTextInput: info.defaultValue,
    }
  }

  if (info.selectMode === 'confirm') {
    const confirmDefault = normalizeConfirmDefault(info.defaultValue)
    return {
      selectedOption: confirmDefault === true
        ? PRIMARY_CONFIRM_YES_VALUE
        : confirmDefault === false
          ? PRIMARY_CONFIRM_NO_VALUE
          : '',
      selectedOptions: [],
      primaryTextInput: '',
    }
  }

  return {
    selectedOption: info.defaultValue,
    selectedOptions: [],
    primaryTextInput: '',
  }
}

function isDecisionAnswered(decision: DecisionCardItem, answer: DecisionAnswerState | undefined): boolean {
  if (!answer) {
    return false
  }

  const selected = Array.isArray(answer.selected) ? answer.selected : []
  const hasCustomSelected = hasCustomSelection(selected)
  const hasCustomText = Boolean(answer.text.trim())

  if (decision.selectMode === 'single') {
    if (selected.length === 0) {
      return false
    }
    if (hasCustomSelected) {
      return hasCustomText
    }
    return true
  }
  if (decision.selectMode === 'multiple') {
    if (selected.length === 0) {
      return false
    }
    if (hasCustomSelected) {
      return hasCustomText
    }
    return true
  }
  if (decision.selectMode === 'text') {
    return hasCustomText
  }
  if (decision.selectMode === 'confirm') {
    if (hasCustomSelected) {
      return hasCustomText
    }
    return answer.confirmed !== null
  }
  return false
}

function buildDecisionPayload(decision: DecisionCardItem, answer: DecisionAnswerState | undefined): DecisionAnswerPayload | null {
  if (!answer) {
    return null
  }
  const payload: DecisionAnswerPayload = {}
  const selected = Array.isArray(answer.selected) ? answer.selected : []
  const hasCustomSelected = hasCustomSelection(selected)
  const customText = answer.text.trim()

  if ((decision.selectMode === 'single' || decision.selectMode === 'multiple') && selected.length > 0) {
    payload.selected = selected
  }
  if (decision.selectMode === 'text' && customText) {
    payload.text = customText
  }
  if ((decision.selectMode === 'single' || decision.selectMode === 'multiple' || decision.selectMode === 'confirm') && hasCustomSelected && customText) {
    payload.text = customText
  }
  if (answer.note.trim()) {
    payload.note = answer.note.trim()
  }
  if (decision.selectMode === 'confirm' && answer.confirmed !== null && !hasCustomSelected) {
    payload.confirmed = answer.confirmed
  }

  return Object.keys(payload).length > 0 ? payload : null
}

function categoryLabel(raw: string) {
  const value = raw.trim().toLowerCase()
  if (value === 'requirements') {
    return '需求确认'
  }
  if (value === 'approach') {
    return '方案选择'
  }
  if (value === 'risk') {
    return '风险确认'
  }
  if (value === 'parameters') {
    return '参数选择'
  }
  if (value === 'flow_confirmation') return '流程确认'
  return raw.trim()
}

function optionClassName(selected: boolean) {
  return cn(
    'flex cursor-pointer items-start gap-2 rounded-sm border px-2 py-1.5 text-[11px] transition-colors',
    selected
      ? 'border-blue-500 bg-[#f0f9ff] text-zinc-900 dark:border-blue-500 dark:bg-blue-950/20 dark:text-zinc-100'
      : 'border-zinc-200 bg-white text-zinc-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-200',
  )
}

function textAreaClassName() {
  return 'w-full rounded-sm border border-zinc-200 bg-white px-2 py-1.5 text-[11px] text-zinc-700 dark:border-zinc-700 dark:bg-zinc-950 dark:text-zinc-200'
}

function decisionStepClassName(answered: boolean, active: boolean) {
  if (active) {
    return 'border-blue-500 bg-[#f0f9ff] text-blue-700 dark:border-blue-500 dark:bg-blue-950/20 dark:text-blue-300'
  }
  if (answered) {
    return 'border-blue-500 bg-blue-500 text-white dark:border-blue-500 dark:bg-blue-500 dark:text-white'
  }
  return 'border-zinc-200 bg-white text-zinc-500 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300'
}

function parseInterruptInfo(interrupt: InterruptRequestData): ParsedInterruptInfo {
  const contexts = Array.isArray(interrupt.interrupt_contexts)
    ? interrupt.interrupt_contexts
    : []
  const info = parseInfoFromContexts(contexts)

  const type = String(info.type || 'plan_confirmation')
  const options = parseOptions(info.options)
  const decisions = parseDecisionItems(info.decisions)
  const fallbackMode = type === 'clarification'
    ? 'single'
    : type === 'plan_confirmation'
      ? 'confirm'
      : 'single'
  const selectMode = normalizeSelectMode(info.select_mode, fallbackMode)
  const defaultValue = String(info.default || '').trim()
  let defaultValues = normalizeStringArray(info.defaults)
  if (selectMode === 'multiple' && defaultValues.length === 0 && defaultValue) {
    defaultValues = [defaultValue]
  }

  return {
    type,
    title: String(info.title || '需要您的确认'),
    description: String(info.description || ''),
    options,
    selectMode,
    defaultValue,
    defaultValues,
    plan: asRecord(info.plan),
    decisions,
    required: Boolean(info.required),
    category: String(info.category || '').trim(),
  }
}

interface Props {
  interrupt: InterruptRequestData
  submitting?: boolean
  resolvedState?: 'approved' | 'rejected'
  onApprove: (payload?: unknown) => void
  onReject: () => void
}

export default function InterruptCard({
  interrupt,
  submitting,
  resolvedState,
  onApprove,
  onReject,
}: Props) {
  const info = useMemo(() => parseInterruptInfo(interrupt), [interrupt])
  const checkpointId = String(interrupt.checkpoint_id || '')
  const [selectedOption, setSelectedOption] = useState('')
  const [selectedOptions, setSelectedOptions] = useState<string[]>([])
  const [primaryTextInput, setPrimaryTextInput] = useState('')
  const [activeDecisionIndex, setActiveDecisionIndex] = useState(0)
  const [decisionAnswers, setDecisionAnswers] = useState<Record<string, DecisionAnswerState>>(
    () => buildInitialDecisionAnswers(info.decisions),
  )
  const [manualInput, setManualInput] = useState('')
  const interactionDisabled = Boolean(submitting || resolvedState)
  const initializedInterruptRef = useRef('')

  const interruptIdentity = useMemo(() => {
    if (checkpointId) {
      return checkpointId
    }
    return JSON.stringify({
      type: info.type,
      title: info.title,
      selectMode: info.selectMode,
      defaultValue: info.defaultValue,
      defaultValues: info.defaultValues,
      options: info.options.map((item) => String(item.value || item.label || '')),
      decisions: info.decisions.map((item) => ({
        id: item.id,
        defaultValue: item.defaultValue,
        defaultValues: item.defaultValues,
      })),
    })
  }, [checkpointId, info])

  const initialPrimaryState = useMemo(() => buildInitialPrimaryState(info), [info])

  useEffect(() => {
    if (initializedInterruptRef.current === interruptIdentity) {
      return
    }
    initializedInterruptRef.current = interruptIdentity

    setSelectedOption(initialPrimaryState.selectedOption)
    setSelectedOptions(initialPrimaryState.selectedOptions)
    setPrimaryTextInput(initialPrimaryState.primaryTextInput)
    setActiveDecisionIndex(0)
    setDecisionAnswers(buildInitialDecisionAnswers(info.decisions))
    setManualInput('')
  }, [
    initialPrimaryState,
    info,
    info.defaultValue,
    info.defaultValues,
    info.decisions,
    interruptIdentity,
  ])

  const objective = String(info.plan.objective || '')
  const steps = Array.isArray(info.plan.steps)
    ? info.plan.steps.filter((item) => item && typeof item === 'object').map((item) => item as Record<string, unknown>)
    : []
  const risks = Array.isArray(info.plan.risks)
    ? info.plan.risks.map((item) => String(item || '')).filter(Boolean)
    : []

  const resolvedText = resolvedState === 'approved'
    ? '已确认，任务继续执行。'
    : resolvedState === 'rejected'
      ? '已拒绝，等待后续处理。'
      : ''

  const allDecisionsAnswered = useMemo(
    () => info.decisions.length > 0 && info.decisions.every((decision) => isDecisionAnswered(decision, decisionAnswers[decision.id])),
    [decisionAnswers, info.decisions],
  )

  const requiredUnfilled = useMemo(() => {
    if (info.decisions.length > 0) {
      return !allDecisionsAnswered
    }

    if (info.selectMode === 'single') {
      if (selectedOption === CUSTOM_INPUT_VALUE) {
        return !primaryTextInput.trim()
      }
      return info.required ? !selectedOption : false
    }
    if (info.selectMode === 'multiple') {
      const hasCustomSelected = selectedOptions.includes(CUSTOM_INPUT_VALUE)
      if (hasCustomSelected && !primaryTextInput.trim()) {
        return true
      }
      return info.required ? selectedOptions.length === 0 : false
    }
    if (info.selectMode === 'text') {
      return info.required ? !primaryTextInput.trim() : false
    }
    if (info.selectMode === 'confirm') {
      if (selectedOption === CUSTOM_INPUT_VALUE) {
        return !primaryTextInput.trim()
      }
      const hasBasicConfirm = selectedOption === PRIMARY_CONFIRM_YES_VALUE || selectedOption === PRIMARY_CONFIRM_NO_VALUE
      return info.required ? !hasBasicConfirm : false
    }
    return false
  }, [allDecisionsAnswered, info, primaryTextInput, selectedOption, selectedOptions])

  const updateDecisionAnswer = (decisionId: string, updater: (current: DecisionAnswerState) => DecisionAnswerState) => {
    setDecisionAnswers((prev) => ({
      ...prev,
      [decisionId]: updater(prev[decisionId] || { selected: [], text: '', note: '', confirmed: null }),
    }))
  }

  const currentDecision = info.decisions.length > 0
    ? info.decisions[Math.min(activeDecisionIndex, info.decisions.length - 1)]
    : null

  const currentDecisionAnswer = currentDecision
    ? decisionAnswers[currentDecision.id] || { selected: [], text: '', note: '', confirmed: null }
    : null

  const currentDecisionOptions = useMemo(() => {
    if (!currentDecision) {
      return []
    }
    if (info.category !== 'flow_confirmation' && currentDecision.category !== 'flow_confirmation' && (currentDecision.selectMode === 'single' || currentDecision.selectMode === 'multiple')) {
      return appendCustomOption(currentDecision.options)
    }
    return currentDecision.options
  }, [currentDecision, info.category])
  const primaryOptions = info.category === 'flow_confirmation' ? info.options : appendCustomOption(info.options)

  const answeredDecisionCount = useMemo(
    () => info.decisions.filter((decision) => isDecisionAnswered(decision, decisionAnswers[decision.id])).length,
    [decisionAnswers, info.decisions],
  )

  const activeDecisionAnswered = currentDecision
    ? isDecisionAnswered(currentDecision, currentDecisionAnswer || undefined)
    : false

  const isLastDecision = info.decisions.length > 0 && activeDecisionIndex >= info.decisions.length - 1
  const currentDecisionHasCustomSelected = currentDecisionAnswer
    ? hasCustomSelection(currentDecisionAnswer.selected)
    : false
  const showNextDecisionAction = info.decisions.length > 0 && !isLastDecision
  const canMoveToNextDecision = info.decisions.length > 0 && activeDecisionAnswered

  const validationMessage = useMemo(() => {
    if (resolvedState) {
      return ''
    }

    if (info.decisions.length > 0) {
      if (showNextDecisionAction && !activeDecisionAnswered) {
        return '请先回答当前问题后继续下一项。'
      }
      if (!showNextDecisionAction && !allDecisionsAnswered) {
        return '请先完成全部问题后再确认。'
      }
      return ''
    }

    if (requiredUnfilled) {
      return '请先完成必填决策项。'
    }
    return ''
  }, [activeDecisionAnswered, allDecisionsAnswered, info.decisions.length, requiredUnfilled, resolvedState, showNextDecisionAction])

  return (
    <div
      className={cn(
        'rounded border border-amber-500 border-l-[3px] border-l-amber-500 bg-white p-3 text-[11px] dark:border-amber-700 dark:bg-zinc-900',
      )}
    >
      <div className="mb-2 border-b border-amber-100 pb-2 dark:border-amber-900/40">
        <div className="flex items-center justify-between gap-2">
          <div className="font-semibold text-zinc-900 dark:text-zinc-100">{info.title}</div>
          <div className="flex items-center gap-2 text-[9px] text-zinc-500 dark:text-zinc-400">
            {info.decisions.length > 0 && (
              <>
                {info.decisions.length > 1 ? (
                  <div className="inline-flex items-center rounded-sm border border-zinc-200 bg-white font-mono dark:border-zinc-700 dark:bg-zinc-900">
                  <button
                    type="button"
                    className="px-1.5 py-0.5 hover:bg-zinc-50 disabled:opacity-40 dark:hover:bg-zinc-800"
                    onClick={() => setActiveDecisionIndex((prev) => Math.max(0, prev - 1))}
                    disabled={interactionDisabled || activeDecisionIndex <= 0}
                    title="上一项"
                  >
                    ←
                  </button>
                  <span className="px-1 py-0.5">
                    {Math.min(activeDecisionIndex + 1, info.decisions.length)}/{info.decisions.length}
                  </span>
                  <button
                    type="button"
                    className="px-1.5 py-0.5 hover:bg-zinc-50 disabled:opacity-40 dark:hover:bg-zinc-800"
                    onClick={() => setActiveDecisionIndex((prev) => Math.min(info.decisions.length - 1, prev + 1))}
                    disabled={interactionDisabled || activeDecisionIndex >= info.decisions.length - 1}
                    title="下一项"
                  >
                    →
                  </button>
                  </div>
                ) : (
                  <span className="rounded-sm border border-zinc-200 bg-white px-1.5 py-0.5 font-mono dark:border-zinc-700 dark:bg-zinc-900">
                    {Math.min(activeDecisionIndex + 1, info.decisions.length)}/{info.decisions.length}
                  </span>
                )}
                <span className={cn(
                  'rounded-sm border px-1.5 py-0.5',
                  activeDecisionAnswered
                    ? 'border-blue-500 bg-blue-500 text-white dark:border-blue-500 dark:bg-blue-500'
                    : 'border-zinc-200 bg-white text-zinc-500 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300',
                )}
                >
                  {activeDecisionAnswered ? '已回答' : '待回答'}
                </span>
              </>
            )}
            <div className="font-mono text-[8px]">{checkpointId || 'pending checkpoint'}</div>
          </div>
        </div>
      </div>

      {info.category && (
        <div className="mb-2 inline-flex rounded-sm bg-amber-100 px-1.5 py-0.5 text-[8px] text-amber-800 dark:bg-amber-900/30 dark:text-amber-300">
          {categoryLabel(info.category)}
        </div>
      )}

      {resolvedText && (
        <div
          className={cn(
            'mb-2 rounded-sm border px-2 py-1 text-[10px]',
            resolvedState === 'approved'
              ? 'border-emerald-200 bg-emerald-100 text-emerald-800 dark:border-emerald-800 dark:bg-emerald-900/20 dark:text-emerald-300'
              : 'border-zinc-200 bg-zinc-100 text-zinc-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300',
          )}
        >
          {resolvedText}
        </div>
      )}

      {info.description && (
        <div className="mb-2 whitespace-pre-wrap break-words text-zinc-700 dark:text-zinc-200">{info.description}</div>
      )}

      {info.decisions.length > 0 && currentDecision && currentDecisionAnswer && (
        <div className="mb-2 space-y-2 rounded-sm border border-zinc-200 bg-white p-2 text-[11px] dark:border-zinc-800 dark:bg-zinc-900/40">
          <div className="mb-1 flex items-center gap-2">
            <div className="font-medium text-zinc-800 dark:text-zinc-100">
              {Math.min(activeDecisionIndex + 1, info.decisions.length)}. {currentDecision.title}
              {currentDecision.required && <span className="ml-1 text-red-500">*</span>}
            </div>
            {currentDecision.category && (
              <span className="rounded-sm border border-zinc-200 px-1 py-0.5 text-[8px] text-zinc-500 dark:border-zinc-700 dark:text-zinc-300">
                {categoryLabel(currentDecision.category)}
              </span>
            )}
          </div>

          {currentDecision.description && (
            <div className="mb-1 text-zinc-600 dark:text-zinc-300">{currentDecision.description}</div>
          )}

          {(currentDecision.selectMode === 'single' || currentDecision.selectMode === 'multiple') && currentDecisionOptions.length > 0 && (
            <div className="space-y-1">
              {currentDecisionOptions.map((option, optionIdx) => {
                const value = String(option.value || '')
                const label = String(option.label || value || `选项 ${optionIdx + 1}`)
                const checked = currentDecision.selectMode === 'single'
                  ? currentDecisionAnswer.selected[0] === value
                  : currentDecisionAnswer.selected.includes(value)
                return (
                  <label key={`${currentDecision.id}-${value}-${optionIdx}`} className={optionClassName(checked)}>
                    <input
                      type={currentDecision.selectMode === 'single' ? 'radio' : 'checkbox'}
                      name={`decision-${checkpointId}-${currentDecision.id}`}
                      checked={checked}
                      onChange={() => {
                        if (currentDecision.selectMode === 'single') {
                          updateDecisionAnswer(currentDecision.id, (current) => ({
                            ...current,
                            selected: value ? [value] : [],
                          }))
                          return
                        }

                        updateDecisionAnswer(currentDecision.id, (current) => {
                          const nextSelected = current.selected.includes(value)
                            ? current.selected.filter((item) => item !== value)
                            : [...current.selected, value]
                          return {
                            ...current,
                            selected: nextSelected,
                          }
                        })
                      }}
                      disabled={interactionDisabled}
                      className="mt-0.5 h-3.5 w-3.5 rounded-sm"
                    />
                    <span className="min-w-0">
                      <span className="font-medium text-zinc-900 dark:text-zinc-100">{label}</span>
                      {option.reason && <span className="ml-1 text-[9px] text-zinc-500 dark:text-zinc-400">{String(option.reason)}</span>}
                    </span>
                  </label>
                )
              })}

              {currentDecisionHasCustomSelected && (
                <textarea
                  className={cn('mt-1', textAreaClassName())}
                  placeholder="请填写自定义回答"
                  rows={2}
                  value={currentDecisionAnswer.text}
                  onChange={(e) => {
                    const value = e.target.value
                    updateDecisionAnswer(currentDecision.id, (current) => ({
                      ...current,
                      text: value,
                    }))
                  }}
                  disabled={interactionDisabled}
                />
              )}
            </div>
          )}

          {currentDecision.selectMode === 'text' && (
            <textarea
              className={textAreaClassName()}
              placeholder="请输入决策内容"
              rows={2}
              value={currentDecisionAnswer.text}
              onChange={(e) => {
                const value = e.target.value
                updateDecisionAnswer(currentDecision.id, (current) => ({
                  ...current,
                  text: value,
                }))
              }}
              disabled={interactionDisabled}
            />
          )}

          {currentDecision.selectMode === 'confirm' && (
            <div className="space-y-2">
              <div className="grid gap-2 sm:grid-cols-3">
                <label className={optionClassName(currentDecisionAnswer.confirmed === true && !currentDecisionHasCustomSelected)}>
                  <input
                    type="radio"
                    name={`decision-${checkpointId}-${currentDecision.id}-confirm`}
                    checked={currentDecisionAnswer.confirmed === true && !currentDecisionHasCustomSelected}
                    onChange={() => {
                      updateDecisionAnswer(currentDecision.id, (current) => ({
                        ...current,
                        selected: current.selected.filter((item) => item !== CUSTOM_INPUT_VALUE),
                        confirmed: true,
                      }))
                    }}
                    disabled={interactionDisabled}
                    className="h-3.5 w-3.5"
                  />
                  <span>是</span>
                </label>
                <label className={optionClassName(currentDecisionAnswer.confirmed === false && !currentDecisionHasCustomSelected)}>
                  <input
                    type="radio"
                    name={`decision-${checkpointId}-${currentDecision.id}-confirm`}
                    checked={currentDecisionAnswer.confirmed === false && !currentDecisionHasCustomSelected}
                    onChange={() => {
                      updateDecisionAnswer(currentDecision.id, (current) => ({
                        ...current,
                        selected: current.selected.filter((item) => item !== CUSTOM_INPUT_VALUE),
                        confirmed: false,
                      }))
                    }}
                    disabled={interactionDisabled}
                    className="h-3.5 w-3.5"
                  />
                  <span>否</span>
                </label>
                <label className={optionClassName(currentDecisionHasCustomSelected)}>
                  <input
                    type="radio"
                    name={`decision-${checkpointId}-${currentDecision.id}-confirm`}
                    checked={currentDecisionHasCustomSelected}
                    onChange={() => {
                      updateDecisionAnswer(currentDecision.id, (current) => ({
                        ...current,
                        selected: [CUSTOM_INPUT_VALUE],
                        confirmed: null,
                      }))
                    }}
                    disabled={interactionDisabled}
                    className="h-3.5 w-3.5"
                  />
                  <span>{CUSTOM_INPUT_LABEL}</span>
                </label>
              </div>

              {currentDecisionHasCustomSelected && (
                <textarea
                  className={textAreaClassName()}
                  placeholder="请填写自定义回答"
                  rows={2}
                  value={currentDecisionAnswer.text}
                  onChange={(e) => {
                    const value = e.target.value
                    updateDecisionAnswer(currentDecision.id, (current) => ({
                      ...current,
                      text: value,
                    }))
                  }}
                  disabled={interactionDisabled}
                />
              )}
            </div>
          )}

          <div>
            <textarea
              className={textAreaClassName()}
              placeholder="可选：补充该问题的决策说明"
              rows={2}
              value={currentDecisionAnswer.note}
              onChange={(e) => {
                const value = e.target.value
                updateDecisionAnswer(currentDecision.id, (current) => ({
                  ...current,
                  note: value,
                }))
              }}
              disabled={interactionDisabled}
            />
          </div>

          <div className="text-[10px] text-zinc-500 dark:text-zinc-400">
            已完成 {answeredDecisionCount}/{info.decisions.length} 个问题
          </div>
        </div>
      )}

      {info.decisions.length === 0 && info.selectMode === 'single' && (
        <div className="mb-2 space-y-1 rounded-sm border border-zinc-200 bg-white p-2 dark:border-zinc-800 dark:bg-zinc-900/40">
          {primaryOptions.map((option, idx) => {
            const value = String(option.value || '')
            const label = String(option.label || value || `选项 ${idx + 1}`)
            const checked = selectedOption === value
            return (
              <label key={`${value}-${idx}`} className={optionClassName(checked)}>
                <input
                  type="radio"
                  name={`interrupt-${checkpointId}`}
                  checked={checked}
                  onChange={() => setSelectedOption(value)}
                  disabled={interactionDisabled}
                  className="mt-0.5 h-3.5 w-3.5"
                />
                <span className="min-w-0">
                  <span className="font-medium text-zinc-900 dark:text-zinc-100">{label}</span>
                  {option.reason && <span className="block text-[9px] text-zinc-500 dark:text-zinc-400">{String(option.reason)}</span>}
                </span>
              </label>
            )
          })}

          {selectedOption === CUSTOM_INPUT_VALUE && (
            <textarea
              className={textAreaClassName()}
              placeholder="请填写自定义回答"
              rows={2}
              value={primaryTextInput}
              onChange={(e) => setPrimaryTextInput(e.target.value)}
              disabled={interactionDisabled}
            />
          )}
        </div>
      )}

      {info.decisions.length === 0 && info.selectMode === 'multiple' && (
        <div className="mb-2 space-y-1 rounded-sm border border-zinc-200 bg-white p-2 dark:border-zinc-800 dark:bg-zinc-900/40">
          {primaryOptions.map((option, idx) => {
            const value = String(option.value || '')
            const label = String(option.label || value || `选项 ${idx + 1}`)
            const checked = selectedOptions.includes(value)
            return (
              <label key={`${value}-${idx}`} className={optionClassName(checked)}>
                <input
                  type="checkbox"
                  checked={checked}
                  onChange={() => {
                    setSelectedOptions((prev) => (
                      prev.includes(value)
                        ? prev.filter((item) => item !== value)
                        : [...prev, value]
                    ))
                  }}
                  disabled={interactionDisabled}
                  className="mt-0.5 h-3.5 w-3.5 rounded-sm"
                />
                <span className="min-w-0">
                  <span className="font-medium text-zinc-900 dark:text-zinc-100">{label}</span>
                  {option.reason && <span className="block text-[9px] text-zinc-500 dark:text-zinc-400">{String(option.reason)}</span>}
                </span>
              </label>
            )
          })}

          {selectedOptions.includes(CUSTOM_INPUT_VALUE) && (
            <textarea
              className={textAreaClassName()}
              placeholder="请填写自定义回答"
              rows={2}
              value={primaryTextInput}
              onChange={(e) => setPrimaryTextInput(e.target.value)}
              disabled={interactionDisabled}
            />
          )}
        </div>
      )}

      {info.decisions.length === 0 && info.selectMode === 'confirm' && (
        <div className="mb-2 space-y-2 rounded-sm border border-zinc-200 bg-white p-2 text-[11px] dark:border-zinc-800 dark:bg-zinc-900/40">
          <div className="grid gap-2 sm:grid-cols-3">
            <label className={optionClassName(selectedOption === PRIMARY_CONFIRM_YES_VALUE)}>
              <input
                type="radio"
                name={`interrupt-${checkpointId}-confirm`}
                checked={selectedOption === PRIMARY_CONFIRM_YES_VALUE}
                onChange={() => setSelectedOption(PRIMARY_CONFIRM_YES_VALUE)}
                disabled={interactionDisabled}
                className="h-3.5 w-3.5"
              />
              <span>是</span>
            </label>
            <label className={optionClassName(selectedOption === PRIMARY_CONFIRM_NO_VALUE)}>
              <input
                type="radio"
                name={`interrupt-${checkpointId}-confirm`}
                checked={selectedOption === PRIMARY_CONFIRM_NO_VALUE}
                onChange={() => setSelectedOption(PRIMARY_CONFIRM_NO_VALUE)}
                disabled={interactionDisabled}
                className="h-3.5 w-3.5"
              />
              <span>否</span>
            </label>
            <label className={optionClassName(selectedOption === CUSTOM_INPUT_VALUE)}>
              <input
                type="radio"
                name={`interrupt-${checkpointId}-confirm`}
                checked={selectedOption === CUSTOM_INPUT_VALUE}
                onChange={() => setSelectedOption(CUSTOM_INPUT_VALUE)}
                disabled={interactionDisabled}
                className="h-3.5 w-3.5"
              />
              <span>{CUSTOM_INPUT_LABEL}</span>
            </label>
          </div>

          {selectedOption === CUSTOM_INPUT_VALUE && (
            <textarea
              className={textAreaClassName()}
              placeholder="请填写自定义回答"
              rows={2}
              value={primaryTextInput}
              onChange={(e) => setPrimaryTextInput(e.target.value)}
              disabled={interactionDisabled}
            />
          )}
        </div>
      )}

      {info.decisions.length === 0 && info.selectMode === 'text' && (
        <div className="mb-2">
          <textarea
            className={textAreaClassName()}
            placeholder="请输入您的决策说明"
            rows={3}
            value={primaryTextInput}
            onChange={(e) => setPrimaryTextInput(e.target.value)}
            disabled={interactionDisabled}
          />
        </div>
      )}

      {info.type === 'plan_confirmation' && (objective || steps.length > 0 || risks.length > 0) && (
        <div className="mb-2 rounded-sm border border-zinc-200 bg-white p-2 text-[11px] dark:border-zinc-800 dark:bg-zinc-900/40">
          {objective && (
            <div className="mb-1">
              <span className="font-medium">目标：</span>
              {objective}
            </div>
          )}
          {steps.length > 0 && (
            <div className="mb-1">
              <div className="font-medium">执行步骤：</div>
              <ol className="list-inside list-decimal">
                {steps.map((step, idx) => (
                  <li key={idx}>
                    <span className="font-mono">[{String(step.agent || 'agent')}]</span> {String(step.action || '')}
                  </li>
                ))}
              </ol>
            </div>
          )}
          {risks.length > 0 && (
            <div className="text-red-700 dark:text-red-300">
              <span className="font-medium">风险：</span>
              {risks.join('、')}
            </div>
          )}
        </div>
      )}

      <div className="mb-2">
        <input
          className="h-6 w-full rounded-sm border border-zinc-200 bg-zinc-100 px-2 text-[11px] text-zinc-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-200"
          placeholder="可选：补充确认信息"
          value={manualInput}
          onChange={(e) => setManualInput(e.target.value)}
          disabled={interactionDisabled}
        />
      </div>

      {validationMessage && (
        <div className="mb-2 text-[10px] text-red-600 dark:text-red-300">{validationMessage}</div>
      )}

      {!resolvedState && (
        <div className="flex items-center justify-between gap-2 border-t border-zinc-100 pt-2 dark:border-zinc-800">
          <div className="flex flex-wrap items-center gap-1">
            {info.decisions.length > 1 && info.decisions.map((decision, idx) => {
              const answered = isDecisionAnswered(decision, decisionAnswers[decision.id])
              const isActive = idx === activeDecisionIndex
              return (
                <button
                  key={`${decision.id}-${idx}`}
                  type="button"
                  className={cn(
                    'rounded-sm border px-1.5 py-0.5 text-[8px]',
                    decisionStepClassName(answered, isActive),
                  )}
                  onClick={() => setActiveDecisionIndex(idx)}
                  disabled={interactionDisabled}
                  title={decision.title}
                >
                  Q{idx + 1}{answered ? '✓' : '○'}
                </button>
              )
            })}
          </div>
          <div className="flex items-center gap-2">
          {info.category !== 'flow_confirmation' && <button
            type="button"
            className="rounded-sm border border-zinc-200 px-2 py-1 text-[9px] text-zinc-600 hover:bg-zinc-100 disabled:opacity-60 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-800"
            onClick={() => {
              if (interactionDisabled) {
                return
              }
              onReject()
            }}
            disabled={interactionDisabled}
          >
            拒绝
          </button>}
          {showNextDecisionAction ? (
            <button
              type="button"
              className="rounded-sm bg-zinc-900 px-2 py-1 text-[9px] text-white hover:bg-zinc-800 disabled:opacity-60 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-200"
              onClick={() => {
                setActiveDecisionIndex((prev) => Math.min(info.decisions.length - 1, prev + 1))
              }}
              disabled={interactionDisabled || !canMoveToNextDecision}
            >
              下一项
            </button>
          ) : (
            <button
              type="button"
              className="rounded-sm bg-zinc-900 px-2 py-1 text-[9px] text-white hover:bg-zinc-800 disabled:opacity-60 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-200"
              onClick={() => {
                if (interactionDisabled) {
                  return
                }

                const payload: Record<string, unknown> = {}

                if (info.decisions.length > 0) {
                  const decisionPayload: Record<string, DecisionAnswerPayload> = {}
                  info.decisions.forEach((decision) => {
                    const parsed = buildDecisionPayload(decision, decisionAnswers[decision.id])
                    if (parsed) {
                      decisionPayload[decision.id] = parsed
                    }
                  })
                  if (Object.keys(decisionPayload).length > 0) {
                    payload.decisions = decisionPayload
                  }
                } else if (info.selectMode === 'single' && selectedOption) {
                  if (selectedOption === CUSTOM_INPUT_VALUE) {
                    const customText = primaryTextInput.trim()
                    if (customText) {
                      payload.text = customText
                    }
                  } else {
                    payload.option = selectedOption
                  }
                } else if (info.selectMode === 'multiple' && selectedOptions.length > 0) {
                  const selectedWithoutCustom = selectedOptions.filter((item) => item !== CUSTOM_INPUT_VALUE)
                  const hasCustomSelected = selectedOptions.includes(CUSTOM_INPUT_VALUE)
                  if (selectedWithoutCustom.length > 0) {
                    payload.options = selectedWithoutCustom
                  }
                  if (hasCustomSelected && primaryTextInput.trim()) {
                    payload.text = primaryTextInput.trim()
                  }
                } else if (info.selectMode === 'text' && primaryTextInput.trim()) {
                  payload.input = primaryTextInput.trim()
                } else if (info.selectMode === 'confirm') {
                  if (selectedOption === PRIMARY_CONFIRM_YES_VALUE) {
                    payload.confirmed = true
                  } else if (selectedOption === PRIMARY_CONFIRM_NO_VALUE) {
                    payload.confirmed = false
                  } else if (selectedOption === CUSTOM_INPUT_VALUE && primaryTextInput.trim()) {
                    payload.text = primaryTextInput.trim()
                  }
                }

                const manualText = manualInput.trim()
                if (manualText) {
                  const existingText = typeof payload.text === 'string' ? payload.text : ''
                  payload.text = existingText ? `${existingText}\n补充：${manualText}` : manualText
                }

                onApprove(Object.keys(payload).length > 0 ? payload : undefined)
              }}
              disabled={interactionDisabled || (info.decisions.length > 0 ? !allDecisionsAnswered : requiredUnfilled)}
            >
              确认并继续
            </button>
          )}
          </div>
        </div>
      )}
    </div>
  )
}
