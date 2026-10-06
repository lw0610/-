import React from 'react'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi } from 'vitest'
import ChatInput from '@/components/chat/ChatInput'

describe('ChatInput', () => {
  it('渲染带 aria-label 的 textarea', () => {
    render(<ChatInput onSend={vi.fn()} />)
    expect(screen.getByRole('textbox', { name: '消息输入' })).toBeInTheDocument()
  })

  it('Enter 键调用 onSend 并传入 trimmed 文本', async () => {
    const onSend = vi.fn()
    render(<ChatInput onSend={onSend} />)
    const textarea = screen.getByRole('textbox', { name: '消息输入' })
    await userEvent.type(textarea, '  hello  ')
    await userEvent.keyboard('{Enter}')
    expect(onSend).toHaveBeenCalledWith('hello')
  })

  it('Shift+Enter 不调用 onSend', async () => {
    const onSend = vi.fn()
    render(<ChatInput onSend={onSend} />)
    const textarea = screen.getByRole('textbox', { name: '消息输入' })
    await userEvent.type(textarea, 'hello')
    await userEvent.keyboard('{Shift>}{Enter}{/Shift}')
    expect(onSend).not.toHaveBeenCalled()
  })

  it('disabled 状态下 textarea 被禁用', () => {
    render(<ChatInput onSend={vi.fn()} disabled />)
    expect(screen.getByRole('textbox', { name: '消息输入' })).toBeDisabled()
  })

  it('running 状态下按钮包含取消文字', () => {
    render(<ChatInput onSend={vi.fn()} running />)
    expect(screen.getByText('取消任务')).toBeInTheDocument()
  })

  it('发送按钮包含 sr-only 文字', () => {
    render(<ChatInput onSend={vi.fn()} />)
    expect(screen.getByText('发送')).toBeInTheDocument()
  })

  it('BUILD 模式使用绿色执行态主题', () => {
    render(<ChatInput onSend={vi.fn()} executionMode="build" />)

    expect(screen.getByText('⚡ BUILD · 直接执行')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '发送' }).className).toContain('emerald')
  })
})
