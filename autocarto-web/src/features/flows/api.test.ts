import { beforeEach, expect, it, vi } from 'vitest'
import { flowApi, resourceApi } from './api'
import { normalizeToolCatalogResponse } from '@/lib/api'
const api = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn(),
  getToolCatalog: vi.fn(),
}))
vi.mock('@/lib/api', async original => ({ ...await original<typeof import('@/lib/api')>(), default: api }))
beforeEach(() => {
  vi.clearAllMocks()
  api.getToolCatalog.mockImplementation(async () => normalizeToolCatalogResponse(await api.get('/api/tools/catalog?scope=global')))
})
it('encodes identifiers and sends the documented import, save and resource fields', async () => {
  await flowApi.get('流程', '方案 1')
  expect(api.get).toHaveBeenCalledWith(
    '/api/flows/%E6%B5%81%E7%A8%8B?scheme=%E6%96%B9%E6%A1%88%201',
  )
  await flowApi.import('/data/流程.zip', true, 'new-flow')
  expect(api.post).toHaveBeenCalledWith('/api/flows/import', {
    source_path: '/data/流程.zip',
    overwrite: true,
    new_id: 'new-flow',
  })
  await flowApi.save('demo', 'updated flow', 'default')
  expect(api.put).toHaveBeenCalledWith(
    '/api/flows/demo?scheme=default',
    { flow_md: 'updated flow' },
  )
  await flowApi.importResource('demo', 'source.csv', 'data')
  expect(api.post).toHaveBeenCalledWith('/api/flows/demo/resources/import', {
    source_path: 'source.csv',
    target_subdir: 'resources/data',
  })
  await resourceApi.bind('library:底图', 'data/base')
  expect(api.put).toHaveBeenCalledWith('/api/resources/bindings', {
    binding_key: 'library:底图',
    base_dir: 'data/base',
  })
})
it('addresses management and references by stable ID with explicit override and delete options', async () => {
  await resourceApi.types()
  expect(api.get).toHaveBeenCalledWith('/api/resources/types')
  await resourceApi.get('layout.u-a')
  expect(api.get).toHaveBeenCalledWith('/api/resources/by-id/layout.u-a')
  await resourceApi.copy('layout.four-corner', true, '我的版式')
  expect(api.post).toHaveBeenCalledWith('/api/resources/layout.four-corner/copy', { override: true, name: '我的版式' })
  await resourceApi.replace('layout.u-a', '/tmp/new.qpt')
  expect(api.put).toHaveBeenCalledWith('/api/resources/layout.u-a/content', { source_path: '/tmp/new.qpt' })
  await resourceApi.references('layout.u-a')
  expect(api.get).toHaveBeenCalledWith('/api/resources/layout.u-a/references')
  await resourceApi.remove('layout.u-a', true)
  expect(api.delete).toHaveBeenCalledWith('/api/resources/layout.u-a?force=true')
  await resourceApi.upgrade('legacy.layout.a')
  expect(api.post).toHaveBeenCalledWith('/api/resources/legacy.layout.a/upgrade', {})
  await resourceApi.import('/tmp/new.qpt', '另一个版式')
  expect(api.post).toHaveBeenCalledWith('/api/resources/import', { source_path: '/tmp/new.qpt', name: '另一个版式' })
})
it('exports dependencies by default and uses stable resource repair and binding fields', async () => {
  await flowApi.export('demo', '/tmp/demo.zip', false)
  expect(api.post).toHaveBeenCalledWith('/api/flows/demo/export', { target_path: '/tmp/demo.zip', include_data_files: false, include_library_deps: true })
  await flowApi.repairResource('demo', 'choose_resource', 'library:layout.old', 'layout.chosen', 'review')
  expect(api.post).toHaveBeenCalledWith('/api/flows/demo/resources/repair?scheme=review', { action: 'choose_resource', resource_key: 'library:layout.old', resource_id: 'layout.chosen' })
  await flowApi.bind('demo', 'path:data/base.csv', '/tmp/data')
  expect(api.put).toHaveBeenCalledWith('/api/flows/demo/bindings', { resource_key: 'path:data/base.csv', base_dir: '/tmp/data' })
  await flowApi.bindingStatus()
  expect(api.get).toHaveBeenCalledWith('/api/flows/bindings/status')
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
  expect(await flowApi.capabilities()).toMatchObject([
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
  expect(await flowApi.capabilities()).toMatchObject([{ name: 'layout_compose', display_name: '按版式一次排版', keywords: ['排版'], package_display_name: '版面编排' }])
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
  expect(await flowApi.capabilities()).toEqual([
    expect.objectContaining({
      name: 'layout_compose',
      package_display_name: '版面编排',
    }),
  ])
})
