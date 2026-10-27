import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import ModelConfigWorkspace from './ModelConfigWorkspace'
import { useSettingsStore } from '@/stores/settingsStore'
const mocks = vi.hoisted(() => ({
  save: vi.fn().mockResolvedValue({ persisted: true, required_action: 'none' }),
  registry: vi.fn().mockResolvedValue([]),
}))
vi.mock('@/lib/api', async (original) => ({
  ...(await original<typeof import('@/lib/api')>()),
  default: { saveLLMProviders: mocks.save, getLLMRegistry: mocks.registry },
}))
it('round-trips and updates actual vision capability through existing model save', async () => {
  useSettingsStore.setState({
    llmProviders: {
      active: 'fixture',
      providers: [
        {
          name: 'fixture',
          provider: 'openai',
          model: 'fixture-model',
          vision: true,
          has_api_key: true,
        },
      ],
    },
    refreshLLMProviders: vi.fn().mockResolvedValue(undefined),
  })
  render(<ModelConfigWorkspace />)
  const checkbox = await screen.findByRole('checkbox', { name: /支持读图/ })
  expect(checkbox).toBeChecked()
  fireEvent.click(checkbox)
  fireEvent.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() =>
    expect(mocks.save).toHaveBeenCalledWith(
      expect.objectContaining({
        providers: [
          expect.objectContaining({ name: 'fixture', vision: false }),
        ],
      }),
    ),
  )
})
