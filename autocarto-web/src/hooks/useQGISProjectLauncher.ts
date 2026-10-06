'use client'

import { useCallback, useState } from 'react'
import api from '@/lib/api'
import {
  buildProjectFilePath,
  deriveProjectName,
  normalizeRecentProject,
  sortRecentProjects,
} from '@/lib/qgisProjects'
import { isDesktopRuntime, launchQGIS, pickQGISProject } from '@/lib/tauri'
import wsClient, { getInstanceSelectRollbackTarget } from '@/lib/websocket'
import { useInstanceStore } from '@/stores/instanceStore'
import type { QGISRecentProject } from '@/types/config'
import type { QGISInstance } from '@/types/instance'

const DESKTOP_ONLY_ERROR = '当前运行环境不是 AutoCarto 桌面端，请在桌面应用中使用该功能。'
const INSTANCE_WAIT_TIMEOUT_MS = 60_000
const INSTANCE_POLL_INTERVAL_MS = 1000

interface WaitForNewInstanceOptions {
  expectedProjectPath?: string | null
}

export interface CreateProjectPayload {
  directoryPath: string
  projectName: string
}

export interface ProjectLaunchResult {
  reused: boolean
  instanceId: string
  projectPath: string
  projectName: string
  mode: 'opened' | 'created'
}

function ensureDesktopRuntime() {
  if (!isDesktopRuntime()) {
    throw new Error(DESKTOP_ONLY_ERROR)
  }
}

function normalizeProjectPath(projectPath?: string | null) {
  return String(projectPath || '').trim().replaceAll('\\', '/')
}

function isManagedRunningInstance(instance: QGISInstance) {
  return instance.status === 'running' && instance.source === 'reverse_ws'
}

function isReusableBlankInstance(instance: QGISInstance) {
  return isManagedRunningInstance(instance)
    && !normalizeProjectPath(instance.project_path)
}

export function useQGISProjectLauncher() {
  const fetchInstances = useInstanceStore((s) => s.fetchInstances)
  const setActiveInstance = useInstanceStore((s) => s.setActiveInstance)
  const [launching, setLaunching] = useState(false)
  const [loadingRecent, setLoadingRecent] = useState(false)
  const [recentProjects, setRecentProjects] = useState<QGISRecentProject[]>([])

  const readQGISConfig = useCallback(async () => {
    const config = await api.getConfig()
    const executablePath = String(config.qgis?.executable_path || '').trim()
    const parsedRecentProjects = Array.isArray(config.qgis?.recent_projects)
      ? config.qgis.recent_projects
        .map((item) => normalizeRecentProject(item))
        .filter((item): item is QGISRecentProject => item !== null)
      : []

    return {
      executablePath,
      recentProjects: sortRecentProjects(parsedRecentProjects),
    }
  }, [])

  const loadRecentProjects = useCallback(async () => {
    setLoadingRecent(true)
    try { const qgis = await readQGISConfig()
    setRecentProjects(qgis.recentProjects) } catch (error) { void error; setRecentProjects([]) } finally {
      setLoadingRecent(false)
    }
  }, [readQGISConfig])

  const readQGISExecutablePath = useCallback(async () => {
    try { const qgis = await readQGISConfig()
    return qgis.executablePath } catch (error) { void error; return '' }
  }, [readQGISConfig])

  const upsertRecentProject = useCallback(async (projectPath: string, projectName: string) => {
    const normalizedPath = String(projectPath || '').trim()
    if (!normalizedPath) {
      return
    }

    const qgis = await readQGISConfig()
    const next = [
      {
        path: normalizedPath,
        name: String(projectName || '').trim() || deriveProjectName(normalizedPath),
        last_opened: new Date().toISOString(),
      },
      ...qgis.recentProjects.filter((item) => item.path !== normalizedPath),
    ].slice(0, 10)

    await api.batchSetConfig([
      { path: 'qgis.recent_projects', value: next },
    ])

    setRecentProjects(next)
  }, [readQGISConfig])

  const triggerFetchAfterLaunch = useCallback(() => {
    void fetchInstances()
    window.setTimeout(() => {
      void fetchInstances()
    }, 2000)
  }, [fetchInstances])

  const waitForNewInstance = useCallback(async (
    existingInstanceIds: Set<string>,
    options?: WaitForNewInstanceOptions,
  ) => {
    const expectedProjectPath = normalizeProjectPath(options?.expectedProjectPath)
    let candidateInstanceId = ''

    const findMatch = (instances: QGISInstance[]) => {
      const newInstances = instances.filter((instance) => (
        !existingInstanceIds.has(instance.id)
        && isManagedRunningInstance(instance)
      ))

      if (newInstances.length === 0) {
        return null
      }

      if (expectedProjectPath) {
        const exactMatch = newInstances.find(
          (instance) => normalizeProjectPath(instance.project_path) === expectedProjectPath,
        )
        if (exactMatch) {
          candidateInstanceId = exactMatch.id
          return exactMatch
        }

        if (!candidateInstanceId) {
          candidateInstanceId = newInstances[0].id
        }
        return null
      }

      if (candidateInstanceId) {
        const candidate = newInstances.find((instance) => instance.id === candidateInstanceId)
        if (candidate) {
          return candidate
        }
      }

      candidateInstanceId = newInstances[0].id
      return newInstances[0]
    }

    const currentMatch = findMatch(useInstanceStore.getState().instances)
    if (currentMatch) {
      return currentMatch
    }

    return new Promise<QGISInstance>((resolve, reject) => {
      let settled = false
      let dispose = () => {}

      const timer = window.setTimeout(() => {
        dispose()
        reject(new Error('等待新 QGIS 实例注册超时，请确认插件已成功反向连接'))
      }, INSTANCE_WAIT_TIMEOUT_MS)

      const finish = (instance: QGISInstance) => {
        if (settled) {
          return
        }
        settled = true
        window.clearTimeout(timer)
        window.clearInterval(pollTimer)
        dispose()
        resolve(instance)
      }

      const pollSnapshot = async () => {
        if (settled) {
          return
        }
        try { const snapshot = await api.getInstanceSnapshot()
        const found = findMatch(snapshot.instances)
        if (found) {
          finish(found)
        } } catch (error) { void error;  }
      }

      const pollTimer = window.setInterval(() => {
        void pollSnapshot()
      }, INSTANCE_POLL_INTERVAL_MS)

      dispose = wsClient.onMessage((msg) => {
        if (
          msg.type !== 'instance_changed'
          || (msg.data?.event_type !== 'added' && msg.data?.event_type !== 'changed')
        ) {
          return
        }

        const found = findMatch(useInstanceStore.getState().instances)
        if (found) {
          finish(found)
        }
      })

      void pollSnapshot()
    })
  }, [])

  const selectInstance = useCallback(async (instanceId: string) => {
    const normalizedInstanceId = instanceId.trim()
    if (!normalizedInstanceId) {
      return
    }
    const previousActiveInstanceId = useInstanceStore.getState().activeInstanceId
    setActiveInstance(normalizedInstanceId)
    try {
      await wsClient.selectInstance(normalizedInstanceId)
    } catch (error) {
      setActiveInstance(getInstanceSelectRollbackTarget(error, previousActiveInstanceId || null))
      throw error
    }
  }, [setActiveInstance])

  const waitForInstanceProjectPath = useCallback(async (instanceId: string, projectPath: string) => {
    const normalizedProjectPath = normalizeProjectPath(projectPath)
    if (!instanceId || !normalizedProjectPath) {
      const current = useInstanceStore.getState().instances.find((instance) => instance.id === instanceId)
      if (!current) {
        throw new Error('目标 QGIS 实例不存在或尚未注册')
      }
      return current
    }

    const findMatch = (instances: QGISInstance[]) => instances.find((instance) => (
      instance.id === instanceId
      && isManagedRunningInstance(instance)
      && normalizeProjectPath(instance.project_path) === normalizedProjectPath
    ))

    const currentMatch = findMatch(useInstanceStore.getState().instances)
    if (currentMatch) {
      return currentMatch
    }

    return new Promise<QGISInstance>((resolve, reject) => {
      let settled = false
      let dispose = () => {}

      const timer = window.setTimeout(() => {
        dispose()
        reject(new Error('等待 QGIS 工程加载超时，请确认工程已成功打开'))
      }, INSTANCE_WAIT_TIMEOUT_MS)

      const finish = (instance: QGISInstance) => {
        if (settled) {
          return
        }
        settled = true
        window.clearTimeout(timer)
        window.clearInterval(pollTimer)
        dispose()
        resolve(instance)
      }

      const pollSnapshot = async () => {
        if (settled) {
          return
        }
        try { const snapshot = await api.getInstanceSnapshot()
        const found = findMatch(snapshot.instances)
        if (found) {
          finish(found)
        } } catch (error) { void error; return }
      }

      const pollTimer = window.setInterval(() => {
        void pollSnapshot()
      }, INSTANCE_POLL_INTERVAL_MS)

      dispose = wsClient.onMessage((msg) => {
        if (
          msg.type !== 'instance_changed'
          || (msg.data?.event_type !== 'added' && msg.data?.event_type !== 'changed')
        ) {
          return
        }

        const found = findMatch(useInstanceStore.getState().instances)
        if (found) {
          finish(found)
        }
      })

      void pollSnapshot()
    })
  }, [])

  const launchNewInstance = useCallback(async (
    existingInstanceIds: Set<string>,
    options?: WaitForNewInstanceOptions,
  ) => {
    const expectedProjectPath = normalizeProjectPath(options?.expectedProjectPath)
    const executablePath = await readQGISExecutablePath()
    await launchQGIS({
      executablePath: executablePath || null,
      projectPath: expectedProjectPath || null,
      forceNewInstance: true,
    })
    return waitForNewInstance(existingInstanceIds, {
      expectedProjectPath: expectedProjectPath || null,
    })
  }, [readQGISExecutablePath, waitForNewInstance])

  const launchProjectInNewInstance = useCallback(async (
    existingInstanceIds: Set<string>,
    projectPath: string,
  ) => {
    try {
      return await launchNewInstance(existingInstanceIds, {
        expectedProjectPath: projectPath,
      })
    } catch (error) {
      const snapshot = await api.getInstanceSnapshot()
      const fallbackInstance = snapshot.instances.find((instance) => (
        !existingInstanceIds.has(instance.id)
        && isManagedRunningInstance(instance)
      ))
      if (!fallbackInstance) {
        throw error
      }

      await api.openInstanceProject(fallbackInstance.id, projectPath)
      return waitForInstanceProjectPath(fallbackInstance.id, projectPath)
    }
  }, [launchNewInstance, waitForInstanceProjectPath])

  const openProjectViaPlugin = useCallback(async (projectPath: string, projectName: string) => {
    const snapshot = await api.getInstanceSnapshot()
    const runningInstances = snapshot.instances.filter((instance) => instance.status === 'running')
    const blankInstance = runningInstances.find(isReusableBlankInstance)

    if (blankInstance) {
      await api.openInstanceProject(blankInstance.id, projectPath)
      const readyInstance = await waitForInstanceProjectPath(blankInstance.id, projectPath)
      await selectInstance(readyInstance.id)
      await upsertRecentProject(projectPath, projectName)
      triggerFetchAfterLaunch()
      return {
        reused: true,
        instanceId: readyInstance.id,
        projectPath,
        projectName,
        mode: 'opened',
      } satisfies ProjectLaunchResult
    }

    const existingInstanceIds = new Set(snapshot.instances.map((instance) => instance.id))
    const newInstance = await launchProjectInNewInstance(existingInstanceIds, projectPath)

    await selectInstance(newInstance.id)
    await upsertRecentProject(projectPath, projectName)
    triggerFetchAfterLaunch()
    return {
      reused: false,
      instanceId: newInstance.id,
      projectPath,
      projectName,
      mode: 'opened',
    } satisfies ProjectLaunchResult
  }, [launchProjectInNewInstance, selectInstance, triggerFetchAfterLaunch, upsertRecentProject, waitForInstanceProjectPath])

  const openProjectFromPicker = useCallback(async () => {
    ensureDesktopRuntime()
    setLaunching(true)
    try {
      const projectPath = await pickQGISProject()
      if (!projectPath) {
        return null
      }
      return await openProjectViaPlugin(projectPath, deriveProjectName(projectPath))
    } finally {
      setLaunching(false)
    }
  }, [openProjectViaPlugin])

  const openRecentProject = useCallback(async (project: QGISRecentProject) => {
    ensureDesktopRuntime()
    setLaunching(true)
    try {
      return await openProjectViaPlugin(project.path, project.name)
    } finally {
      setLaunching(false)
    }
  }, [openProjectViaPlugin])

  const createProject = useCallback(async ({ directoryPath, projectName }: CreateProjectPayload) => {
    ensureDesktopRuntime()
    const trimmedDirectoryPath = String(directoryPath || '').trim()
    const trimmedProjectName = String(projectName || '').trim()
    const projectPath = buildProjectFilePath(trimmedDirectoryPath, trimmedProjectName, '.qgz')

    setLaunching(true)
    try {
      const snapshot = await api.getInstanceSnapshot()
      const existingInstanceIds = new Set(snapshot.instances.map((instance) => instance.id))
      const newInstance = await launchNewInstance(existingInstanceIds)

      try {
        await api.createInstanceProject(newInstance.id, projectPath)
      } catch (error) {
        const message = error instanceof Error ? error.message : '未知错误'
        throw new Error(`QGIS 已启动，但创建工程失败：${message}`)
      }

      const readyInstance = await waitForInstanceProjectPath(newInstance.id, projectPath)
      await selectInstance(readyInstance.id)
      await upsertRecentProject(projectPath, trimmedProjectName)
      triggerFetchAfterLaunch()
      return {
        reused: false,
        instanceId: readyInstance.id,
        projectPath,
        projectName: trimmedProjectName,
        mode: 'created',
      } satisfies ProjectLaunchResult
    } finally {
      setLaunching(false)
    }
  }, [launchNewInstance, selectInstance, triggerFetchAfterLaunch, upsertRecentProject, waitForInstanceProjectPath])

  return {
    launching,
    loadingRecent,
    recentProjects,
    loadRecentProjects,
    openProjectFromPicker,
    openRecentProject,
    createProject,
  }
}
