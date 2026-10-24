import { expect, it } from 'vitest'
import { insertResourceReference, rewriteResourceType } from './editor'
const resourceTypes = [
  { type: 'data_list', public: false, label: '数据清单', words: ['数据清单'], group: 'data', folder: false, entry_extensions: ['.csv'] },
  { type: 'style', public: true, label: '样式', words: ['样式'], group: 'styles', folder: false, entry_extensions: ['.qml'] },
  { type: 'style_set', public: true, label: '样式集', words: ['样式集'], group: 'styles', folder: true, entry_extensions: [] },
]
it('inserts public resource declarations in their section, before later steps', () => {
  const source = '## 使用的资源\n\n## 步骤\n### 读取\n- 做法：读取'
  const result = insertResourceReference(source, {
    label: '底图',
    type: 'style_set',
  }, resourceTypes)
  expect(result.indexOf('样式集「底图」：公共资源库')).toBeLessThan(
    result.indexOf('## 步骤'),
  )
  expect(result).toContain('### 读取\n- 做法：读取')
})


it.each([['style', '样式'], ['style_set', '样式集']])('keeps %s reference type distinct', (type, label) => {
  const result = insertResourceReference('## 使用的资源\n', { label: '道路', type }, resourceTypes)
  expect(result).toContain(`${label}「道路」：公共资源库`)
})

it('uses the server-provided type word and refuses to invent an unknown type', () => {
  expect(insertResourceReference('', { label: '道路', type: 'style' }, [
    { type: 'style', public: true, label: '样式', words: ['自定义样式'], group: 'styles', folder: false, entry_extensions: ['.qml'] },
  ])).toContain('自定义样式「道路」：公共资源库')
  expect(() => insertResourceReference('', { label: '说明', type: 'data_list' }, resourceTypes)).toThrow()
  expect(() => insertResourceReference('', { label: '说明', type: 'unknown' }, resourceTypes)).toThrow()

})

it('rewrites only the matching resource declaration and leaves fenced examples and steps intact', () => {
  const source = '## 使用的资源\n```md\n- 样式集「道路」：公共资源库\n```\n- 样式集「道路」：公共资源库\n## 步骤\n- 样式集「道路」：公共资源库'
  expect(rewriteResourceType(source, { label: '道路', location: '公共资源库' }, 'style', resourceTypes)).toBe('## 使用的资源\n```md\n- 样式集「道路」：公共资源库\n```\n- 样式「道路」：公共资源库\n## 步骤\n- 样式集「道路」：公共资源库')
  expect(() => rewriteResourceType(source, { label: '其他', location: '公共资源库' }, 'style', resourceTypes)).toThrow('资源声明已变化')
})
