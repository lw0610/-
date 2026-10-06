import { create } from 'zustand'
import type { RuntimeSnapshot } from '@/lib/api'
import { QGISInstance, InstanceEvent } from '@/types/instance'
import { useRuntimeSnapshotStore } from '@/stores/runtimeSnapshotStore'

interface InstanceState {
  instances: QGISInstance[]
  activeInstanceId: string | null
  qgisAvailable: boolean
  projectConnected: boolean
  instanceCount: number
  runningCount: number
  stale: boolean
  loading: boolean
  fetchInstances: () => Promise<void>
  syncFromRuntimeSnapshot: (snapshot: RuntimeSnapshot) => void
  setActiveInstance: (id: string | null) => void
  handleInstanceEvent: (event: InstanceEvent) => void
}

function getEffectiveRunningInstances(instances: QGISInstance[]) {
  return instances.filter((inst) => inst.status === 'running')
}

function computeQGISAvailability(instances: QGISInstance[]) {
  const runningCount = instances.filter((inst) => inst.status === 'running').length
  return {
    qgisAvailable: runningCount > 0,
    instanceCount: instances.length,
    runningCount,
  }
}

function computeProjectConnected(instances: QGISInstance[], activeInstanceId: string | null) {
  if (!activeInstanceId) {
    return false
  }
  const active = instances.find((inst) => inst.id === activeInstanceId)
  if (!active) {
    return false
  }
  return active.status === 'running' && active.project_path.trim().length > 0
}

function chooseActiveInstanceId(
  instances: QGISInstance[],
  snapshotActiveInstanceId: string | undefined,
  fallbackActiveInstanceId: string | null,
  allowLocalFallback: boolean,
) {
  const runningInstances = getEffectiveRunningInstances(instances)
  const normalizedSnapshotActive = typeof snapshotActiveInstanceId === 'string'
    ? snapshotActiveInstanceId.trim()
    : ''
  if (normalizedSnapshotActive && runningInstances.some((inst) => inst.id === normalizedSnapshotActive)) {
    return normalizedSnapshotActive
  }
  if (!allowLocalFallback) {
    return null
  }
  if (fallbackActiveInstanceId && runningInstances.some((inst) => inst.id === fallbackActiveInstanceId)) {
    return fallbackActiveInstanceId
  }
  const firstRunningInstance = runningInstances[0]
  if (firstRunningInstance) {
    return firstRunningInstance.id
  }
  return null
}

function deriveInstanceProjection(
  instances: QGISInstance[],
  snapshotActiveInstanceId: string | undefined,
  fallbackActiveInstanceId: string | null,
  stale: boolean,
  allowLocalFallback = true,
) {
  const nextActiveInstanceId = chooseActiveInstanceId(
    instances,
    snapshotActiveInstanceId,
    fallbackActiveInstanceId,
    allowLocalFallback,
  )
  const derived = computeQGISAvailability(instances)

  return {
    instances,
    activeInstanceId: nextActiveInstanceId,
    qgisAvailable: derived.qgisAvailable,
    projectConnected: computeProjectConnected(instances, nextActiveInstanceId),
    instanceCount: derived.instanceCount,
    runningCount: derived.runningCount,
    stale,
  }
}

export const useInstanceStore = create<InstanceState>((set, get) => ({
  instances: [],
  activeInstanceId: null,
  qgisAvailable: false,
  projectConnected: false,
  instanceCount: 0,
  runningCount: 0,
  stale: false,
  loading: false,

  fetchInstances: async () => {
    set({ loading: true })
    try { const snapshot = await useRuntimeSnapshotStore.getState().fetchSnapshot()
    get().syncFromRuntimeSnapshot(snapshot) } catch (error) { void error; set({ loading: false }) }
  },

  syncFromRuntimeSnapshot: (snapshot) => {
    const instances = Array.isArray(snapshot.instances.items)
      ? snapshot.instances.items
      : []

    set({
      ...deriveInstanceProjection(
        instances,
        snapshot.instances.active_id,
        get().activeInstanceId,
        Boolean(snapshot.stale),
        false,
      ),
      loading: false,
    })
  },

  setActiveInstance: (activeInstanceId) => {
    const { instances } = get()
    const nextActiveInstanceId = activeInstanceId && instances.some((inst) => inst.id === activeInstanceId)
      ? activeInstanceId
      : null
    set({
      activeInstanceId: nextActiveInstanceId,
      projectConnected: computeProjectConnected(instances, nextActiveInstanceId),
    })
  },

  handleInstanceEvent: (event: InstanceEvent) => {
    const { instances, activeInstanceId, stale } = get()
    switch (event.type) {
      case 'added': {
        const exists = instances.some((item) => item.id === event.instance.id)
        if (exists) {
          return
        }
        const nextInstances = [...instances, event.instance]
        set(deriveInstanceProjection(nextInstances, undefined, activeInstanceId, stale))
        return
      }
      case 'removed': {
        const filtered = instances.filter((item) => item.id !== event.instance.id)
        set(
          deriveInstanceProjection(
            filtered,
            undefined,
            activeInstanceId === event.instance.id ? null : activeInstanceId,
            stale,
          ),
        )
        return
      }
      case 'changed': {
        const exists = instances.some((item) => item.id === event.instance.id)
        const updated = exists
          ? instances.map((item) => (
            item.id === event.instance.id ? event.instance : item
          ))
          : [...instances, event.instance]
        set(deriveInstanceProjection(updated, undefined, activeInstanceId, stale))
      }
    }
  },
}))

useRuntimeSnapshotStore.subscribe((state, prevState) => {
  if (state.snapshot === prevState.snapshot) {
    return
  }
  useInstanceStore.getState().syncFromRuntimeSnapshot(state.snapshot)
})
