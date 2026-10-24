// @vitest-environment node
import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { createInterface } from 'node:readline'
import { existsSync } from 'node:fs'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import api from '@/lib/api'
import { useConnectionStore } from '@/stores/connectionStore'
import { flowApi, resourceApi, loadResourcePreview } from './api'

// Opt-in: build carto_agent/integration/resource-api-fixture and provide its path.
// Ordinary UI unit tests retain their fast, deterministic mocks.
const fixtureBinary = process.env.AUTOCARTO_RESOURCE_CONTRACT_BIN
describe.skipIf(!fixtureBinary)('resource UI clients against production Go HTTP handlers', () => {
  let server: ChildProcessWithoutNullStreams
  let root = ''
  let sources: Record<string, string>
  beforeAll(async () => {
    server = spawn(fixtureBinary!, [], { stdio: 'pipe' })
    let stderr = ''
    server.stderr.on('data', chunk => { stderr += String(chunk) })
    const metadata = await new Promise<{ url: string; root: string; sources: Record<string, string> }>((resolve, reject) => {
      const lines = createInterface({ input: server.stdout })
      server.once('error', reject)
      server.once('exit', code => reject(new Error(`fixture exited ${code}: ${stderr}`)))
      lines.on('line', line => {
        try {
          const value = JSON.parse(line)
          if (value.url) { lines.close(); resolve(value) }
        } catch { /* Production logging can precede the metadata line. */ }
      })
    })
    root = metadata.root
    sources = metadata.sources
    api.setBaseURL(metadata.url)
    useConnectionStore.setState({ backendUrl: metadata.url })
  }, 15_000)
  afterAll(async () => {
    if (!server || server.exitCode !== null) return
    const exited = new Promise<void>(resolve => server.once('exit', () => resolve()))
    server.stdin.end()
    await exited
    expect(existsSync(root)).toBe(false)
  })

  it('reads the canonical types, precise management identity, preview and binding status', async () => {
    expect((await resourceApi.types()).map(item => item.type).sort()).toEqual(['data_folder', 'data_list', 'doc', 'layout', 'route_style', 'style', 'style_set', 'symbols'])
    expect(await flowApi.bindingStatus()).toEqual({ orphaned_count: 0 })
    const original = await resourceApi.get('style.first')
    expect(original).toMatchObject({ id: 'style.first', owner: 'library_builtin', version: '1.0.0', name: '说明' })
    // QML has file metadata rather than an image/text preview endpoint.
    await expect(loadResourcePreview('style.first')).rejects.toThrow('资源预览不可用')
    const independent = await resourceApi.copy('style.first', false, '独立副本')
    expect(independent.id).not.toBe('style.first')
    expect(independent.overrides).toBeFalsy()
    await resourceApi.remove(independent.id!)
    const copy = await resourceApi.copy('style.first', true, '我的说明')
    expect(copy.overrides).toBe('style.first')
    expect(copy.owner).toBe('library_user')
    expect(await resourceApi.get('style.first')).toMatchObject({ id: 'style.first', owner: 'library_builtin', overridden_by: copy.id })
    await resourceApi.remove(copy.id!, true)
  })

  it('uses actual conflict responses and protects references before replace/delete', async () => {
    const imported = await resourceApi.import(sources.import, '契约文档')
    expect(imported.id).toMatch(/^style\./)
    await expect(resourceApi.import(sources.import, '契约文档')).rejects.toMatchObject({ status: 409 })
    const created = await flowApi.create(`---\n编号: imported-ref\n名称: 导入引用\n---\n## 使用的资源\n- 样式「契约文档」：公共资源库/${imported.id}\n## 步骤\n### 阅读\n- 负责：用户\n- 做法：查看「契约文档」。\n`, 'imported-ref')
    expect(created.meta.id).toBe('imported-ref')
    expect(await resourceApi.references(imported.id!)).toEqual(expect.arrayContaining([expect.objectContaining({ flow_id: 'imported-ref' })]))
    await expect(resourceApi.remove(imported.id!)).rejects.toMatchObject({ status: 409 })
    const replaced = await resourceApi.replace(imported.id!, sources.replacement)
    expect(replaced.id).toBe(imported.id)
    expect(replaced.digest).not.toBe(imported.digest)
    await resourceApi.remove(imported.id!, true)
    await expect(resourceApi.get(imported.id!)).rejects.toMatchObject({ status: 404 })
  })

  it('repairs ambiguity and lock drift with exact resource identities', async () => {
    const before = await flowApi.get('ambiguous', 'default')
    const diagnostic = before.diagnostics.find(item => item.code === 'FLOW_RESOURCE_AMBIGUOUS')
    expect(diagnostic?.fix?.candidates).toEqual(expect.arrayContaining([expect.objectContaining({ id: 'style.second', owner: 'library_user' })]))
    const chosen = await flowApi.repairResource('ambiguous', 'choose_resource', diagnostic!.fix!.resource_key!, 'style.second', 'default')
    expect(chosen.plan!.resources[0].id).toBe('style.second')
    await resourceApi.replace('style.second', sources.replacement)
    const drift = await flowApi.get('ambiguous', 'default')
    expect(drift.diagnostics.some(item => item.code === 'FLOW_RESOURCE_CHANGED')).toBe(true)
    const fixed = await flowApi.repairResource('ambiguous', 'update_lock', 'library:style.second', undefined, 'default')
    expect(fixed.meta.id).toBe('ambiguous')
    expect(fixed.diagnostics.some(item => item.code === 'FLOW_RESOURCE_CHANGED')).toBe(false)
  })

  it('upgrades both flow-owned and public legacy resources through their real endpoints', async () => {
    const before = await flowApi.get('legacy-flow', 'default')
    expect(before.plan!.resources[0].legacy).toBe(true)
    const upgraded = await flowApi.repairResource('legacy-flow', 'upgrade_resource', before.plan!.resources[0].key, undefined, 'default')
    expect(upgraded.plan!.resources[0].legacy).toBeFalsy()
    expect(upgraded.plan!.resources[0].id).toMatch(/^doc\./)
    const legacy = (await resourceApi.list()).find(item => item.name === '旧样式')!
    const publicUpgrade = await resourceApi.upgrade(legacy.id!)
    expect(publicUpgrade.id).toMatch(/^style\./)
    expect(publicUpgrade.legacy).toBeFalsy()
  })

  it('shows both bundled choices, installs one copy and selects the local resource explicitly', async () => {
    const bundled = await flowApi.get('bundled-install', 'default')
    expect(bundled.plan!.resources[0].owner).toBe('bundled')
    const installed = await flowApi.repairResource('bundled-install', 'install_resource', bundled.plan!.resources[0].key, undefined, 'default')
    expect(installed.plan!.resources[0].owner).toBe('library_user')
    const other = await flowApi.get('bundled-use', 'default')
    expect(other.plan!.resources[0].owner).toBe('bundled')
    expect(other.diagnostics.some(item => item.fix?.actions?.includes('use_library'))).toBe(true)
    const selected = await flowApi.repairResource('bundled-use', 'use_library', other.plan!.resources[0].key, undefined, 'default')
    expect(selected.plan!.resources[0]).toMatchObject({ owner: 'library_builtin', id: 'style.localbundle' })
  })

  it('validates data as input, remembers local choices and exports only portable public dependencies', async () => {
    await expect(resourceApi.import(sources.data_list, '公共数据')).rejects.toMatchObject({ status: 400 })
    const unfilled = await flowApi.get('data-flow', 'default')
    expect(unfilled.plan!.resources[0].base_dir_from_input).toBe('数据目录')
    expect(unfilled.diagnostics.some(item => item.code === 'FLOW_RESOURCE_UNBOUND')).toBe(false)
    expect((await flowApi.checkInputs('data-flow', 'default', { 数据目录: '/does-not-exist' })).some(item => item.level === 'error')).toBe(true)
    expect((await flowApi.checkInputs('data-flow', 'default', { 数据目录: sources.base_dir })).filter(item => item.level === 'error')).toEqual([])
    await flowApi.setInputMemory('data-flow', '数据目录', sources.base_dir)
    expect(await flowApi.inputMemory('data-flow')).toEqual({ 数据目录: sources.base_dir })
    await flowApi.clearInputMemory('data-flow', '数据目录')
    expect(await flowApi.inputMemory('data-flow')).toEqual({})
    const archive = await flowApi.export('ambiguous', sources.export, false)
    expect(archive.zip_path).toBe(sources.export)
    expect(existsSync(archive.zip_path)).toBe(true)
    const imported = await flowApi.import(archive.zip_path, false, 'portable-import')
    expect(imported.flow.meta.id).toBe('portable-import')
    const detail = await flowApi.get('portable-import', 'default')
    expect(detail.plan!.resources[0].id).toBe('style.second')
    expect(await flowApi.inputMemory('portable-import')).toEqual({})
  })
})
