import { render, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import SystemSettingsPage from './page'

const {
  batchSetConfigMock,
  refreshConfigMock,
  capturedUseSettingsFormOptions,
} = vi.hoisted(() => ({
  batchSetConfigMock: vi.fn(),
  refreshConfigMock: vi.fn(),
  capturedUseSettingsFormOptions: {
    current: null as null | Record<string, unknown>,
  },
}))

vi.mock('sonner', () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
    warning: vi.fn(),
  },
}))

vi.mock('@/components/layout/AppShell', () => ({
  default: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}))

vi.mock('@/components/layout/WorkspaceShell', () => ({
  default: ({ children, actionBar }: { children: React.ReactNode; actionBar?: React.ReactNode }) => (
    <div>
      {actionBar}
      {children}
    </div>
  ),
}))

vi.mock('@/components/settings/ActionBar', () => ({
  default: () => <div data-testid="action-bar" />,
  ActionButton: ({ children }: { children: React.ReactNode }) => <button type="button">{children}</button>,
}))

vi.mock('@/components/settings/FormSection', () => ({
  default: ({ children }: { children: React.ReactNode }) => <section>{children}</section>,
}))

vi.mock('@/components/settings/SectionNav', () => ({
  default: () => <nav data-testid="section-nav" />,
}))

// The integration card owns a separate form, covered by integrationsSettings.
// Keep this test's captured form scoped to the system settings save path.
vi.mock('@/components/settings/IntegrationsConfigWorkspace', () => ({
  default: () => <div data-testid="integrations-config" />,
}))

vi.mock('@/components/settings/FormField', () => ({
  Field: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  CheckboxField: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  NumberInput: (props: React.InputHTMLAttributes<HTMLInputElement>) => <input {...props} />,
  PathInput: (props: React.InputHTMLAttributes<HTMLInputElement>) => <input {...props} />,
  SelectInput: ({ children, ...props }: React.SelectHTMLAttributes<HTMLSelectElement>) => <select {...props}>{children}</select>,
  TextInput: (props: React.InputHTMLAttributes<HTMLInputElement>) => <input {...props} />,
}))

vi.mock('@/hooks/useSettingsActionStatus', () => ({
  useSettingsActionStatus: () => ({
    actionStatus: null,
    publishActionStatus: vi.fn(),
  }),
}))

vi.mock('@/hooks/useSettingsForm', () => ({
  useSettingsForm: (options: Record<string, unknown>) => {
    capturedUseSettingsFormOptions.current = options
    const defaultValues = options.defaultValues as Record<string, unknown>
    return {
      form: defaultValues,
      setForm: vi.fn(),
      isDirty: false,
      loading: false,
      saving: false,
      reload: vi.fn(),
      handleSave: vi.fn(),
      resetToDefault: vi.fn(),
    }
  },
}))

vi.mock('@/lib/api', () => ({
  default: {
    batchSetConfig: batchSetConfigMock,
  },
}))

vi.mock('@/stores/settingsStore', () => ({
  useSettingsStore: (selector: (state: { refreshConfig: typeof refreshConfigMock }) => unknown) =>
    selector({ refreshConfig: refreshConfigMock }),
}))

vi.mock('@/lib/config-form-helpers', () => ({
  DEFAULT_CUSTOM_NAME_PATTERN: '{title}',
  TIMESTAMP_PATTERN: '{timestamp}',
  inferPatternMode: () => ({ mode: 'timestamp', custom: '{title}' }),
  pickQGISExecutable: vi.fn(),
  resolvePattern: () => '{timestamp}',
}))

vi.mock('@/lib/config-utils', () => ({
  asBool: (value: unknown, fallback: boolean) =>
    typeof value === 'boolean' ? value : fallback,
  asNumber: (value: unknown, fallback: number) =>
    typeof value === 'number' ? value : fallback,
  asRecord: (value: unknown) =>
    value && typeof value === 'object' ? (value as Record<string, unknown>) : {},
  asString: (value: unknown, fallback = '') =>
    typeof value === 'string' ? value : fallback,
  clampInt: (value: number) => Math.trunc(value),
}))

vi.mock('@/lib/runIntent', () => ({
  parseExecutionMode: () => 'build',
}))

describe('SystemSettingsPage save path', () => {
  beforeEach(() => {
    batchSetConfigMock.mockReset()
    refreshConfigMock.mockReset()
    capturedUseSettingsFormOptions.current = null
    batchSetConfigMock.mockResolvedValue({ success: true })
    refreshConfigMock.mockResolvedValue(undefined)
  })

  it('saves through api.batchSetConfig instead of local config mutation bypass', async () => {
    render(<SystemSettingsPage />)

    await waitFor(() => {
      expect(capturedUseSettingsFormOptions.current).not.toBeNull()
    })

    const options = capturedUseSettingsFormOptions.current as {
      save: (values: Record<string, unknown>) => Promise<unknown>
      defaultValues: Record<string, unknown>
    }

    const values = {
      ...options.defaultValues,
      qgisExecutablePath: '/usr/bin/qgis',
      logLevel: 'DEBUG',
      maxConcurrentTasks: 7,
    }

    await options.save(values)

    expect(batchSetConfigMock).toHaveBeenCalledTimes(1)
    expect(batchSetConfigMock).toHaveBeenCalledWith(
      expect.arrayContaining([
        expect.objectContaining({ path: 'log.level', value: 'DEBUG' }),
        expect.objectContaining({ path: 'qgis.executable_path', value: '/usr/bin/qgis' }),
        expect.objectContaining({ path: 'performance.max_concurrent_tasks', value: 7 }),
      ]),
    )
    expect(refreshConfigMock).toHaveBeenCalledTimes(1)
  })
})
