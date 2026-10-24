import React from 'react'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ToolCenterWorkspace from './ToolCenterWorkspace'

const apiMocks = vi.hoisted(() => ({
  getSkillPackages: vi.fn(),
  getSkillPackage: vi.fn(),
  toggleSkillPackage: vi.fn(),
  getToolCatalog: vi.fn(),
  publishActionStatus: vi.fn(),
}))

vi.mock('@/hooks/useSettingsActionStatus', () => ({
  useSettingsActionStatus: () => ({
    actionStatus: null,
    publishActionStatus: apiMocks.publishActionStatus,
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
  describeSettingsMutation: (_result: unknown, message: string) => ({
    message,
    warning: '',
  }),
}))

describe('ToolCenterWorkspace', () => {
  beforeEach(() => {
    apiMocks.getSkillPackages.mockReset()
    apiMocks.getSkillPackage.mockReset()
    apiMocks.toggleSkillPackage.mockReset()
    apiMocks.getToolCatalog.mockReset()
    apiMocks.publishActionStatus.mockReset()
    apiMocks.getToolCatalog.mockResolvedValue({ tool_infos: [], tools: [] })

    apiMocks.getSkillPackages.mockResolvedValue([
      {
        package_id: 'creator-tools',
        name: 'creator-tools',
        description: '创建和维护工具包与方案配置的内部工具集合。',
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
      description: '创建和维护工具包与方案配置的内部工具集合。',
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
          description: '将脚本文件写入指定工具包目录。',
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

    expect(screen.queryByRole('button', { name: '导入工具包' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '能力手册' }))
    expect(screen.queryByRole('button', { name: '导入工具包' })).not.toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    expect(await screen.findByText('save_skill_script')).toBeInTheDocument()
    expect(apiMocks.getToolCatalog).toHaveBeenCalledTimes(1)
    expect(apiMocks.getToolCatalog).toHaveBeenCalledWith('global')
    await waitFor(() => {
      expect(screen.getByText(/写流程时可以这样描述/)).toBeInTheDocument()
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
    fireEvent.click(screen.getByRole('button', { name: '能力手册' }))

    expect(await screen.findByText('abnormal_empty')).toBeInTheDocument()
    expect(screen.getByText('工具目录异常为空，当前能力已降级。')).toBeInTheDocument()
    expect(screen.getByText('请刷新工具目录或检查运行态注册是否完成。')).toBeInTheDocument()
  })

  it('收到目录变更时刷新工具包及已加载能力目录，卸载后解除监听', async () => {
    const { unmount } = render(<ToolCenterWorkspace />)
    expect(await screen.findByText('creator-tools')).toBeInTheDocument()
    act(() => window.dispatchEvent(new CustomEvent('autocarto:skill_catalog_changed')))
    await waitFor(() => expect(apiMocks.getSkillPackages).toHaveBeenCalledTimes(2))
    expect(apiMocks.getToolCatalog).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '能力手册' }))
    await waitFor(() => expect(apiMocks.getToolCatalog).toHaveBeenCalledTimes(1))
    act(() => window.dispatchEvent(new CustomEvent('autocarto:skill_catalog_changed')))
    await waitFor(() => expect(apiMocks.getToolCatalog).toHaveBeenCalledTimes(2))
    expect(apiMocks.getSkillPackages).toHaveBeenCalledTimes(3)
    unmount()
    act(() => window.dispatchEvent(new CustomEvent('autocarto:skill_catalog_changed')))
    expect(apiMocks.getSkillPackages).toHaveBeenCalledTimes(3)
    expect(apiMocks.getToolCatalog).toHaveBeenCalledTimes(2)
  })

  it('刷新后的工具目录不会被较早的请求结果覆盖', async () => {
    let resolveEarlier!: (value: unknown) => void
    apiMocks.getToolCatalog.mockReturnValueOnce(new Promise((resolve) => { resolveEarlier = resolve }))
      .mockResolvedValueOnce({ tool_infos: [{ name: 'new-tool', available: true }], tools: ['new-tool'] })
    render(<ToolCenterWorkspace />)
    expect(await screen.findByText('creator-tools')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '能力手册' }))
    act(() => window.dispatchEvent(new CustomEvent('autocarto:skill_catalog_changed')))
    expect(await screen.findByText('new-tool')).toBeInTheDocument()
    await act(async () => {
      resolveEarlier({ tool_infos: [{ name: 'old-tool', available: true }], tools: ['old-tool'] })
    })
    expect(screen.getByText('new-tool')).toBeInTheDocument()
    expect(screen.queryByText('old-tool')).not.toBeInTheDocument()
  })
  it('只展示正式工具包，系统与隐藏包折叠在系统分组', async () => {
    apiMocks.getSkillPackages.mockResolvedValue([
      {package_id:'visible',name:'visible',display_name:'可选工具包',kind:'category',enabled:true,tags:[],tool_count:1},
      {package_id:'system',name:'system',display_name:'系统基础',kind:'category',system:true,enabled:true,tags:[],tool_count:1},
      {package_id:'hidden',name:'hidden',display_name:'隐藏能力',kind:'category',hidden:true,enabled:true,tags:[],tool_count:1},
      {package_id:'old',name:'旧方法',kind:'workflow',enabled:true,tags:[],tool_count:0},
      {package_id:'retired',name:'已废弃工具包',kind:'category',deprecated:true,enabled:true,tags:[],tool_count:0},
    ])
    render(<ToolCenterWorkspace />)
    expect(await screen.findByText('可选工具包')).toBeVisible()
    expect(screen.queryByText('旧方法')).not.toBeInTheDocument()
    expect(screen.queryByText('已废弃工具包')).not.toBeInTheDocument()
    expect(screen.getByText('系统基础')).not.toBeVisible()
    fireEvent.click(screen.getByText('系统（2）'))
    expect(screen.getByText('系统基础')).toBeVisible()
    expect(screen.getByText('隐藏能力')).toBeVisible()
  })

})
