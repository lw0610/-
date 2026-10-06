import { beforeEach, expect, test } from 'vitest'

import { EMPTY_RUNTIME_SNAPSHOT, type RuntimeSnapshot } from '@/lib/api'
import { useInstanceStore } from '@/stores/instanceStore'
import { useRuntimeSnapshotStore } from '@/stores/runtimeSnapshotStore'
import type { InstanceEvent, QGISInstance } from '@/types/instance'

function buildInstance(overrides: Partial<QGISInstance> & Pick<QGISInstance, 'id' | 'status'>): QGISInstance {
  const now = '2026-03-19T12:00:00.000Z'
  return {
    id: overrides.id,
    pid: overrides.pid ?? 1001,
    qgis_version: overrides.qgis_version ?? '3.34',
    project_path: overrides.project_path ?? '',
    status: overrides.status,
    services: overrides.services ?? {},
    created_at: overrides.created_at ?? now,
    updated_at: overrides.updated_at ?? now,
    last_heartbeat: overrides.last_heartbeat ?? now,
    source: overrides.source ?? 'reverse_ws',
    machine_fingerprint: overrides.machine_fingerprint,
    start_time: overrides.start_time,
  }
}

function resetStores() {
  useRuntimeSnapshotStore.setState({
    snapshot: EMPTY_RUNTIME_SNAPSHOT,
    loading: false,
  })
  useInstanceStore.setState({
    instances: [],
    activeInstanceId: null,
    qgisAvailable: false,
    projectConnected: false,
    instanceCount: 0,
    runningCount: 0,
    stale: false,
    loading: false,
  })
}

beforeEach(() => {
  resetStores()
})

function buildSnapshot(instances: QGISInstance[], activeID?: string): RuntimeSnapshot {
  return {
    ...EMPTY_RUNTIME_SNAPSHOT,
    instances: {
      items: instances,
      active_id: activeID,
      instance_count: instances.length,
      running_count: instances.filter((instance) => instance.status === 'running').length,
    },
  }
}

test('syncFromRuntimeSnapshot 会保留非运行实例且不在无效 active_id 时本地回退', () => {
  const stopped = buildInstance({
    id: 'qgis-stopped',
    status: 'stopped',
    project_path: '/tmp/stopped.qgz',
  })
  const runningFirst = buildInstance({
    id: 'qgis-running-a',
    status: 'running',
  })
  const runningSecond = buildInstance({
    id: 'qgis-running-b',
    status: 'running',
    project_path: '/tmp/running.qgz',
  })

  const snapshot = {
    ...buildSnapshot([stopped, runningFirst, runningSecond], 'qgis-stopped'),
    stale: true,
  }

  useInstanceStore.getState().syncFromRuntimeSnapshot(snapshot)

  const state = useInstanceStore.getState()
  expect(
    state.instances.map((instance) => instance.id),
  ).toEqual(['qgis-stopped', 'qgis-running-a', 'qgis-running-b'])
  expect(state.activeInstanceId).toBeNull()
  expect(state.instanceCount).toBe(3)
  expect(state.runningCount).toBe(2)
  expect(state.qgisAvailable).toBe(true)
  expect(state.projectConnected).toBe(false)
  expect(state.stale).toBe(true)
})

test('syncFromRuntimeSnapshot 在 backend 未给出可用 active_id 时不会沿用旧实例', () => {
  const running = buildInstance({
    id: 'qgis-running-kept',
    status: 'running',
    project_path: '/tmp/kept.qgz',
  })
  useInstanceStore.setState({
    instances: [running],
    activeInstanceId: running.id,
    qgisAvailable: true,
    projectConnected: true,
    instanceCount: 1,
    runningCount: 1,
  })

  const stopped = buildInstance({
    id: 'qgis-stopped',
    status: 'stopped',
  })

  useInstanceStore.getState().syncFromRuntimeSnapshot(
    buildSnapshot([stopped, running], stopped.id),
  )

  const state = useInstanceStore.getState()
  expect(state.activeInstanceId).toBeNull()
  expect(state.projectConnected).toBe(false)
  expect(state.instances.map((instance) => instance.id)).toEqual([stopped.id, running.id])
})

test('syncFromRuntimeSnapshot 会保留 expired 实例的解释字段', () => {
  const expired = {
    ...buildInstance({
      id: 'qgis-expired',
      status: 'dead',
      project_path: '/tmp/expired.qgz',
    }),
    status_reason: 'lease_expired',
    status_detail: '实例心跳已过期，等待重新注册恢复。',
  } as QGISInstance

  useInstanceStore.getState().syncFromRuntimeSnapshot(buildSnapshot([expired]))

  const state = useInstanceStore.getState()
  const instance = state.instances[0] as QGISInstance & {
    status_reason?: string
    status_detail?: string
  }
  expect(state.instanceCount).toBe(1)
  expect(state.runningCount).toBe(0)
  expect(instance.id).toBe('qgis-expired')
  expect(instance.status).toBe('dead')
  expect(instance.status_reason).toBe('lease_expired')
  expect(instance.status_detail).toBe('实例心跳已过期，等待重新注册恢复。')
})

test('handleInstanceEvent 会保留变为 stopped 的当前实例并清空活跃态', () => {
  const running = buildInstance({
    id: 'qgis-running',
    status: 'running',
    project_path: '/tmp/current.qgz',
  })
  useInstanceStore.setState({
    instances: [running],
    activeInstanceId: running.id,
    qgisAvailable: true,
    projectConnected: true,
    instanceCount: 1,
    runningCount: 1,
  })

  const stoppedEvent: InstanceEvent = {
    type: 'changed',
    instance: buildInstance({
      id: running.id,
      status: 'stopped',
      project_path: running.project_path,
    }),
  }

  useInstanceStore.getState().handleInstanceEvent(stoppedEvent)

  const state = useInstanceStore.getState()
  expect(state.instances).toEqual([stoppedEvent.instance])
  expect(state.activeInstanceId).toBeNull()
  expect(state.qgisAvailable).toBe(false)
  expect(state.projectConnected).toBe(false)
  expect(state.instanceCount).toBe(1)
  expect(state.runningCount).toBe(0)
})
