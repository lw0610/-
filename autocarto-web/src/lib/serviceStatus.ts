import type { RuntimeServiceStatus, RuntimeServicesSnapshot } from '@/lib/api'

const MODE_LABELS: Record<string, string> = {
  build: '执行模式',
  plan: '规划模式',
}

export type StatusTone = 'success' | 'warning' | 'danger' | 'neutral'

export interface StatusConfig {
  label: string
  tone: StatusTone
  dotClassName: string
  badgeClassName: string
  priority: number
}

export interface ServiceStatusItem {
  id: string
  label: string
  kind: 'service' | 'connection'
  status: string
  statusLabel: string
  tone: StatusTone
  dotClassName: string
  badgeClassName: string
  meta: string
  lastError: string
}

interface ConnectionStatusOptions {
  connectedLabel?: string
  disconnectedLabel?: string
  disconnectedTone?: StatusTone
}

function createStatusConfig(label: string, tone: StatusTone): StatusConfig {
  if (tone === 'success') {
    return {
      label,
      tone,
      dotClassName: 'bg-green-500',
      badgeClassName: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300',
      priority: 0,
    }
  }
  if (tone === 'warning') {
    return {
      label,
      tone,
      dotClassName: 'bg-amber-500',
      badgeClassName: 'bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300',
      priority: 2,
    }
  }
  if (tone === 'danger') {
    return {
      label,
      tone,
      dotClassName: 'bg-red-500',
      badgeClassName: 'bg-red-100 text-red-700 dark:bg-red-950 dark:text-red-300',
      priority: 3,
    }
  }
  return {
    label,
    tone,
    dotClassName: 'bg-zinc-400',
    badgeClassName: 'bg-zinc-100 text-zinc-600 dark:bg-zinc-800 dark:text-zinc-300',
    priority: 1,
  }
}

export function getRuntimeServiceStatusConfig(status: string): StatusConfig {
  const value = String(status || '').trim().toLowerCase()
  if (value === 'running') {
    return createStatusConfig('运行中', 'success')
  }
  if (value === 'ok') {
    return createStatusConfig('正常', 'success')
  }
  if (value === 'starting') {
    return createStatusConfig('启动中', 'warning')
  }
  if (value === 'restarting') {
    return createStatusConfig('重启中', 'warning')
  }
  if (value === 'syncing') {
    return createStatusConfig('同步中', 'warning')
  }
  if (value === 'restricted') {
    return createStatusConfig('受限', 'warning')
  }
  if (value === 'disconnected') {
    return createStatusConfig('已断开', 'warning')
  }
  if (value === 'failed') {
    return createStatusConfig('异常', 'danger')
  }
  if (value === 'disabled') {
    return createStatusConfig('已禁用', 'neutral')
  }
  return createStatusConfig(status || '未知', 'neutral')
}

export function getConnectionStatusConfig(
  connected: boolean,
  options: ConnectionStatusOptions = {},
): StatusConfig {
  if (connected) {
    return createStatusConfig(options.connectedLabel || '已连接', 'success')
  }
  return createStatusConfig(
    options.disconnectedLabel || '未连接',
    options.disconnectedTone || 'danger',
  )
}

export function buildRuntimeServiceMeta(service?: RuntimeServiceStatus): string[] {
  if (!service) {
    return []
  }

  const meta: string[] = []
  const endpoint = formatRuntimeServiceEndpoint(service)
  if (endpoint) {
    meta.push(endpoint)
  }
  if (typeof service.instance_count === 'number') {
    meta.push(`实例 ${service.instance_count}`)
  }
  if (typeof service.running_count === 'number') {
    meta.push(`运行 ${service.running_count}`)
  }
  if (typeof service.provider_count === 'number' && service.provider_count > 0) {
    meta.push(`Provider ${service.provider_count}`)
  }
  return meta
}

export function formatRuntimeServiceEndpoint(service?: RuntimeServiceStatus) {
  if (!service) {
    return ''
  }

  const rawHost = String(service.host || '').trim()
  const normalizedHost = rawHost && rawHost !== '0.0.0.0' && rawHost !== '::'
    ? rawHost
    : 'localhost'

  if (typeof service.port === 'number' && service.port > 0) {
    return `${normalizedHost}:${service.port}`
  }
  return rawHost
}

function buildServiceStatusItem(id: string, label: string, service?: RuntimeServiceStatus): ServiceStatusItem {
  const config = getRuntimeServiceStatusConfig(service?.status || 'unknown')
  return {
    id,
    label,
    kind: 'service',
    status: String(service?.status || 'unknown'),
    statusLabel: config.label,
    tone: config.tone,
    dotClassName: config.dotClassName,
    badgeClassName: config.badgeClassName,
    meta: buildRuntimeServiceMeta(service).join(' · '),
    lastError: String(service?.last_error || '').trim(),
  }
}

function buildModeReadinessItems(snapshot: RuntimeServicesSnapshot): ServiceStatusItem[] {
  const entries = Object.entries(snapshot.mode_readiness || {})
  return entries
    .filter(([, readiness]) => readiness && readiness.ready === false && readiness.enabled !== false)
    .map(([mode, readiness]) => {
      const config = getRuntimeServiceStatusConfig('restricted')
      const label = MODE_LABELS[mode] || `${mode} 模式`
      const issueMessage = snapshot.mode_issues?.[mode]?.message
      const detail = readiness.message || issueMessage || '当前处于受限模式'
      return {
        id: `mode_${mode}`,
        label,
        kind: 'service' as const,
        status: 'restricted',
        statusLabel: config.label,
        tone: config.tone,
        dotClassName: config.dotClassName,
        badgeClassName: config.badgeClassName,
        meta: detail,
        lastError: '',
      }
    })
}

function buildComputeServiceItem(
  snapshot: RuntimeServicesSnapshot,
  qgisBridgeSnapshot: RuntimeServiceStatus,
): ServiceStatusItem | null {
  const computeStatus = snapshot.services.cli || snapshot.services.compute
  if (!computeStatus) {
    return null
  }

  const config = getRuntimeServiceStatusConfig(computeStatus.status)
  const meta = buildRuntimeServiceMeta(computeStatus)
  const diagnostics = computeStatus.diagnostics
  let tone: StatusTone = config.tone
  let lastError = String(computeStatus.last_error || '').trim()

  if (diagnostics?.startup_phase) {
    meta.push(`阶段 ${diagnostics.startup_phase}`)
  }

  if (diagnostics?.service_ready && diagnostics.qgis_ready === false) {
    tone = 'warning'
    meta.push('等待 QGIS 就绪')
    if (!lastError) {
      lastError = diagnostics.startup_error || '计算服务已就绪，但 QGIS 环境仍未就绪'
    }
  } else if (!diagnostics && computeStatus.ready && qgisBridgeSnapshot.status !== 'running' && qgisBridgeSnapshot.status !== 'ok') {
    tone = 'warning'
    meta.push('等待 QGIS 就绪')
    if (!lastError) {
      lastError = '计算服务已就绪，但 QGIS 环境仍未就绪'
    }
  }

  return {
    id: 'cli',
    label: '计算服务',
    kind: 'service',
    status: String(computeStatus.status || 'unknown'),
    statusLabel: config.label,
    tone,
    dotClassName: tone === 'warning' ? 'bg-amber-500' : config.dotClassName,
    badgeClassName:
      tone === 'warning'
        ? 'bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300'
        : config.badgeClassName,
    meta: meta.join(' · '),
    lastError,
  }
}

function buildConnectionStatusItem(
  id: string,
  label: string,
  connected: boolean,
  options: ConnectionStatusOptions = {},
  meta = '',
): ServiceStatusItem {
  const config = getConnectionStatusConfig(connected, options)
  return {
    id,
    label,
    kind: 'connection',
    status: connected ? 'connected' : 'disconnected',
    statusLabel: config.label,
    tone: config.tone,
    dotClassName: config.dotClassName,
    badgeClassName: config.badgeClassName,
    meta,
    lastError: '',
  }
}

function buildQGISBridgeFallback(snapshot: RuntimeServicesSnapshot) {
  if (snapshot.services.qgis) {
    return snapshot.services.qgis
  }
  if (snapshot.services.qgis_bridge) {
    return snapshot.services.qgis_bridge
  }
  const instanceCount = typeof snapshot.instances.instance_count === 'number'
    ? snapshot.instances.instance_count
    : undefined
  const runningCount = typeof snapshot.instances.running_count === 'number'
    ? snapshot.instances.running_count
    : undefined

  return {
    status: runningCount && runningCount > 0 ? 'running' : 'unknown',
    instance_count: instanceCount,
    running_count: runningCount,
  } as RuntimeServiceStatus
}

export function buildTopBarServiceStatusItems(options: {
  snapshot: RuntimeServicesSnapshot
  websocketConnected: boolean
  projectConnected: boolean
  projectStatusText?: string
}) {
  const { snapshot, websocketConnected, projectConnected, projectStatusText = '' } = options
  const qgisBridge = buildQGISBridgeFallback(snapshot)
  const runtimeStale = Boolean(snapshot.stale)
  const qgisBridgeSnapshot = runtimeStale
    ? {
        ...qgisBridge,
        status: 'syncing',
      }
    : qgisBridge

  const items = [
    buildServiceStatusItem('agent', '智能体系统', snapshot.services.agent),
    ...buildModeReadinessItems(snapshot),
  ]

  const computeItem = buildComputeServiceItem(snapshot, qgisBridgeSnapshot)
  if (computeItem) {
    items.push(computeItem)
  }

	items.push(buildServiceStatusItem('qgis', 'QGIS 桥接', qgisBridgeSnapshot))

	items.push(
    buildConnectionStatusItem('backend_ws', '后端 WebSocket', websocketConnected),
    buildConnectionStatusItem(
      'project',
      '当前项目',
      runtimeStale ? false : projectConnected,
      runtimeStale
        ? { disconnectedLabel: '同步中', disconnectedTone: 'warning' }
        : { disconnectedTone: 'warning' },
      runtimeStale
        ? '等待运行时快照恢复'
        : projectConnected ? '' : projectStatusText,
    ),
  )

  return items
}

export function getAggregateStatus(items: ServiceStatusItem[]): StatusConfig {
  if (items.length === 0) {
    return createStatusConfig('状态待确认', 'neutral')
  }

  let highestPriority = 0
  for (const item of items) {
    const priority = createStatusConfig(item.statusLabel, item.tone).priority
    if (priority > highestPriority) {
      highestPriority = priority
    }
  }

  if (highestPriority >= 3) {
    return createStatusConfig('服务异常', 'danger')
  }
  if (highestPriority === 2) {
    return createStatusConfig('连接受限', 'warning')
  }
  const allHealthy = items.every((item) => item.tone === 'success')
  if (allHealthy) {
    return createStatusConfig('服务正常', 'success')
  }
  return createStatusConfig('状态待确认', 'neutral')
}
