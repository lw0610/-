import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '@/lib/api'
import { useSettingsForm } from '@/hooks/useSettingsForm'

const { toastErrorMock, toastSuccessMock, toastWarningMock } = vi.hoisted(() => ({
  toastErrorMock: vi.fn(),
  toastSuccessMock: vi.fn(),
  toastWarningMock: vi.fn(),
}))

vi.mock('sonner', () => ({
  toast: {
    error: toastErrorMock,
    success: toastSuccessMock,
    warning: toastWarningMock,
  },
}))

describe('useSettingsForm', () => {
  beforeEach(() => {
    toastErrorMock.mockReset()
    toastSuccessMock.mockReset()
    toastWarningMock.mockReset()
  })

  it('propagates evidence metadata when save fails', async () => {
    const onSaveFeedback = vi.fn()
    const save = vi.fn(async () => {
      throw new ApiError('Failed to save config', 400, 'CONFIG_BATCH_INVALID', {
        evidence_id: 'EVIDENCE-1234',
        correlation_id: 'CORR-5678',
        required_action: 'runtime_rebuild',
      })
    })

    const { result } = renderHook(() =>
      useSettingsForm({
        defaultValues: { qgisExecutablePath: '' },
        load: async () => ({ qgisExecutablePath: '' }),
        save,
        onSaveFeedback,
      }),
    )

    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    await act(async () => {
      result.current.setForm({ qgisExecutablePath: '/tmp/qgis' })
    })

    let saveResult = false
    await act(async () => {
      saveResult = await result.current.handleSave()
    })

    expect(saveResult).toBe(false)
    expect(save).toHaveBeenCalledWith({ qgisExecutablePath: '/tmp/qgis' })
    expect(onSaveFeedback).toHaveBeenCalledWith(
      expect.objectContaining({
        message: 'Failed to save config',
        evidence_id: 'EVIDENCE-1234',
        correlation_id: 'CORR-5678',
      }),
    )
    expect(toastErrorMock).toHaveBeenCalledWith('Failed to save config')
  })
})
