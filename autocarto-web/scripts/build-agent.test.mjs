import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { mkdtemp, mkdir, readFile, rm, stat, symlink, writeFile } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { buildAgent, buildEnvironment, publishBinary, resolveTarget, targetConfig } from './build-agent.mjs'

test('symlink CLI entry executes but importing the same module does not', async t => {
  const dir = await mkdtemp(path.join(os.tmpdir(), 'agent-entry-test-'))
  t.after(() => rm(dir, { recursive: true, force: true }))
  const link = path.join(dir, 'scripts')
  await symlink(fileURLToPath(new URL('.', import.meta.url)), link, process.platform === 'win32' ? 'junction' : 'dir')
  const entry = path.join(link, 'build-agent.mjs')
  const cli = spawnSync(process.execPath, [entry, '--unknown-entry-regression'], { encoding: 'utf8', timeout: 10_000 })
  assert.equal(cli.error, undefined)
  assert.equal(cli.status, 1, cli.stderr)
  assert.match(cli.stderr, /Unknown argument: --unknown-entry-regression/)
  assert.equal(cli.stdout, '')

  const importer = path.join(dir, 'importer.mjs')
  await writeFile(importer, `import ${JSON.stringify(pathToFileURL(entry).href)}; console.log('imported without building')\n`)
  const imported = spawnSync(process.execPath, [importer, '--unknown-entry-regression'], { encoding: 'utf8', timeout: 10_000 })
  assert.equal(imported.error, undefined)
  assert.equal(imported.status, 0, imported.stderr)
  assert.equal(imported.stdout.trim(), 'imported without building')
  assert.equal(imported.stderr, '')
})

test('maps both desktop architectures and Windows executable suffix', () => {
  for (const [arch, goarch] of [['aarch64', 'arm64'], ['x86_64', 'amd64']]) {
    for (const [suffix, goos] of [['apple-darwin', 'darwin'], ['pc-windows-msvc', 'windows'], ['pc-windows-gnu', 'windows'], ['unknown-linux-gnu', 'linux'], ['unknown-linux-musl', 'linux']]) {
      const config = targetConfig(`${arch}-${suffix}`)
      assert.equal(config.goos, goos)
      assert.equal(config.goarch, goarch)
      assert.equal(config.extension, goos === 'windows' ? '.exe' : '')
    }
  }
})

test('uses actual Cargo/Tauri target; never falls back from invalid explicit targets', () => {
  const noHost = () => assert.fail('must not inspect host for an explicit target')
  assert.equal(resolveTarget(['--target', 'x86_64-pc-windows-msvc'], { TAURI_ENV_TARGET_TRIPLE: 'aarch64-apple-darwin' }, noHost).goos, 'windows')
  assert.equal(resolveTarget([], { TAURI_ENV_TARGET_TRIPLE: 'aarch64-unknown-linux-gnu' }, noHost).goarch, 'arm64')
  assert.equal(resolveTarget([], {}, () => 'rustc x\nhost: aarch64-apple-darwin\n').target, 'aarch64-apple-darwin')
  for (const target of ['', 'aarch64-linux', 'universal-apple-darwin', 'i686-pc-windows-msvc', '../../bad']) {
    assert.throws(() => resolveTarget([], { TAURI_ENV_TARGET_TRIPLE: target }, noHost), /Unsupported/)
  }
  assert.throws(() => resolveTarget(['--target'], {}, noHost), /requires/)
  assert.throws(() => resolveTarget(['--wat'], {}, noHost), /Unknown/)
})

test('requires SQLite CGO and explicit cross compiler, ignoring inherited GOOS/GOARCH', () => {
  const host = { GOHOSTOS: 'darwin', GOHOSTARCH: 'arm64', CGO_ENABLED: '1' }
  const native = targetConfig('aarch64-apple-darwin')
  assert.throws(() => buildEnvironment({ CGO_ENABLED: '0' }, native, host), /go-sqlite3/)
  assert.throws(() => buildEnvironment({}, targetConfig('x86_64-unknown-linux-gnu'), host), /explicit CC/)
  const env = buildEnvironment({ GOOS: 'bad', GOARCH: 'bad' }, native, host)
  assert.equal(env.GOOS, 'darwin')
  assert.equal(env.GOARCH, 'arm64')
  assert.equal(env.CGO_ENABLED, '1')
  assert.equal(buildEnvironment({ CC_x86_64_unknown_linux_gnu: 'x86_64-linux-gnu-gcc' }, targetConfig('x86_64-unknown-linux-gnu'), host).CC, 'x86_64-linux-gnu-gcc')
})

test('publishes complete output and avoids timestamp churn for unchanged binary', async t => {
  const dir = await mkdtemp(path.join(os.tmpdir(), 'agent-publish-test-'))
  t.after(() => rm(dir, { recursive: true, force: true }))
  const source = path.join(dir, 'source')
  const destination = path.join(dir, 'bin', 'agent')
  await writeFile(source, 'current binary')
  assert.equal(await publishBinary(source, destination), true)
  const before = await stat(destination)
  assert.equal(await publishBinary(source, destination), false)
  assert.equal((await stat(destination)).mtimeMs, before.mtimeMs)
  assert.equal(await readFile(destination, 'utf8'), 'current binary')
})

test('failed Go build cannot publish or silently reuse the old backend', async t => {
  const dir = await mkdtemp(path.join(os.tmpdir(), 'agent-build-failure-test-'))
  t.after(() => rm(dir, { recursive: true, force: true }))
  const root = path.join(dir, 'web')
  await mkdir(path.join(dir, 'carto_agent'))
  await writeFile(path.join(dir, 'carto_agent', 'go.mod'), 'module test\ngo 1.24\n')
  const binaries = path.join(root, 'src-tauri', 'binaries')
  await mkdir(binaries, { recursive: true })
  const destination = path.join(binaries, 'carto-agent-aarch64-apple-darwin')
  await writeFile(destination, 'old binary')
  const runner = (command, args) => {
    assert.equal(command, 'go')
    if (args[0] === 'env') return JSON.stringify({ GOHOSTOS: 'darwin', GOHOSTARCH: 'arm64', CGO_ENABLED: '1', GOVERSION: 'go1.24.11' })
    if (args[0] === 'build') throw new Error('compiler failed')
    assert.fail(`unexpected command ${args}`)
  }
  await assert.rejects(buildAgent({ argv: ['--target=aarch64-apple-darwin'], env: {}, root, runner, log: () => {} }), /compiler failed/)
  assert.equal(await readFile(destination, 'utf8'), 'old binary')
})

test('desktop hook owns backend build without changing web scripts', async () => {
  const packageJSON = JSON.parse(await readFile(new URL('../package.json', import.meta.url), 'utf8'))
  assert.equal(packageJSON.scripts.dev, 'next dev -p 3000')
  assert.equal(packageJSON.scripts.build, 'next build')
  const buildRS = await readFile(new URL('../src-tauri/build.rs', import.meta.url), 'utf8')
  assert.ok(buildRS.indexOf('status.success()') < buildRS.indexOf('tauri_build::build()'))
  for (const source of ['"go.mod"', '"go.sum"', '"cmd"', '"internal"', '"defaults"']) assert.ok(buildRS.includes(source))
})
