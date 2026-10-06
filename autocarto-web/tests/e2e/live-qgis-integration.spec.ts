import { expect, test } from '@playwright/test'
import { spawn } from 'node:child_process'
import { appendFileSync, writeFileSync } from 'node:fs'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import os from 'node:os'

const WEB_ROOT = '/mnt/210/HewittWong/AI_Dev/AutoCartoAI/autocarto-web'
const QGIS_PROJECT = '/mnt/210/HewittWong/AI_Dev/autocarto-test-qgis/t.qgz'
const QGIS_PROJECT_2 = '/mnt/210/HewittWong/AI_Dev/autocarto-test-qgis_2/t.qgz'
const QGIS_INI = join(os.homedir(), '.local/share/QGIS/QGIS3/profiles/default/QGIS/QGIS3.ini')
const TASK_30_MULTI_INSTANCE_SCREENSHOT = join(WEB_ROOT, '../.sisyphus/evidence/task-30-multi-instance.png')
const TASK_34_RESTART_SCREENSHOT = join(WEB_ROOT, '../.sisyphus/evidence/task-34-live-qgis-restart.png')
const TASK_34_RESTART_LIFECYCLE = join(WEB_ROOT, '.tmp/live-restart-lifecycle.log')
const PLAYWRIGHT_LIVE_BACKEND_CHILD_PID = join(WEB_ROOT, '.tmp/playwright-live-backend-child.pid')
const PLAYWRIGHT_LIVE_BACKEND_RESTART = join(WEB_ROOT, '.tmp/playwright-live-backend.restart')
const PLAYWRIGHT_LIVE_BACKEND_SUPERVISOR_PID = join(WEB_ROOT, '.tmp/playwright-live-backend-supervisor.pid')
const LIVE_RUNTIME_HOME = process.env.AUTOCARTO_HOME || join(os.homedir(), '.autocarto')

async function api<T>(request: any, method: 'GET' | 'POST', url: string, data?: unknown) {
  const response = method === 'POST'
    ? await request.post(url, { data })
    : await request.get(url)
  expect(response.ok()).toBeTruthy()
  const payload = await response.json()
  expect(payload.success).toBeTruthy()
  return payload.data as T
}

async function ensureAutocartoPluginEnabled() {
  const text = await readFile(QGIS_INI, 'utf8')
  if (text.includes('autocarto=true')) {
    return
  }
  await writeFile(QGIS_INI, text.replace('[PythonPlugins]\n', '[PythonPlugins]\nautocarto=true\n'))
}

function spawnQGIS(projectPath: string) {
  return spawn('qgis', [projectPath], {
    detached: true,
    stdio: 'ignore',
    env: {
      ...process.env,
      AUTOCARTO_HOME: LIVE_RUNTIME_HOME,
    },
  })
}

async function waitForLiveInstance(request: any, launchedAt: number, expectedProjectPath: string) {
  const startedAt = Date.now()
  const normalizedExpectedProjectPath = expectedProjectPath.trim()
  while (Date.now() - startedAt < 120_000) {
    const snapshot = await api<any>(request, 'GET', 'http://127.0.0.1:3000/api/runtime/snapshot')
    const instances = Array.isArray(snapshot.instances?.items) ? snapshot.instances.items : []
    const instance = instances.find((item: any) => {
      const started = Date.parse(String(item?.start_time || item?.created_at || ''))
      const projectPath = String(item?.project_path || '').trim()
      return item?.id
        && Number.isFinite(started)
        && started >= launchedAt - 5000
        && projectPath === normalizedExpectedProjectPath
    })
    if (instance?.id) {
      const pidValue = typeof instance?.pid === 'number' ? instance.pid : Number(instance?.pid)
      return {
        id: String(instance.id),
        count: Number(snapshot.instances?.instance_count || 0),
        pid: Number.isFinite(pidValue) ? pidValue : null,
      }
    }
    await new Promise((resolve) => setTimeout(resolve, 2000))
  }
  throw new Error('等待 QGIS 实例接入超时')
}

async function waitForRunningProjectExit(request: any, expectedProjectPath: string) {
  const startedAt = Date.now()
  const normalizedExpectedProjectPath = expectedProjectPath.trim()
  while (Date.now() - startedAt < 120_000) {
    const snapshot = await api<any>(request, 'GET', 'http://127.0.0.1:3000/api/runtime/snapshot')
    const instances = Array.isArray(snapshot.instances?.items) ? snapshot.instances.items : []
    const stillRunning = instances.some((item: any) => {
      const projectPath = String(item?.project_path || '').trim()
      const status = String(item?.status || '').trim().toLowerCase()
      return projectPath === normalizedExpectedProjectPath && status === 'running'
    })
    if (!stillRunning) {
      return snapshot
    }
    await new Promise((resolve) => setTimeout(resolve, 2000))
  }
  throw new Error('等待已退出的 QGIS 项目从运行实例中移除超时')
}

async function waitForProjectNotRunning(request: any, expectedProjectPath: string) {
  try {
    await waitForRunningProjectExit(request, expectedProjectPath)
  } catch {
  }
}

async function waitForProcessExit(pid: number, timeoutMs = 30_000) {
  const startedAt = Date.now()
  while (Date.now() - startedAt < timeoutMs) {
    try {
      process.kill(pid, 0)
    } catch {
      return
    }
    await new Promise((resolve) => setTimeout(resolve, 500))
  }
  throw new Error(`等待进程退出超时 pid=${pid}`)
}

async function terminateQGISProcess(pid: number | null | undefined) {
  if (!pid) {
    return
  }
  try {
    process.kill(pid, 'SIGTERM')
  } catch {
    return
  }
  try {
    await waitForProcessExit(pid)
    return
  } catch {
  }
  try {
    process.kill(pid, 'SIGKILL')
  } catch {
    return
  }
  try {
    await waitForProcessExit(pid, 10_000)
  } catch {
  }
}

async function ensureNoRunningProjects(request: any, projectPaths: string[], timeoutMs = 120_000) {
  const targets = new Set(projectPaths.map((path) => path.trim()))
  const startedAt = Date.now()
  while (Date.now() - startedAt < timeoutMs) {
    const snapshot = await api<any>(request, 'GET', 'http://127.0.0.1:3000/api/runtime/snapshot')
    const runningInstances = Array.isArray(snapshot.instances?.items)
      ? snapshot.instances.items.filter((item: any) => {
          const projectPath = String(item?.project_path || '').trim()
          const status = String(item?.status || '').trim().toLowerCase()
          return targets.has(projectPath) && status === 'running'
        })
      : []
    if (runningInstances.length === 0) {
      return
    }
    for (const instance of runningInstances) {
      await terminateQGISProcess(typeof instance?.pid === 'number' ? instance.pid : Number(instance?.pid))
    }
    await new Promise((resolve) => setTimeout(resolve, 1000))
  }
  throw new Error(`等待测试目标项目清场超时: ${Array.from(targets).join(', ')}`)
}

async function bestEffortEnsureNoRunningProjects(request: any, projectPaths: string[], timeoutMs = 10_000) {
  try {
    await ensureNoRunningProjects(request, projectPaths, timeoutMs)
  } catch {
  }
}

async function runShell(command: string) {
  await new Promise<void>((resolve, reject) => {
    const child = spawn('bash', ['-lc', command], {
      cwd: WEB_ROOT,
      stdio: 'ignore',
    })
    child.on('error', reject)
    child.on('exit', (code) => {
      if (code === 0) {
        resolve()
        return
      }
      reject(new Error(`命令执行失败: ${command} (exit ${code ?? 'unknown'})`))
    })
  })
}

async function runShellCapture(command: string) {
  return await new Promise<string>((resolve, reject) => {
    const child = spawn('bash', ['-lc', command], {
      cwd: WEB_ROOT,
      stdio: ['ignore', 'pipe', 'ignore'],
    })
    const chunks: Buffer[] = []
    child.stdout.on('data', (chunk) => chunks.push(Buffer.from(chunk)))
    child.on('error', reject)
    child.on('exit', (code) => {
      if (code === 0) {
        resolve(Buffer.concat(chunks).toString('utf8'))
        return
      }
      reject(new Error(`命令执行失败: ${command} (exit ${code ?? 'unknown'})`))
    })
  })
}

async function ensureNoProjectProcesses(projectPaths: string[], timeoutMs = 30_000) {
  const targets = new Set(projectPaths.map((path) => path.trim()))
  const startedAt = Date.now()
  while (Date.now() - startedAt < timeoutMs) {
    const output = await runShellCapture('ps -eo pid=,args=')
    const matchingPids = output
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean)
      .map((line) => {
        const match = line.match(/^(\d+)\s+(.*)$/)
        if (!match) {
          return null
        }
        return { pid: Number(match[1]), args: match[2] }
      })
      .filter((entry): entry is { pid: number; args: string } => Boolean(entry))
      .filter((entry) => entry.pid !== process.pid)
      .filter((entry) => entry.args.includes('qgis'))
      .filter((entry) => Array.from(targets).some((path) => entry.args.includes(path)))

    if (matchingPids.length === 0) {
      return
    }

    for (const entry of matchingPids) {
      await terminateQGISProcess(entry.pid)
    }
    await new Promise((resolve) => setTimeout(resolve, 1000))
  }
  throw new Error(`等待测试目标项目进程清场超时: ${Array.from(targets).join(', ')}`)
}

async function bestEffortEnsureNoProjectProcesses(projectPaths: string[], timeoutMs = 10_000) {
  try {
    await ensureNoProjectProcesses(projectPaths, timeoutMs)
  } catch {
  }
}

async function waitForBackendDown(request: any) {
  const startedAt = Date.now()
  while (Date.now() - startedAt < 60_000) {
    try {
      const response = await request.get('http://127.0.0.1:3000/api/health')
      if (!response.ok()) {
        return
      }
    } catch {
      return
    }
    await new Promise((resolve) => setTimeout(resolve, 1000))
  }
  throw new Error('等待 backend 停止超时')
}

async function readChildPid() {
  try {
    const text = await readFile(PLAYWRIGHT_LIVE_BACKEND_CHILD_PID, 'utf8')
    const pid = Number.parseInt(text.trim(), 10)
    return Number.isFinite(pid) ? pid : null
  } catch {
    return null
  }
}

async function readSupervisorPid() {
  try {
    const text = await readFile(PLAYWRIGHT_LIVE_BACKEND_SUPERVISOR_PID, 'utf8')
    const pid = Number.parseInt(text.trim(), 10)
    return Number.isFinite(pid) ? pid : null
  } catch {
    return null
  }
}

function isProcessAlive(pid: number | null) {
  if (!pid) {
    return false
  }
  try {
    process.kill(pid, 0)
    return true
  } catch {
    return false
  }
}

async function waitForRestartConsumption() {
  const startedAt = Date.now()
  while (Date.now() - startedAt < 30_000) {
    try {
      await readFile(PLAYWRIGHT_LIVE_BACKEND_RESTART, 'utf8')
    } catch {
      const supervisorPid = await readSupervisorPid()
      const supervisorAlive = isProcessAlive(supervisorPid)
      if (supervisorAlive) {
        return
      }
      throw new Error(
        `restart 信号文件已消失，但 supervisor 不存活 (supervisorPid=${supervisorPid ?? 'null'}, supervisorAlive=${supervisorAlive})`,
      )
    }
    await new Promise((resolve) => setTimeout(resolve, 250))
  }
  const supervisorPid = await readSupervisorPid()
  const supervisorAlive = isProcessAlive(supervisorPid)
  throw new Error(
    `等待 restart 信号被 supervisor 消费超时 (supervisorPid=${supervisorPid ?? 'null'}, supervisorAlive=${supervisorAlive})`,
  )
}

async function waitForChildPidChange(previousPid: number | null) {
  const startedAt = Date.now()
  let lastSeenPid: number | null = null
  while (Date.now() - startedAt < 60_000) {
    const nextPid = await readChildPid()
    lastSeenPid = nextPid
    if (nextPid && nextPid !== previousPid) {
      return nextPid
    }
    await new Promise((resolve) => setTimeout(resolve, 500))
  }
  const supervisorPid = await readSupervisorPid()
  const supervisorAlive = isProcessAlive(supervisorPid)
  throw new Error(
    `等待 backend child pid 变化超时 (previousPid=${previousPid ?? 'null'}, lastSeenPid=${lastSeenPid ?? 'null'}, supervisorPid=${supervisorPid ?? 'null'}, supervisorAlive=${supervisorAlive})`,
  )
}

async function waitForBackendReady(request: any) {
  const startedAt = Date.now()
  while (Date.now() - startedAt < 120_000) {
    try {
      const health = await request.get('http://127.0.0.1:3000/api/health')
      if (!health.ok()) {
        throw new Error(`health status ${health.status()}`)
      }
      const snapshotResponse = await request.get('http://127.0.0.1:3000/api/runtime/snapshot')
      if (!snapshotResponse.ok()) {
        throw new Error(`snapshot status ${snapshotResponse.status()}`)
      }
      const payload = await snapshotResponse.json()
      if (payload?.success) {
        return payload.data
      }
    } catch {
    }
    await new Promise((resolve) => setTimeout(resolve, 2000))
  }
  throw new Error('等待 backend 重启就绪超时')
}

async function restartBackend(request: any) {
  const previousPid = await readChildPid()
  await runShell(`: > "${PLAYWRIGHT_LIVE_BACKEND_RESTART}"`)
  await waitForRestartConsumption()
  await waitForChildPidChange(previousPid)
  await waitForBackendReady(request)
}

async function mockDesktopRuntime(page: any) {
  const runtimePort = 3000
  await page.addInitScript((portArg: unknown) => {
    const port = typeof portArg === 'number' ? portArg : 3000
    ;(window as typeof window & {
      __TAURI_INTERNALS__?: { invoke: (command: string) => Promise<unknown> }
    }).__TAURI_INTERNALS__ = {
      invoke: async (command: string) => {
        if (command === 'get_backend_runtime') {
          return {
            port,
            backend_url: `http://127.0.0.1:${port}`,
            ws_url: `ws://127.0.0.1:${port}/ws`,
          }
        }
        throw new Error(`unsupported tauri command: ${command}`)
      },
    }
  }, runtimePort)
}

test('live qgis project loop works across runtime and UI', async ({ page, request }) => {
  test.setTimeout(180_000)
  await ensureAutocartoPluginEnabled()
  await mockDesktopRuntime(page)
  await waitForBackendReady(request)
  await ensureNoProjectProcesses([QGIS_PROJECT])
  await ensureNoRunningProjects(request, [QGIS_PROJECT])
  const launchedAt = Date.now()

  const child = spawnQGIS(QGIS_PROJECT)
  child.unref()

  try {
    const instanceInfo = await waitForLiveInstance(request, launchedAt, QGIS_PROJECT)
    const instanceId = (instanceInfo as { id: string }).id

    const snapshot = await api<any>(request, 'GET', 'http://127.0.0.1:3000/api/runtime/snapshot')
    const runtimeInstances = Array.isArray(snapshot.instances?.items) ? snapshot.instances.items : []
    const currentInstance = runtimeInstances.find((item: any) => item.id === instanceId)
    expect(String(currentInstance?.project_path || '').trim()).toBe(QGIS_PROJECT)
    expect(Number(snapshot.instances?.instance_count || 0)).toBeGreaterThanOrEqual(1)

    await page.goto('/instances.html', { waitUntil: 'domcontentloaded' })
    await expect(page.locator('body')).toContainText('QGIS 实例管理')
    await expect(page.locator('body')).toContainText('运行中')

    await page.goto('/diagnostics.html', { waitUntil: 'domcontentloaded' })
    await expect(page.locator('body')).toContainText('运行时诊断')
    await expect(page.locator('body')).toContainText('QGIS 环境')
    await expect(page.locator('body')).toContainText('可用')
  } finally {
    await terminateQGISProcess(child.pid)
    await bestEffortEnsureNoProjectProcesses([QGIS_PROJECT])
    await bestEffortEnsureNoRunningProjects(request, [QGIS_PROJECT])
  }
})

test('live qgis runtime recovers after one project exits and another project re-registers', async ({ page, request }) => {
  test.setTimeout(300_000)
  await ensureAutocartoPluginEnabled()
  await mockDesktopRuntime(page)
  await waitForBackendReady(request)
  await ensureNoProjectProcesses([QGIS_PROJECT, QGIS_PROJECT_2])
  await ensureNoRunningProjects(request, [QGIS_PROJECT, QGIS_PROJECT_2])

  const firstLaunchedAt = Date.now()
  const firstChild = spawnQGIS(QGIS_PROJECT)
  firstChild.unref()

  let secondChild: ReturnType<typeof spawn> | null = null

  try {
    const firstInstanceInfo = await waitForLiveInstance(request, firstLaunchedAt, QGIS_PROJECT)
    expect((firstInstanceInfo as { id: string }).id).toBeTruthy()

    try {
      process.kill(firstChild.pid!, 'SIGTERM')
    } catch {
    }

    const exitedSnapshot = await waitForRunningProjectExit(request, QGIS_PROJECT)
    const exitedRunningProjects = Array.isArray(exitedSnapshot.instances?.items)
      ? exitedSnapshot.instances.items
          .filter((item: any) => String(item?.status || '').trim().toLowerCase() === 'running')
          .map((item: any) => String(item?.project_path || '').trim())
      : []
    expect(exitedRunningProjects).not.toContain(QGIS_PROJECT)

    const secondLaunchedAt = Date.now()
    secondChild = spawnQGIS(QGIS_PROJECT_2)
    secondChild.unref()

    const secondInstanceInfo = await waitForLiveInstance(request, secondLaunchedAt, QGIS_PROJECT_2)
    expect((secondInstanceInfo as { id: string }).id).toBeTruthy()

    const recoveredSnapshot = await api<any>(request, 'GET', 'http://127.0.0.1:3000/api/runtime/snapshot')
    const runningProjects = Array.isArray(recoveredSnapshot.instances?.items)
      ? recoveredSnapshot.instances.items
          .filter((item: any) => String(item?.status || '').trim().toLowerCase() === 'running')
          .map((item: any) => String(item?.project_path || '').trim())
      : []
    expect(runningProjects).toContain(QGIS_PROJECT_2)
    expect(runningProjects).not.toContain(QGIS_PROJECT)

    await page.goto('/instances.html', { waitUntil: 'domcontentloaded' })
    await expect(page.locator('body')).toContainText('QGIS 实例管理')
    await expect(page.locator('body')).toContainText('运行中')

    await page.goto('/diagnostics.html', { waitUntil: 'domcontentloaded' })
    await expect(page.locator('body')).toContainText('运行时诊断')
    await expect(page.locator('body')).toContainText('QGIS 环境')
    await expect(page.locator('body')).toContainText('可用')
  } finally {
    await terminateQGISProcess(firstChild.pid)
    await bestEffortEnsureNoProjectProcesses([QGIS_PROJECT])
    await bestEffortEnsureNoRunningProjects(request, [QGIS_PROJECT])
    if (secondChild?.pid) {
      await terminateQGISProcess(secondChild.pid)
    }
    await bestEffortEnsureNoProjectProcesses([QGIS_PROJECT_2])
    await bestEffortEnsureNoRunningProjects(request, [QGIS_PROJECT_2])
  }
})

test('live qgis runtime recovers after backend restart and re-registers the same project', async ({ browser, page, request }, testInfo) => {
  test.setTimeout(300_000)
  await ensureAutocartoPluginEnabled()
  await mockDesktopRuntime(page)
  await waitForBackendReady(request)
  await ensureNoProjectProcesses([QGIS_PROJECT])
  await ensureNoRunningProjects(request, [QGIS_PROJECT])
  const browserContext = page.context()
  const lifecycleEvents: string[] = []
  const recordLifecycle = (event: string) => {
    const line = `${new Date().toISOString()} ${event}`
    lifecycleEvents.push(line)
    appendFileSync(TASK_34_RESTART_LIFECYCLE, `${line}\n`, 'utf8')
  }

  page.on('close', () => recordLifecycle('page.close'))
  browserContext.on('close', () => recordLifecycle('context.close'))
  browser.on('disconnected', () => recordLifecycle('browser.disconnected'))

  const launchedAt = Date.now()
  const qgisChild = spawnQGIS(QGIS_PROJECT)
  qgisChild.unref()
  qgisChild.on('exit', (code, signal) => {
    recordLifecycle(`qgis-child.exit code=${code ?? 'null'} signal=${signal ?? 'null'}`)
  })

  try {
    writeFileSync(TASK_34_RESTART_LIFECYCLE, '', 'utf8')
    const initialInstance = await waitForLiveInstance(request, launchedAt, QGIS_PROJECT)
    const qgisInstancePid = initialInstance.pid

    await page.goto('/diagnostics.html', { waitUntil: 'domcontentloaded' })
    await expect.poll(async () => await page.locator('body').innerText(), { timeout: 30_000 }).toContain('运行时诊断')
    await expect(page.locator('body')).toContainText('可用')

    recordLifecycle(`before restart pageClosed=${page.isClosed()}`)
    await restartBackend(request)
    recordLifecycle(`after restart pageClosed=${page.isClosed()}`)

    const activePage = page.isClosed() ? await browserContext.newPage() : page

    await activePage.goto('/diagnostics.html', { waitUntil: 'domcontentloaded' })
    await expect.poll(async () => await activePage.locator('body').innerText(), { timeout: 30_000 }).toMatch(
      /正在连接后端服务|请检查 carto-agent 是否正常运行|未连接|连接检查|运行时诊断/,
    )

    const recoveredSnapshot = await expect.poll(async () => {
      const snapshot = await api<any>(request, 'GET', 'http://127.0.0.1:3000/api/runtime/snapshot')
      const runningProjects = Array.isArray(snapshot.instances?.items)
        ? snapshot.instances.items
            .filter((item: any) => String(item?.status || '').trim().toLowerCase() === 'running')
            .map((item: any) => String(item?.project_path || '').trim())
        : []
      const sameProjectCount = runningProjects.filter((projectPath: string) => projectPath === QGIS_PROJECT).length
      const matchingInstances = Array.isArray(snapshot.instances?.items)
        ? snapshot.instances.items
            .filter((item: any) => String(item?.project_path || '').trim() === QGIS_PROJECT)
            .map((item: any) => ({
              id: String(item?.id || ''),
              status: String(item?.status || ''),
              pid: typeof item?.pid === 'number' ? item.pid : Number(item?.pid),
            }))
        : []
      const observedState = {
        pidAlive: isProcessAlive(qgisInstancePid),
        stale: Boolean(snapshot.stale),
        sameProjectCount,
        matchingInstances,
        runningProjects,
      }
      recordLifecycle(`poll ${JSON.stringify(observedState)}`)
      return observedState
    }, { timeout: 180_000 }).toEqual({
      pidAlive: true,
      stale: false,
      sameProjectCount: 1,
      matchingInstances: [
        {
          id: expect.stringContaining('qgis_'),
          status: 'running',
          pid: qgisInstancePid,
        },
      ],
      runningProjects: [QGIS_PROJECT],
    })

    recordLifecycle(`after recovered poll ${JSON.stringify(recoveredSnapshot)}`)

    recordLifecycle('before goto instances')
    await activePage.goto('/instances.html', { waitUntil: 'domcontentloaded' })
    recordLifecycle('after goto instances')
    await expect(activePage.locator('body')).toContainText('QGIS 实例管理')
    await expect(activePage.locator('body')).toContainText('运行中')
    await expect(activePage.locator('body')).toContainText('t.qgz')
    recordLifecycle('after instances assertions')

    recordLifecycle('before goto diagnostics final')
    await activePage.goto('/diagnostics.html', { waitUntil: 'domcontentloaded' })
    recordLifecycle('after goto diagnostics final')
    await expect(activePage.locator('body')).toContainText('运行时诊断')
    await expect(activePage.locator('body')).toContainText('可用')
    await activePage.getByLabel('服务连接状态').hover()
    await expect(activePage.locator('body')).toContainText('服务连接状态')
    await expect(activePage.locator('body')).toContainText(/已连接|运行中|正常/)
    recordLifecycle('after diagnostics assertions')

    await mkdir(join(WEB_ROOT, '../.sisyphus/evidence'), { recursive: true })
    await activePage.screenshot({ path: TASK_34_RESTART_SCREENSHOT, fullPage: true })
    recordLifecycle('after screenshot')
  } finally {
    recordLifecycle('finally: attach lifecycle')
    await testInfo.attach('live-restart-lifecycle', {
      body: Buffer.from(`${lifecycleEvents.join('\n')}\n`, 'utf8'),
      contentType: 'text/plain',
    })
    recordLifecycle('finally: terminate qgis child begin')
    await terminateQGISProcess(qgisChild.pid)
    recordLifecycle('finally: terminate qgis child complete')
    recordLifecycle('finally: ensure no project processes begin')
    await bestEffortEnsureNoProjectProcesses([QGIS_PROJECT])
    recordLifecycle('finally: ensure no project processes complete')
    recordLifecycle('finally: ensure no running projects begin')
    await bestEffortEnsureNoRunningProjects(request, [QGIS_PROJECT])
    recordLifecycle('finally: ensure no running projects complete')
  }
})

test('simultaneous multi-instance live qgis runtime', async ({ page, request }) => {
  test.setTimeout(300_000)
  await ensureAutocartoPluginEnabled()
  await mockDesktopRuntime(page)
  await waitForBackendReady(request)
  await ensureNoProjectProcesses([QGIS_PROJECT, QGIS_PROJECT_2])
  await ensureNoRunningProjects(request, [QGIS_PROJECT, QGIS_PROJECT_2])

  const firstLaunchedAt = Date.now()
  const firstChild = spawnQGIS(QGIS_PROJECT)
  firstChild.unref()

  let secondChild: ReturnType<typeof spawn> | null = null

  try {
    const firstInstanceInfo = await waitForLiveInstance(request, firstLaunchedAt, QGIS_PROJECT)
    expect((firstInstanceInfo as { id: string }).id).toBeTruthy()

    const secondLaunchedAt = Date.now()
    secondChild = spawnQGIS(QGIS_PROJECT_2)
    secondChild.unref()

    const secondInstanceInfo = await waitForLiveInstance(request, secondLaunchedAt, QGIS_PROJECT_2)
    expect((secondInstanceInfo as { id: string }).id).toBeTruthy()

    const snapshot = await api<any>(request, 'GET', 'http://127.0.0.1:3000/api/runtime/snapshot')
    const runningProjects = Array.isArray(snapshot.instances?.items)
      ? snapshot.instances.items
          .filter((item: any) => String(item?.status || '').trim().toLowerCase() === 'running')
          .map((item: any) => String(item?.project_path || '').trim())
      : []
    expect(runningProjects).toContain(QGIS_PROJECT)
    expect(runningProjects).toContain(QGIS_PROJECT_2)

    await page.goto('/instances.html', { waitUntil: 'domcontentloaded' })
    await expect.poll(async () => await page.locator('body').innerText(), { timeout: 30_000 }).toContain('QGIS 实例管理')
    await expect.poll(async () => await page.locator('body').innerText(), { timeout: 30_000 }).toContain('共 2 个实例')
    await expect.poll(async () => await page.locator('body').innerText(), { timeout: 30_000 }).toContain('运行中 2')
    await mkdir(join(WEB_ROOT, '../.sisyphus/evidence'), { recursive: true })
    await page.screenshot({ path: TASK_30_MULTI_INSTANCE_SCREENSHOT, fullPage: true })

    // kill first instance
    try {
      process.kill(firstChild.pid!, 'SIGTERM')
    } catch {}

    const exitedSnapshot = await waitForRunningProjectExit(request, QGIS_PROJECT)
    const exitedRunningProjects = Array.isArray(exitedSnapshot.instances?.items)
      ? exitedSnapshot.instances.items
          .filter((item: any) => String(item?.status || '').trim().toLowerCase() === 'running')
          .map((item: any) => String(item?.project_path || '').trim())
      : []
    expect(exitedRunningProjects).not.toContain(QGIS_PROJECT)
    expect(exitedRunningProjects).toContain(QGIS_PROJECT_2)

    await page.goto('/instances.html', { waitUntil: 'domcontentloaded' })
    await expect.poll(async () => await page.locator('body').innerText(), { timeout: 30_000 }).toContain('共 1 个实例')
    await expect.poll(async () => await page.locator('body').innerText(), { timeout: 30_000 }).toContain('运行中 1')

  } finally {
    await terminateQGISProcess(firstChild.pid)
    await bestEffortEnsureNoProjectProcesses([QGIS_PROJECT])
    await bestEffortEnsureNoRunningProjects(request, [QGIS_PROJECT])
    if (secondChild?.pid) {
      await terminateQGISProcess(secondChild.pid)
    }
    await bestEffortEnsureNoProjectProcesses([QGIS_PROJECT_2])
    await bestEffortEnsureNoRunningProjects(request, [QGIS_PROJECT_2])
  }
})
