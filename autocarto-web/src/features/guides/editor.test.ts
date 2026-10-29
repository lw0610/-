import { expect, it } from 'vitest'
import { flowBody, guideTemplate, insertResourceReference } from './editor'
it('inserts references as ordinary prose without creating a special resource section', () => {
  expect(insertResourceReference('任意内容', {label: '河流线', type: 'style'})).toBe('任意内容\n\n使用样式「河流线」。\n')
  expect(insertResourceReference('', {label: '四角对齐', type: 'layout'})).toContain('使用版面「四角对齐」。')
})
it('rejects retired resource types rather than inserting unusable references', () => {
  for (const type of ['style_set', 'data_list', 'symbols', 'doc']) expect(() => insertResourceReference('', {label: '旧资源', type})).toThrow('请选择样式或版面')
})
it('removes only front matter from the reading view', () => {
  expect(flowBody('---\r\n名称: 例子\r\n---\r\n# 正文\n\n---\n尾部')).toBe('# 正文\n\n---\n尾部')
  expect(flowBody('# 普通文档')).toBe('# 普通文档')
  expect(flowBody('---\n未关闭')).toBe('---\n未关闭')
})
it('starts from prose with no fixed data schema or role mapping', () => {
  expect(guideTemplate).not.toContain('| 数据 |'); expect(guideTemplate).not.toContain('## 使用的资源'); expect(guideTemplate).not.toContain('负责')
})
