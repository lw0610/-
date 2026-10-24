import type { ResourceTypeDefinition } from './types'

export const flowTemplate = `---
名称: 新流程
说明: 描述这个流程解决的问题
版本: 1.0.0
---

## 适用场景
说明何时使用这个流程。

## 需要准备
- 数据文件（必填，文件）：需要处理的数据文件。

## 使用的资源

## 步骤

### 1. 检查数据
- 负责：系统自动
- 做法：读取「数据文件」，检查字段、几何类型和坐标系。
- 完成标准：「检查结果」（文本）
- 失败时：询问用户

## 必须遵守
- 只依据数据和工具返回的实际结果。

## 交付
交付检查结果和发现的问题。
`
export const templateSections = {
  step: '\n### 新步骤\n- 负责：系统自动\n- 做法：说明这一步要完成的工作。\n- 完成标准：「产出名称」（文本）\n- 失败时：重试一次后询问用户\n',
  resource: '\n- 数据清单「资源名称」：resources/data/数据清单.csv\n',
  condition: '\n- 条件：提供了「输入名称」时执行\n',
}
export function resourceTypeLabel(types: ResourceTypeDefinition[], type: string): string {
  return types.find(item => item.type === type)?.label || '资源'
}
export function insertResourceReference(
  markdown: string,
  resource: { label: string; type: string },
  types: ResourceTypeDefinition[],
): string {
  const label = resource.label.replace(/[\r\n「」]/g, '')
  const definition = types.find(item => item.type === resource.type)
  const word = definition?.words[0]
  if (!word || !definition?.public) throw new Error('请选择模板与样式库中的版面、样式或样式集')
  const entry = `- ${word}「${label}」：公共资源库`
  const lines = markdown.split('\n')
  let start = -1,
    fence = ''
  for (let i = 0; i < lines.length; i++) {
    const match = lines[i].match(/^\s*(`{3,}|~{3,})/)
    if (match) {
      if (!fence) fence = match[1][0]
      else if (match[1][0] === fence) fence = ''
      continue
    }
    if (fence) continue
    if (/^##\s+(使用的资源|资源|引用资源)\s*$/.test(lines[i])) {
      start = i
      continue
    }
    if (start >= 0 && /^##\s+/.test(lines[i])) {
      lines.splice(i, 0, entry, '')
      return lines.join('\n')
    }
  }
  if (start >= 0) return `${markdown.trimEnd()}\n${entry}\n`
  return `${markdown.trimEnd()}\n\n## 使用的资源\n${entry}\n`
}

// Change only a declared resource's type word, never matching quoted examples or step prose.
export function rewriteResourceType(
  markdown: string,
  resource: { label: string; location: string },
  actualType: string,
  types: ResourceTypeDefinition[],
): string {
  const word = types.find(item => item.type === actualType)?.words[0]
  if (!word) throw new Error('资源类型尚未加载，请刷新后重试')
  const lines = markdown.split('\n')
  let inResources = false, fence = ''
  const matches: { index: number; prefix: string; suffix: string }[] = []
  for (let i = 0; i < lines.length; i++) {
    const marker = lines[i].match(/^\s*(`{3,}|~{3,})/)
    if (marker) {
      if (!fence) fence = marker[1][0]
      else if (marker[1][0] === fence) fence = ''
      continue
    }
    if (fence) continue
    if (/^##\s+/.test(lines[i])) {
      inResources = /^##\s+(使用的资源|资源|引用资源)\s*$/.test(lines[i])
      continue
    }
    if (!inResources) continue
    const declaration = lines[i].match(/^(\s*[-*+]\s*)[^「]+(「([^」]+)」\s*[：:]\s*(.*))$/)
    if (declaration && declaration[3].trim() === resource.label && declaration[4].trim() === resource.location.trim()) {
      matches.push({ index: i, prefix: declaration[1], suffix: declaration[2] })
    }
  }
  if (matches.length !== 1) throw new Error('资源声明已变化或不唯一，请保存并重新打开流程后修复')
  const match = matches[0]
  lines[match.index] = `${match.prefix}${word}${match.suffix}`
  return lines.join('\n')
}
