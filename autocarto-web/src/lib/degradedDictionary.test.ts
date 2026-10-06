import { describe, expect, it } from 'vitest'

import {
  getToolCatalogDegradedDictionary,
  getToolItemDegradedDictionary,
  getInstanceDegradedDictionary,
} from './degradedDictionary'

describe('degradedDictionary', () => {
  it('maps catalog abnormal_empty into shared degraded dictionary', () => {
    expect(
      getToolCatalogDegradedDictionary({
        id: 'tool_catalog/global',
        version: '1',
        availability: 'unavailable',
        degradation_status: 'abnormal_empty',
        missing_dependency_reason: 'abnormal_empty',
        items: [],
      }),
    ).toMatchObject({
      code: 'abnormal_empty',
      retryable: true,
      message: '工具目录异常为空，当前能力已降级。',
    })
  })

  it('maps provider_missing item and preserves evidence id', () => {
    expect(
      getToolItemDegradedDictionary({
        name: 'buffer',
        available: false,
        missing_reason: 'provider_missing',
        evidence_id: 'EVIDENCE-provider',
      }),
    ).toMatchObject({
      code: 'provider_missing',
      retryable: true,
      evidence_id: 'EVIDENCE-provider',
    })
  })

  it('maps expired instance explainability into shared degraded dictionary', () => {
    expect(
      getInstanceDegradedDictionary({
        id: 'instance-expired',
        pid: 1001,
        qgis_version: '3.40',
        project_path: '/tmp/demo.qgz',
        status: 'dead',
        status_reason: 'lease_expired',
        status_detail: '实例心跳已过期，等待重新注册恢复。',
        services: {},
        created_at: '2026-04-04T10:00:00Z',
        updated_at: '2026-04-04T10:00:00Z',
        last_heartbeat: '2026-04-04T10:00:00Z',
        source: 'reverse_ws',
      }),
    ).toMatchObject({
      code: 'instance_expired',
      retryable: true,
      message: '实例心跳已过期，等待重新注册恢复。',
    })
  })
})
