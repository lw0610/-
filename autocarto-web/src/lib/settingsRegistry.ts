export type SettingsTaskKey =
  | 'system'
  | 'models-agents'
  | 'tools'
  | 'tool-center'
  | 'acquisition'
  | 'services'
  | 'diagnostics'
  | 'about'

export type RuntimeImpact =
  | 'instant'
  | 'runtime_rebuild'
  | 'desktop_restart'
  | 'read_only'

export type SettingMutability = 'editable' | 'read_only'

export interface SettingsSubSection {
  id: string
  label: string
  hint?: string
}

export interface SettingsTaskItem {
  key: SettingsTaskKey
  href: string
  label: string
  hint: string
  routes: string[]
  children?: SettingsSubSection[]
  hidden?: boolean
  priority?: number
}

export interface SettingDescriptor {
  id: string
  task: SettingsTaskKey
  section: string
  path: string
  anchor?: string
  label: string
  description?: string
  aliases?: string[]
  keywords?: string[]
  impact: RuntimeImpact
  mutability: SettingMutability
  priority: number
}

export interface SettingsSearchResult extends SettingDescriptor {
  href: string
  score: number
}

export interface SettingsWorkspaceMetadata {
  tasks: SettingsTaskItem[]
  descriptors: SettingDescriptor[]
}

export const emptySettingsWorkspaceMetadata: SettingsWorkspaceMetadata = {
  tasks: [],
  descriptors: [],
}

export const defaultSettingsWorkspaceMetadata: SettingsWorkspaceMetadata = {
  tasks: [
    {
      key: 'system',
      href: '/settings/system',
      label: '系统',
      hint: '日志、目录、QGIS、性能与会话策略',
      routes: ['/settings/system'],
      priority: 10,
    },
    {
      key: 'models-agents',
      href: '/settings/models-agents',
      label: '模型与智能体',
      hint: 'Provider、Image 2、API Key、方案管理与编排',
      routes: ['/settings/models-agents', '/settings/models', '/settings/agents'],
      priority: 30,
    },
    {
      key: 'tools',
      href: '/settings/tools',
      label: '工具包与能力',
      hint: '工具包目录、启停状态与工具工作区入口',
      routes: ['/settings/tools'],
      priority: 40,
    },
    {
      key: 'tool-center',
      href: '/tool-center',
      label: '工具中心',
      hint: '查看工具包、使用说明、参考资料与能力手册',
      routes: ['/tool-center'],
      hidden: true,
      priority: 45,
    },
    { key: 'acquisition', href: '/settings/acquisition', label: '数据获取', hint: '管理来源、数据记忆、获取策略与计划确认', routes: ['/settings/acquisition'], priority: 48 },
    {
      key: 'services',
      href: '/settings/services',
      label: '服务状态',
      hint: '查看运行态服务、连接状态与实例健康度',
      routes: ['/settings/services'],
      priority: 50,
    },
    {
      key: 'diagnostics',
      href: '/diagnostics',
      label: '诊断',
      hint: '查看运行时快照、关键路径、工具目录与健康状态',
      routes: ['/diagnostics'],
      hidden: true,
      priority: 55,
    },
    {
      key: 'about',
      href: '/settings/about',
      label: '关于',
      hint: '版本、链接与项目信息',
      routes: ['/settings/about'],
      priority: 70,
    }
  ],
  descriptors: [
    {
      id: 'system-log-dir',
      task: 'system',
      section: '系统运行',
      path: '/settings/system',
      anchor: 'system-runtime',
      label: '日志目录',
      description: '设置 AutoCarto 日志输出目录与轮转策略。',
      aliases: ['日志路径', 'log dir'],
      keywords: ['log', 'logging', 'rotate', 'backup'],
      impact: 'instant',
      mutability: 'editable',
      priority: 100,
    },
    {
      id: 'system-qgis-path',
      task: 'system',
      section: '系统运行',
      path: '/settings/system',
      anchor: 'system-runtime',
      label: 'QGIS 可执行路径',
      description: '指定桌面端 QGIS 可执行文件。',
      aliases: ['QGIS 路径', 'QGIS executable'],
      keywords: ['qgis', 'desktop', 'binary', 'path'],
      impact: 'desktop_restart',
      mutability: 'editable',
      priority: 95,
    },
    {
      id: 'system-performance',
      task: 'system',
      section: '系统运行',
      path: '/settings/system',
      anchor: 'system-runtime',
      label: '超时与并发',
      description: '配置工具超时、Agent 响应超时、并发数和循环限制。',
      aliases: ['performance', 'timeout'],
      keywords: ['timeout', 'performance', 'concurrency', 'loop'],
      impact: 'instant',
      mutability: 'editable',
      priority: 90,
    },
    {
      id: 'system-session-policy',
      task: 'system',
      section: '会话策略',
      path: '/settings/system',
      anchor: 'system-session',
      label: '会话策略',
      description: '会话命名、自动命名、快照、归档与清理。',
      aliases: ['会话命名', '归档策略'],
      keywords: ['session', 'snapshot', 'archive', 'cleanup'],
      impact: 'instant',
      mutability: 'editable',
      priority: 85,
    },
    {
      id: 'models-active-provider',
      task: 'models-agents',
      section: '模型',
      path: '/settings/models-agents',
      label: '激活模型',
      description: '选择当前运行时使用的 provider 配置。',
      aliases: ['当前模型', 'active model'],
      keywords: ['provider', 'active', 'model'],
      impact: 'runtime_rebuild',
      mutability: 'editable',
      priority: 100,
    },
    {
      id: 'models-quick-provider',
      task: 'models-agents',
      section: '模型',
      path: '/settings/models-agents',
      label: '快速模型',
      description: '用于系统内部任务与自动命名。',
      aliases: ['quick model', '自动命名模型'],
      keywords: ['quick model', 'auto title'],
      impact: 'runtime_rebuild',
      mutability: 'editable',
      priority: 95,
    },
    {
      id: 'models-api-key',
      task: 'models-agents',
      section: '模型',
      path: '/settings/models-agents',
      label: 'API Key',
      description: '配置 provider 鉴权密钥。',
      aliases: ['密钥', 'token'],
      keywords: ['api key', 'auth', 'secret'],
      impact: 'runtime_rebuild',
      mutability: 'editable',
      priority: 90,
    },
    {
      id: 'models-compression',
      task: 'models-agents',
      section: '模型',
      path: '/settings/models-agents',
      label: '上下文压缩',
      description: '控制压缩阈值、上下文上限与策略。',
      aliases: ['compress', '压缩策略'],
      keywords: ['context', 'compress', 'threshold'],
      impact: 'runtime_rebuild',
      mutability: 'editable',
      priority: 85,
    },
    {
      id: 'image2-config',
      task: 'models-agents',
      section: 'Image 2',
      path: '/settings/models-agents',
      label: 'Image 2 设置',
      description: '配置质量反馈工具使用的 Image 2 服务。',
      aliases: ['图像优化服务', '质量反馈图片'],
      keywords: ['image2', 'gpt-image-2', 'openai image', 'quality feedback'],
      impact: 'instant',
      mutability: 'editable',
      priority: 92,
    },
    {
      id: 'agents-configs',
      task: 'models-agents',
      section: '智能体',
      path: '/settings/models-agents',
      label: '智能体方案',
      description: '维护智能体方案、模型和工具能力。',
      aliases: ['配置方案', 'agent configs'],
      keywords: ['agent', 'config', 'plan', 'runtime'],
      impact: 'runtime_rebuild',
      mutability: 'editable',
      priority: 100,
    },
    {
      id: 'agents-runtime-policy',
      task: 'models-agents',
      section: '智能体',
      path: '/settings/models-agents',
      label: '智能体编排',
      description: '配置运行智能体的职责和共享资产。',
      aliases: ['运行策略', 'plan runtime'],
      keywords: ['runtime', 'planner', 'execution mode'],
      impact: 'runtime_rebuild',
      mutability: 'editable',
      priority: 90,
    },
    {
      id: 'tools-skill-packages',
      task: 'tools',
      section: '工具包与能力',
      path: '/settings/tools',
      label: '工具包目录',
      description: '查看工具包分组、来源、启停状态与详细信息。',
      aliases: ['工具中心', 'tool center'],
      keywords: ['tools', 'skills', 'package', 'catalog'],
      impact: 'runtime_rebuild',
      mutability: 'editable',
      priority: 88,
    },
    {
      id: 'services-runtime',
      task: 'services',
      section: '服务状态',
      path: '/settings/services',
      label: '运行态服务',
      description: '查看智能体系统、QGIS 桥接和连接状态。',
      aliases: ['服务状态', 'runtime services'],
      keywords: ['services', 'runtime', 'health', 'instance'],
      impact: 'read_only',
      mutability: 'read_only',
      priority: 80,
    },
    {
      id: 'about-app',
      task: 'about',
      section: '关于',
      path: '/settings/about',
      label: '应用信息',
      description: '查看应用版本、构建信息与项目链接。',
      aliases: ['关于', 'version'],
      keywords: ['about', 'version', 'build', 'links'],
      impact: 'read_only',
      mutability: 'read_only',
      priority: 60,
    },
  ],
}

export const settingsTaskItems = defaultSettingsWorkspaceMetadata.tasks
export const settingDescriptors = defaultSettingsWorkspaceMetadata.descriptors

function normalizeSubSection(raw: unknown): SettingsSubSection | null {
  if (!raw || typeof raw !== 'object') {
    return null
  }
  const record = raw as Record<string, unknown>
  const id = String(record.id || '').trim()
  const label = String(record.label || '').trim()
  if (!id || !label) {
    return null
  }
  return {
    id,
    label,
    ...(String(record.hint || '').trim()
      ? { hint: String(record.hint || '').trim() }
      : {}),
  }
}

function normalizeTaskItem(raw: unknown): SettingsTaskItem | null {
  if (!raw || typeof raw !== 'object') {
    return null
  }
  const record = raw as Record<string, unknown>
  const key = String(record.key || '').trim() as SettingsTaskKey
  const href = String(record.href || '').trim()
  const label = String(record.label || '').trim()
  if (!key || !href || !label) {
    return null
  }
  return {
    key,
    href,
    label,
    hint: String(record.hint || '').trim(),
    routes: Array.isArray(record.routes)
      ? record.routes.map((item) => String(item || '').trim()).filter(Boolean)
      : [href],
    children: Array.isArray(record.children)
      ? record.children
          .map(normalizeSubSection)
          .filter((item): item is SettingsSubSection => Boolean(item))
      : undefined,
    hidden: Boolean(record.hidden),
    priority: typeof record.priority === 'number' ? record.priority : undefined,
  }
}

function normalizeDescriptor(raw: unknown): SettingDescriptor | null {
  if (!raw || typeof raw !== 'object') {
    return null
  }
  const record = raw as Record<string, unknown>
  const id = String(record.id || '').trim()
  const task = String(record.task || '').trim() as SettingsTaskKey
  const path = String(record.path || '').trim()
  const label = String(record.label || '').trim()
  if (!id || !task || !path || !label) {
    return null
  }
  const impact = (String(record.impact || 'read_only').trim() || 'read_only') as RuntimeImpact
  const rawMutability = String(record.mutability || '').trim()

  return {
    id,
    task,
    section: String(record.section || '').trim() || '未分类',
    path,
    anchor: String(record.anchor || '').trim() || undefined,
    label,
    description: String(record.description || '').trim() || undefined,
    aliases: Array.isArray(record.aliases)
      ? record.aliases.map((item) => String(item || '').trim()).filter(Boolean)
      : [],
    keywords: Array.isArray(record.keywords)
      ? record.keywords.map((item) => String(item || '').trim()).filter(Boolean)
      : [],
    impact,
    mutability:
      rawMutability === 'read_only' || (!rawMutability && impact === 'read_only')
        ? 'read_only'
        : 'editable',
    priority: typeof record.priority === 'number' ? record.priority : 0,
  }
}

export function normalizeSettingsWorkspaceMetadata(raw: unknown): SettingsWorkspaceMetadata {
  if (!raw || typeof raw !== 'object') {
    return emptySettingsWorkspaceMetadata
  }
  const record = raw as Record<string, unknown>
  const tasks = Array.isArray(record.tasks)
    ? record.tasks.map(normalizeTaskItem).filter((item): item is SettingsTaskItem => Boolean(item))
    : []
  const descriptors = Array.isArray(record.descriptors)
    ? record.descriptors
        .map(normalizeDescriptor)
        .filter((item): item is SettingDescriptor => Boolean(item))
    : []

  return {
    tasks:
      tasks.length > 0
        ? tasks.sort((left, right) => (left.priority || 0) - (right.priority || 0))
        : [],
    descriptors:
      descriptors.length > 0
        ? descriptors.sort((left, right) => right.priority - left.priority)
        : [],
  }
}

export function getSettingsTask(
  pathname: string,
  taskItems: SettingsTaskItem[] = [],
): SettingsTaskItem | undefined {
  const normalized = String(pathname || '').trim().replace(/\.html$/, '')
  return (
    taskItems.find((item) =>
      item.routes.some((route) => normalized === route || normalized.startsWith(`${route}/`)),
    ) || taskItems[0]
  )
}

function descriptorText(descriptor: SettingDescriptor) {
  return [
    descriptor.label,
    descriptor.section,
    descriptor.description,
    ...(descriptor.aliases || []),
    ...(descriptor.keywords || []),
  ]
    .join(' ')
    .toLowerCase()
}

export function resolveSettingsHref(descriptor: Pick<SettingDescriptor, 'path' | 'anchor'>) {
  return descriptor.anchor ? `${descriptor.path}#${descriptor.anchor}` : descriptor.path
}

export function searchSettingDescriptors(
  query: string,
  descriptors: SettingDescriptor[] = [],
  limit = 8,
): SettingsSearchResult[] {
  const normalized = String(query || '').trim().toLowerCase()
  if (!normalized) {
    return []
  }
  const tokens = normalized.split(/\s+/).filter(Boolean)
  return descriptors
    .map((descriptor) => {
      const haystack = descriptorText(descriptor)
      if (!tokens.every((token) => haystack.includes(token))) {
        return null
      }

      const score = tokens.reduce((total, token) => {
        if (descriptor.label.toLowerCase().includes(token)) {
          return total + 8
        }
        if (descriptor.section.toLowerCase().includes(token)) {
          return total + 4
        }
        if ((descriptor.aliases || []).some((item) => item.toLowerCase().includes(token))) {
          return total + 3
        }
        if ((descriptor.keywords || []).some((item) => item.toLowerCase().includes(token))) {
          return total + 2
        }
        if (String(descriptor.description || '').toLowerCase().includes(token)) {
          return total + 1
        }
        return total
      }, descriptor.priority)

      return {
        ...descriptor,
        href: resolveSettingsHref(descriptor),
        score,
      }
    })
    .filter((item): item is SettingsSearchResult => Boolean(item))
    .sort((left, right) => right.score - left.score)
    .slice(0, limit)
}
