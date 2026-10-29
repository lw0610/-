// @vitest-environment node
import { execFileSync, spawn, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { createInterface } from 'node:readline'
import { existsSync } from 'node:fs'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import api from '@/lib/api'
import { useConnectionStore } from '@/stores/connectionStore'
import { guideApi, resourceApi, loadResourcePreview } from './api'

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

  it('exposes only visual types, full free text, a stable identity and offline preview', async () => {
    expect((await resourceApi.types()).map(item => item.type).sort()).toEqual(['layout', 'style'])
    expect((await guideApi.get('example')).guide.body).toContain('这里没有固定表格')
    const original = await resourceApi.get('style.first')
    expect(original).toMatchObject({ id: 'style.first', owner: 'library_builtin', name: '内置线样式', geometry: 'line', preview_status: 'pending' })
    expect((await loadResourcePreview('style.first')).type).toContain('image/')
    const copy = await resourceApi.copy('style.first', false, '独立副本')
    expect(copy.id).not.toBe(original.id)
    expect(copy.owner).toBe('library_user')
    await resourceApi.remove(copy.id!)
    await expect(resourceApi.update('style.first', {name: '改内置', description: '', tags: []})).rejects.toMatchObject({status: 403})
  })

  it('protects referenced resources and preserves identity on replacement', async () => {
    const result = await resourceApi.import(sources.import, '契约线样式')
    const imported = 'items' in result ? result.items[0] : result
    expect(imported.id).toMatch(/^style\./)
    await expect(resourceApi.import(sources.import, '契约线样式')).rejects.toMatchObject({ status: 409 })
    await guideApi.create('---\n编号: imported-ref\n名称: 导入引用\n说明: 检验引用\n---\n按样式「契约线样式」绘图。\n', 'imported-ref')
    expect(await resourceApi.references(imported.id!)).toEqual(expect.arrayContaining([expect.objectContaining({ flow_id: 'imported-ref' })]))
    await expect(resourceApi.remove(imported.id!)).rejects.toMatchObject({ status: 409 })
    const replaced = await resourceApi.replace(imported.id!, sources.replacement)
    expect(replaced.id).toBe(imported.id)
    expect(replaced.digest).not.toBe(imported.digest)
    await resourceApi.remove(imported.id!, true)
    await expect(resourceApi.get(imported.id!)).rejects.toMatchObject({ status: 404 })
  })

  it('imports each folder entry independently and reports renamed conflicts', async () => {
    const result = await resourceApi.import(sources.batch)
    expect('items' in result).toBe(true)
    if (!('items' in result)) throw new Error('Expected a batch result')
    expect(result.items).toHaveLength(2)
    expect(new Set(result.items.map(item => item.name)).size).toBe(2)
    expect(result.warnings).toHaveLength(1)
    expect(result.items.every(item => item.geometry === 'line')).toBe(true)
  })

  it('edits metadata without changing identity and keeps locked references after rename', async () => {
    await guideApi.create('---\n编号: renamed-ref\n名称: 改名引用\n说明: 检验稳定编号\n---\n使用样式「用户线样式」。\n', 'renamed-ref')
    const updated = await resourceApi.update('style.second', { name: '重命名线样式', description: '河流和道路', tags: ['线', '交通'] })
    expect(updated).toMatchObject({id: 'style.second', name: '重命名线样式', description: '河流和道路', tags: ['线', '交通']})
    expect((await guideApi.get('renamed-ref')).resolved_resources).toEqual(expect.arrayContaining([expect.objectContaining({id: 'style.second'})]))
    await expect(resourceApi.update('style.second', {name: '内置线样式', description: '', tags: []})).rejects.toMatchObject({status: 409})
  })

  it('exports a portable free-text flow with visual dependencies and imports it again', async () => {
    const archive = await guideApi.export('example', sources.export, false)
    const entries = execFileSync('unzip', ['-Z1', archive.zip_path], { encoding: 'utf8' })
    expect(entries).toContain('FLOW.md')
    expect(entries).toContain('deps/style.first/')
    expect(entries).not.toMatch(/GUIDE\.md|resources\/data|\.csv/)
    const imported = await guideApi.import(archive.zip_path, false, 'portable-import')
    expect(imported.guide.meta.id).toBe('portable-import')
    expect((await guideApi.get('portable-import')).resolved_resources).toEqual(expect.arrayContaining([expect.objectContaining({id: 'style.first'})]))
  })

  it('exports a visual resource and rejects retired data-list imports', async () => {
    await expect(resourceApi.import(sources.data_list, '公共数据')).rejects.toMatchObject({ status: 400 })
    const exported = await resourceApi.export('style.first', sources.resource_export)
    expect(exported.path).toBe(sources.resource_export)
    expect(existsSync(exported.path!)).toBe(true)
    expect(execFileSync('unzip', ['-Z1', exported.path!], {encoding: 'utf8'})).toContain('style.qml')
    expect(await resourceApi.regeneratePreview('style.first')).toMatchObject({preview_status: 'pending'})
  })
})
