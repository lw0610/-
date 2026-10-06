import React from 'react'
import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import StateBlock from '@/components/common/StateBlock'

describe('StateBlock', () => {
  it('渲染 title 和 description', () => {
    render(<StateBlock title="标题" description="描述文字" />)
    expect(screen.getByText('标题')).toBeInTheDocument()
    expect(screen.getByText('描述文字')).toBeInTheDocument()
  })

  it('loading=true 时显示加载 spinner', () => {
    const { container } = render(<StateBlock title="t" description="d" loading />)
    const spinner = container.querySelector('.animate-spin')
    expect(spinner).toBeInTheDocument()
  })

  it('提供 icon 时渲染自定义图标', () => {
    render(<StateBlock title="t" description="d" icon={<span data-testid="custom-icon" />} />)
    expect(screen.getByTestId('custom-icon')).toBeInTheDocument()
  })

  it('未提供 icon 时渲染默认 SVG 图标', () => {
    const { container } = render(<StateBlock title="t" description="d" />)
    const svg = container.querySelector('svg')
    expect(svg).toBeInTheDocument()
  })
})
