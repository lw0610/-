import React from 'react'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ToolCenterWorkspace from './ToolCenterWorkspace'

const apiMocks = vi.hoisted(() => ({
  getSkillPackages: vi.fn(),
  getSkillPackage: vi.fn(),
  toggleSkillPackage: vi.fn(),
  getToolCatalog: vi.fn(),
}))

vi.mock('@/hooks/useSettingsActionStatus', () => ({
  useSettingsActionStatus: () => ({
    actionStatus: null,
    publishActionStatus: vi.fn(),
  }),
}))

vi.mock('@/lib/api', () => ({
  __esModule: true,
  default: {
    getSkillPackages: apiMocks.getSkillPackages,
    getSkillPackage: apiMocks.getSkillPackage,
    toggleSkillPackage: apiMocks.toggleSkillPackage,
    getToolCatalog: apiMocks.getToolCatalog,
  },
  describeSettingsMutation: () => ({
    message: 'ok',
    warning: '',
  }),
}))

describe('ToolCenterWorkspace', () => {
  beforeEach(() => {
    apiMocks.getSkillPackages.mockReset()
    apiMocks.getSkillPackage.mockReset()
    apiMocks.toggleSkillPackage.mockReset()
    apiMocks.getToolCatalog.mockReset()

    apiMocks.getSkillPackages.mockResolvedValue([
      {
        package_id: 'creator-tools',
        name: 'creator-tools',
        description: '创建和维护技能包与方案配置的内部工具集合。',
        kind: 'category',
        backend: 'internal',
        source: 'builtin',
        enabled: true,
        tags: ['internal'],
        requires_tools: [],
        tool_count: 9,
        has_tools: true,
        has_config: false,
        path: '/mock/creator-tools',
        skill_path: '/mock/creator-tools/SKILL.md',
        updated_at: '2026-03-31T00:00:00Z',
        size_bytes: 100,
        scope: 'builtin',
      },
    ])
    apiMocks.getSkillPackage.mockResolvedValue({
      package_id: 'creator-tools',
      name: 'creator-tools',
      description: '创建和维护技能包与方案配置的内部工具集合。',
      kind: 'category',
      backend: 'internal',
      source: 'builtin',
      enabled: true,
      tags: ['internal'],
      requires_tools: [],
      tool_count: 9,
      has_tools: true,
      has_config: false,
      path: '/mock/creator-tools',
      skill_path: '/mock/creator-tools/SKILL.md',
      updated_at: '2026-03-31T00:00:00Z',
      size_bytes: 100,
      scope: 'builtin',
      markdown_body: '# creator-tools',
      tools: [],
      config: {},
      references: [],
    })
  })

  it('切换到工具目录时直接请求 global 目录', async () => {
    apiMocks.getToolCatalog.mockResolvedValue({
      catalog: {
        id: 'tool_catalog/global',
        version: '1',
        availability: 'available',
        degradation_status: 'none',
        items: [],
      },
      tool_infos: [
        {
          name: 'save_skill_script',
          description: '将脚本文件写入指定技能包目录。',
          category: 'internal',
          source: 'bridge',
          backend: 'internal',
          package_id: 'creator-tools',
          available: true,
          instance_ids: ['instance-a'],
        },
      ],
      tools: ['save_skill_script'],
    })

    render(<ToolCenterWorkspace />)

    expect(await screen.findByText('creator-tools')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '工具目录' }))

    expect(await screen.findByText('save_skill_script')).toBeInTheDocument()
    expect(apiMocks.getToolCatalog).toHaveBeenCalledTimes(1)
    expect(apiMocks.getToolCatalog).toHaveBeenCalledWith('global')
    await waitFor(() => {
      expect(screen.getByText('统一工具目录')).toBeInTheDocument()
    })
  })

  it('工具目录异常为空时展示 catalog degraded 字典提示', async () => {
    apiMocks.getToolCatalog.mockResolvedValue({
      catalog: {
        id: 'tool_catalog/global',
        version: '1',
        availability: 'unavailable',
        degradation_status: 'abnormal_empty',
        missing_dependency_reason: 'abnormal_empty',
        items: [],
      },
      tool_infos: [],
      tools: ['buffer'],
    })

    render(<ToolCenterWorkspace />)

    expect(await screen.findByText('creator-tools')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '工具目录' }))

    expect(await screen.findByText('abnormal_empty')).toBeInTheDocument()
    expect(screen.getByText('工具目录异常为空，当前能力已降级。')).toBeInTheDocument()
    expect(screen.getByText('请刷新工具目录或检查运行态注册是否完成。')).toBeInTheDocument()
  })
})
