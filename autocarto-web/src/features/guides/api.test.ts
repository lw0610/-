import { beforeEach, expect, it, vi } from 'vitest'
import { guideApi, resourceApi } from './api'
import { normalizeToolCatalogResponse } from '@/lib/api'
const api = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  postEnvelope: vi.fn(),
  put: vi.fn(),
  patch: vi.fn(),
  delete: vi.fn(),
  getToolCatalog: vi.fn(),
}))
vi.mock('@/lib/api', async original => ({ ...await original<typeof import('@/lib/api')>(), default: api }))
beforeEach(() => {
  vi.clearAllMocks()
  api.getToolCatalog.mockImplementation(async () => normalizeToolCatalogResponse(await api.get('/api/tools/catalog?scope=global')))
})
it('uses guide contracts with no scheme or compiler request', async () => {
  await guideApi.get('指南')
  expect(api.get).toHaveBeenCalledWith('/api/guides/%E6%8C%87%E5%8D%97')
  await guideApi.save('demo', 'new guide')
  expect(api.put).toHaveBeenCalledWith('/api/guides/demo', {guide_md:'new guide'})
  await guideApi.setEnabled('demo', false)
  expect(api.put).toHaveBeenCalledWith('/api/guides/demo/enabled', {enabled:false})
  await guideApi.import('/tmp/legacy.zip', false)
  expect(api.post).toHaveBeenCalledWith('/api/guides/import', {source_path:'/tmp/legacy.zip',overwrite:false,new_id:undefined})
})
it('addresses management and references by stable ID with independent copies and explicit delete options', async () => {
  await resourceApi.types()
  expect(api.get).toHaveBeenCalledWith('/api/resources/types')
  await resourceApi.get('layout.u-a')
  expect(api.get).toHaveBeenCalledWith('/api/resources/by-id/layout.u-a')
  await resourceApi.copy('layout.four-corner', false, '我的版面')
  expect(api.post).toHaveBeenCalledWith('/api/resources/layout.four-corner/copy', { override: false, name: '我的版面' })
  await resourceApi.replace('layout.u-a', '/tmp/new.qpt')
  expect(api.put).toHaveBeenCalledWith('/api/resources/layout.u-a/content', { source_path: '/tmp/new.qpt' })
  await resourceApi.references('layout.u-a')
  expect(api.get).toHaveBeenCalledWith('/api/resources/layout.u-a/references')
  await resourceApi.remove('layout.u-a', true)
  expect(api.delete).toHaveBeenCalledWith('/api/resources/layout.u-a?force=true')
  await resourceApi.import('/tmp/new.qpt', '另一个版式')
  expect(api.post).toHaveBeenCalledWith('/api/resources/import', { source_path: '/tmp/new.qpt', name: '另一个版式' })
})
it('exports visual dependencies and edits metadata by identity', async () => {
 await guideApi.export('demo', '/tmp/demo.zip', false)
 expect(api.post).toHaveBeenCalledWith('/api/guides/demo/export', {target_path:'/tmp/demo.zip',include_data_files:false,include_library_deps:true})
 await resourceApi.update('style.a', {name:'河流线',description:'河流',tags:['水系']})
 expect(api.patch).toHaveBeenCalledWith('/api/resources/by-id/style.a',{name:'河流线',description:'河流',tags:['水系']})
})
it('maps the public capability id to its tool identity for preview names and searches', async () => {
  api.get.mockResolvedValue({
    items: [
      {
        id: 'layout_compose',
        display_name: '按版式一次排版',
        keywords: ['排版'],
      },
    ],
  })
  expect(await guideApi.capabilities()).toMatchObject([
    {
      name: 'layout_compose',
      display_name: '按版式一次排版',
      keywords: ['排版'],
    },
  ])
})
it('reads the production nested catalog envelope through the shared adapter', async () => {
  api.get.mockImplementation((path: string) => Promise.resolve(path.includes('skill-packages')
    ? { packages: [{ package_id: 'print-layout', display_name: '版面编排' }] }
    : { scope: 'global', catalog: { id: 'tool_catalog/global', items: [{ id: 'layout_compose', display_name: '按版式一次排版', keywords: ['排版'], package_id: 'print-layout' }] } }))
  expect(await guideApi.capabilities()).toMatchObject([{ name: 'layout_compose', display_name: '按版式一次排版', keywords: ['排版'], package_display_name: '版面编排' }])
  expect(api.getToolCatalog).toHaveBeenCalledWith('global')
})
it('enriches capability groups with the actual package display name', async () => {
  api.get.mockImplementation((path: string) =>
    Promise.resolve(
      path.includes('skill-packages')
        ? {
            packages: [
              { package_id: 'print-layout', display_name: '版面编排' },
            ],
          }
        : {
            items: [
              {
                id: 'layout_compose',
                package_id: 'print-layout',
                display_name: '按版式一次排版',
              },
            ],
          },
    ),
  )
  expect(await guideApi.capabilities()).toEqual([
    expect.objectContaining({
      name: 'layout_compose',
      package_display_name: '版面编排',
    }),
  ])
})
