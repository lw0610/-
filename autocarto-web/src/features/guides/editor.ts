import type { ResourceTypeDefinition } from './types'

export const guideTemplate = `---
名称: 新流程
说明: 描述这类制图任务的用途和适用请求
版本: 1.0.0
---

# 新流程

## 什么时候用
说明这份流程适合制作什么地图。

## 需要的图层与处理
写清需要的数据、范围、处理方式和标注要求。样式可从资源库插入。

## 做法
依据真实材料核验数据，然后处理图层并制作地图。

## 版面
说明纸张、主图、标题、图例和来源说明的要求。

## 成果
导出 PDF，交付成果路径和实际数据来源。

## 注意事项
不虚构数据，不修改用户原始文件。
`

export function resourceTypeLabel(types: ResourceTypeDefinition[], type: string): string {
  return type === 'layout' ? '版面' : type === 'style' ? '样式' : types.find(item => item.type === type)?.label || '资源'
}

export function insertResourceReference(markdown: string, resource: { label: string; type: string }): string {
  if (!['style', 'layout'].includes(resource.type)) throw new Error('请选择样式或版面')
  const name = resource.label.replace(/[\r\n「」]/g, '').trim()
  if (!name) throw new Error('资源名称不能为空')
  // Insert plain prose; no special section is needed or interpreted at runtime.
  return `${markdown.trimEnd()}\n\n使用${resource.type === 'layout' ? '版面' : '样式'}「${name}」。\n`
}

export function flowBody(markdown: string): string {
  return markdown.replace(/^\uFEFF?---\s*\r?\n[\s\S]*?\r?\n---(?:\r?\n|$)/, '').trim()
}
