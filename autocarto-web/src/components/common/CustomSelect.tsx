'use client'

import { Fragment } from 'react'
import { Listbox, Transition } from '@headlessui/react'
import { cn } from '@/lib/utils'

export interface CustomSelectOption {
  value: string
  label: string
  hint?: string
  disabled?: boolean
  disabledReason?: string
}

interface Props {
  label?: string
  value: string
  options: CustomSelectOption[]
  onChange: (value: string) => void
  disabled?: boolean
  compact?: boolean
  /** 下拉面板展开方向，默认 bottom；工具栏等底部场景用 top */
  placement?: 'top' | 'bottom'
  buttonClassName?: string
  optionClassName?: string
}

export default function CustomSelect({
  label,
  value,
  options,
  onChange,
  disabled,
  compact,
  placement = 'bottom',
  buttonClassName,
  optionClassName,
}: Props) {
  const selected = options.find((item) => item.value === value) || options[0]
  const selectedHint = selected?.disabled
    ? selected.disabledReason || selected.hint || selected.label || ''
    : selected?.hint || selected?.label || ''

  return (
    <Listbox value={selected?.value || ''} onChange={onChange} disabled={disabled}>
      {({ open }) => (
        <div className={cn('relative min-w-[140px]', compact && 'min-w-[96px]', open && 'z-50')}>
          {label && !compact && <div className="mb-1 text-[11px] uppercase tracking-wide text-text-muted">{label}</div>}
          <Listbox.Button
            title={selectedHint}
            className={cn(
              compact
                ? 'w-full rounded-sm border border-border-default bg-surface-overlay px-2.5 py-1.5 text-left text-[11px] text-text-secondary transition-colors'
                : 'w-full rounded-sm border border-border-default bg-surface-overlay px-3 py-2 text-left text-[11px] text-text-secondary transition-colors',
              'hover:border-border-strong hover:bg-surface-sunken',
              'focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-surface-base',
              disabled && 'cursor-not-allowed opacity-60',
              buttonClassName,
            )}
          >
            <span className="flex items-center justify-between gap-2">
              <span className="truncate">{selected?.label || '-'}</span>
              <span className="text-text-faint">▾</span>
            </span>
          </Listbox.Button>

          <Transition
            as={Fragment}
            leave="transition ease-in duration-100"
            leaveFrom="opacity-100"
            leaveTo="opacity-0"
          >
            <Listbox.Options
              className={cn(
                'absolute left-0 z-[70] max-h-[60vh] min-w-[220px] w-max overflow-y-auto rounded border border-border-default bg-surface-overlay p-1.5 shadow-lg',
                placement === 'top' ? 'bottom-full mb-1' : 'mt-1',
                compact ? 'text-[11px]' : 'text-[11px]',
              )}
            >
              {options.map((option) => (
                <Listbox.Option
                  key={option.value}
                  value={option.value}
                  disabled={option.disabled}
                  className={({ active, disabled: optionDisabled }) =>
                    cn(
                      compact ? 'rounded-sm px-2.5 py-2' : 'rounded-sm px-2.5 py-2',
                      optionDisabled
                        ? 'cursor-not-allowed opacity-60'
                        : 'cursor-pointer',
                      active && !optionDisabled
                        ? 'bg-accent/10 text-accent'
                        : 'text-text-secondary',
                      optionClassName,
                    )
                  }
                >
                  {({ selected: activeSelected }) => (
                    <div className="space-y-0.5">
                      <div className={cn('whitespace-normal break-words', activeSelected && 'font-medium')}>
                        {option.label}
                      </div>
                      {(option.hint || option.disabledReason) && (
                        <div className="whitespace-normal break-words text-[11px] text-text-muted">
                          {option.disabledReason || option.hint}
                        </div>
                      )}
                    </div>
                  )}
                </Listbox.Option>
              ))}
            </Listbox.Options>
          </Transition>
        </div>
      )}
    </Listbox>
  )
}
