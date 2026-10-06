import React from 'react'
import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import Chip, { MetricPill } from '@/components/common/Chip'

describe('Chip', () => {
  it('渲染子元素文本', () => {
    render(<Chip>测试标签</Chip>)
    expect(screen.getByText('测试标签')).toBeInTheDocument()
  })

  it.each([
    ['zinc', 'border-zinc-200'],
    ['blue', 'border-blue-200'],
    ['amber', 'border-amber-200'],
    ['emerald', 'border-emerald-200'],
    ['red', 'border-red-200'],
    ['violet', 'border-zinc-200'],
    ['cyan', 'border-cyan-200'],
  ] as const)('tone=%s 应用正确的样式类', (tone, expectedClass) => {
    render(<Chip tone={tone}>label</Chip>)
    const chip = screen.getByText('label')
    expect(chip.className).toContain(expectedClass)
  })

  it('dot=true 时渲染状态点', () => {
    const { container } = render(<Chip dot>label</Chip>)
    const spans = container.querySelectorAll('span')
    expect(spans.length).toBe(2)
  })

  it('默认使用 rounded 形状 (rounded-sm)', () => {
    render(<Chip>label</Chip>)
    expect(screen.getByText('label').className).toContain('rounded-sm')
  })

  it('shape="rounded" 时使用 rounded-sm', () => {
    render(<Chip shape="rounded">label</Chip>)
    expect(screen.getByText('label').className).toContain('rounded-sm')
  })

  it('size xs 使用 text-[9px]', () => {
    render(<Chip size="xs">label</Chip>)
    expect(screen.getByText('label').className).toContain('text-[9px]')
  })

  it('size sm 使用 text-[10px]', () => {
    render(<Chip size="sm">label</Chip>)
    expect(screen.getByText('label').className).toContain('text-[10px]')
  })
})

describe('MetricPill', () => {
  it('渲染 label 和 value', () => {
    render(<MetricPill label="CPU" value="42%" />)
    expect(screen.getByText('CPU')).toBeInTheDocument()
    expect(screen.getByText('42%')).toBeInTheDocument()
  })
})
