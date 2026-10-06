import type {
  RuntimeServiceStatus,
  ToolCatalogContract,
  ToolCatalogItem,
} from '@/lib/api'
import type { QGISInstance } from '@/types/instance'

export interface DegradedDictionaryEntry {
  code?: string
  message?: string
  user_action?: string
  retryable?: boolean
  evidence_id?: string
}

type DictionarySeed = Required<Pick<DegradedDictionaryEntry, 'message' | 'user_action' | 'retryable'>>

const DICTIONARY: Record<string, DictionarySeed> = {
  provider_missing: {
    message: 'Provider 未配置，当前能力已降级。',
    user_action: '请先配置至少一个 Provider 后重试。',
    retryable: true,
  },
  unsupported_runtime: {
    message: '当前运行时不支持该能力。',
    user_action: '请切换到受支持的运行时或启动对应服务。',
    retryable: false,
  },
  abnormal_empty: {
    message: '工具目录异常为空，当前能力已降级。',
    user_action: '请刷新工具目录或检查运行态注册是否完成。',
    retryable: true,
  },
  catalog_degraded: {
    message: '工具目录处于降级状态。',
    user_action: '请刷新工具目录并检查运行态健康状态。',
    retryable: true,
  },
  instance_expired: {
    message: 'QGIS 实例已过期。',
    user_action: '等待实例重新注册或重新启动 QGIS。',
    retryable: true,
  },
  descriptor_blocked: {
    message: '运行时描述符尚未就绪。',
    user_action: '请等待描述符 ready 后重试。',
    retryable: true,
  },
}

function normalizeCode(raw: string | undefined) {
  const normalized = String(raw || '').trim().toLowerCase()
  switch (normalized) {
    case 'lease_expired':
      return 'instance_expired'
    case 'runtime_catalog_missing':
    case 'missing_dependency':
      return 'catalog_degraded'
    default:
      return normalized
  }
}

function buildEntry(code: string, overrides: Partial<DegradedDictionaryEntry> = {}): DegradedDictionaryEntry | null {
  const normalizedCode = normalizeCode(code)
  if (!normalizedCode) {
    return null
  }
  const seed = DICTIONARY[normalizedCode]
  return {
    code: normalizedCode,
    message: String(overrides.message || '').trim() || seed?.message,
    user_action: String(overrides.user_action || '').trim() || seed?.user_action,
    retryable: typeof overrides.retryable === 'boolean' ? overrides.retryable : seed?.retryable,
    evidence_id: String(overrides.evidence_id || '').trim() || undefined,
  }
}

export function getToolCatalogDegradedDictionary(catalog?: ToolCatalogContract | null): DegradedDictionaryEntry | null {
  if (!catalog) {
    return null
  }
  const code = normalizeCode(catalog.missing_dependency_reason || catalog.degradation_status)
  if (!code || code === 'none') {
    return null
  }
  return buildEntry(code)
}

export function getToolItemDegradedDictionary(item?: ToolCatalogItem | null): DegradedDictionaryEntry | null {
  if (!item || item.available !== false) {
    return null
  }
  const code = normalizeCode(item.missing_reason)
  return buildEntry(code, {
    evidence_id: item.evidence_id,
  })
}

export function getInstanceDegradedDictionary(instance?: (QGISInstance & { evidence_id?: string }) | null): DegradedDictionaryEntry | null {
  if (!instance || instance.status === 'running') {
    return null
  }
  const code = normalizeCode(instance.status_reason)
  if (!code) {
    return null
  }
  return buildEntry(code, {
    message: instance.status_detail,
    evidence_id: instance.evidence_id,
  })
}

export function getRuntimeServiceDegradedDictionary(service?: RuntimeServiceStatus | null): DegradedDictionaryEntry | null {
  const diagnostics = service?.diagnostics
  const message = String(service?.last_error || diagnostics?.startup_error || '').trim()
  if (!message) {
    return null
  }
  const suggestedActions = Array.isArray(diagnostics?.suggested_actions)
    ? diagnostics?.suggested_actions.filter((item) => String(item || '').trim())
    : []
  return {
    code: String(diagnostics?.startup_phase || '').trim() || undefined,
    message,
    user_action: suggestedActions[0],
    retryable: undefined,
    evidence_id: String(diagnostics?.evidence_id || '').trim() || undefined,
  }
}
