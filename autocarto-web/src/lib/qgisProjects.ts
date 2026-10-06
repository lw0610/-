import type { QGISRecentProject } from '@/types/config'

const INVALID_PROJECT_NAME_PATTERN = /[<>:"/\\|?*\u0000-\u001F]/

export function deriveProjectName(path: string) {
  const normalized = String(path || '').replace(/\\/g, '/')
  return normalized
    .split('/')
    .pop()
    ?.replace(/\.qgz$/i, '')
    .replace(/\.qgs$/i, '') || path
}

export function normalizeRecentProject(item: unknown): QGISRecentProject | null {
  if (!item || typeof item !== 'object') {
    return null
  }

  const record = item as Record<string, unknown>
  const path = String(record.path || '').trim()
  if (!path) {
    return null
  }

  const name = String(record.name || '').trim()
  const lastOpened = String(record.last_opened || '').trim()

  return {
    path,
    name: name || deriveProjectName(path),
    last_opened: lastOpened || new Date(0).toISOString(),
  }
}

export function sortRecentProjects(projects: QGISRecentProject[]) {
  return [...projects].sort((a, b) => b.last_opened.localeCompare(a.last_opened))
}

export function validateProjectName(projectName: string) {
  const trimmed = String(projectName || '').trim()
  if (!trimmed) {
    return '请输入工程名称'
  }
  if (INVALID_PROJECT_NAME_PATTERN.test(trimmed)) {
    return '工程名称不能包含 / \\ : * ? " < > | 等字符'
  }
  if (/[. ]$/.test(trimmed)) {
    return '工程名称不能以空格或点结尾'
  }
  return ''
}

export function buildProjectFilePath(
  directoryPath: string,
  projectName: string,
  extension = '.qgz',
) {
  const trimmedDirectory = String(directoryPath || '').trim().replace(/[\\/]+$/, '')
  const trimmedExtension = extension.startsWith('.') ? extension : `.${extension}`
  const separator = trimmedDirectory.includes('\\') ? '\\' : '/'
  return `${trimmedDirectory}${separator}${projectName}${trimmedExtension}`
}
