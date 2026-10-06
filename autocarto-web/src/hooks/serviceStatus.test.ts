import { describe, expect, it } from 'vitest'

import type { RuntimeServicesSnapshot } from '@/lib/api'
import { buildTopBarServiceStatusItems } from '@/lib/serviceStatus'

describe('serviceStatus', () => {
  const baseSnapshot: RuntimeServicesSnapshot = {
    services: {},
    instances: {},
    stale: false,
  }

  describe('buildTopBarServiceStatusItems', () => {
    it('no longer surfaces middleware SSE status items', () => {
      const snapshot: RuntimeServicesSnapshot = {
        ...baseSnapshot,
      }

      const items = buildTopBarServiceStatusItems({
        snapshot,
        websocketConnected: true,
        projectConnected: true,
      })

      const sseItem = items.find((item) => item.id === 'middleware_sse')
      expect(sseItem).toBeUndefined()
    })

    it('surfaces restricted mode readiness issues', () => {
      const snapshot: RuntimeServicesSnapshot = {
        ...baseSnapshot,
        mode_readiness: {
          build: { ready: false, message: '服务当前以受限模式运行' },
        },
      }

      const items = buildTopBarServiceStatusItems({
        snapshot,
        websocketConnected: true,
        projectConnected: true,
      })

      const buildItem = items.find((item) => item.id === 'mode_build')
      expect(buildItem).toBeDefined()
      expect(buildItem?.status).toBe('restricted')
      expect(buildItem?.tone).toBe('warning')
      expect(buildItem?.meta).toContain('受限模式')
    })

    it('surfaces compute service_ready true but qgis_ready false warnings', () => {
      const snapshot: RuntimeServicesSnapshot = {
        ...baseSnapshot,
        services: {
          compute: {
            status: 'running',
            ready: true,
            diagnostics: {
              service_ready: true,
              qgis_ready: false,
              startup_error: 'Connection timeout',
              startup_phase: 'connecting',
            },
          },
        },
      }

      const items = buildTopBarServiceStatusItems({
        snapshot,
        websocketConnected: true,
        projectConnected: true,
      })

      const computeItem = items.find((item) => item.id === 'cli')
      expect(computeItem).toBeDefined()
      expect(computeItem?.tone).toBe('warning')
      expect(computeItem?.lastError).toBe('Connection timeout')
      expect(computeItem?.meta).toContain('阶段 connecting')
    })
  })
})
