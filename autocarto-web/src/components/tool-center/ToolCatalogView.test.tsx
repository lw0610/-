import React from 'react'
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import ToolCatalogView from './ToolCatalogView'

describe('ToolCatalogView', () => {
  it('展示工具目录与可用性字段', () => {
    render(
      <ToolCatalogView
        catalog={{
          id: 'tool_catalog/global',
          version: '1',
          availability: 'degraded',
          degradation_status: 'provider_missing',
          missing_dependency_reason: 'provider_missing',
          items: [],
        }}
        loading={false}
        items={[
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
          {
            name: 'buffer',
            description: '创建缓冲区。',
            category: 'vector',
            source: 'builtin',
            backend: 'qgis',
            package_id: 'vector-geometry',
            available: false,
            missing_reason: 'unsupported_runtime',
            evidence_id: 'EVIDENCE-unsupported-runtime',
            instance_ids: [],
          },
        ]}
      />,
    )

    expect(screen.getByText('能力手册')).toBeInTheDocument()
    expect(screen.getByText('save_skill_script')).toBeInTheDocument()
    expect(screen.getByText('buffer')).toBeInTheDocument()
    expect(screen.getByText('可用')).toBeInTheDocument()
    expect(screen.getByText('不可用')).toBeInTheDocument()
    expect(screen.getByText('unsupported_runtime')).toBeInTheDocument()
    expect(screen.getByText('当前运行时不支持该能力。')).toBeInTheDocument()
    expect(screen.getByText('请切换到受支持的运行时或启动对应服务。')).toBeInTheDocument()
    expect(
      screen.getByText((content) => content.includes('EVIDENCE-') && content.includes('unsupported-runtime')),
    ).toBeInTheDocument()
    expect(screen.getByText(/实例：instance-a/)).toBeInTheDocument()
  })
})
