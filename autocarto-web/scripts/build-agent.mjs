// Build the Go sidecar before tauri-build copies externalBin. Never reuse an old
// binary after a failed build, and never disable CGO: SQLite requires it.
import { spawnSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { createReadStream } from 'node:fs'
import { chmod, copyFile, mkdir, mkdtemp, readFile, rename, rm, stat } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

export function targetConfig(target) {
  const match = /^(aarch64|x86_64)-(apple-darwin|pc-windows-(?:msvc|gnu|gnullvm)|unknown-linux-(?:gnu|musl))$/.exec(target)
  if (!match) throw new Error(`Unsupported Tauri target ${JSON.stringify(target)}. Build an explicit aarch64/x86_64 desktop target; no host fallback is used.`)
  const goos = match[2] === 'apple-darwin' ? 'darwin' : match[2].startsWith('pc-windows-') ? 'windows' : 'linux'
  return { target, goos, goarch: match[1] === 'aarch64' ? 'arm64' : 'amd64', extension: goos === 'windows' ? '.exe' : '' }
}

export function resolveTarget(argv, env, rustVersion) {
  let explicit
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i]
    if (arg !== '--target' && !arg.startsWith('--target=')) throw new Error(`Unknown argument: ${arg}`)
    if (explicit !== undefined) throw new Error('--target can only be specified once')
    explicit = arg === '--target' ? argv[++i] : arg.slice('--target='.length)
    if (!explicit || explicit.startsWith('--')) throw new Error('--target requires a Rust target triple')
  }
  const selected = explicit ?? env.TAURI_ENV_TARGET_TRIPLE ?? env.CARGO_BUILD_TARGET
  if (selected !== undefined) return targetConfig(selected.trim())
  const host = /^host:\s*(\S+)\s*$/m.exec(rustVersion())?.[1]
  if (!host) throw new Error('rustc -vV did not report a host triple. Pass --target explicitly.')
  return targetConfig(host)
}

export function buildEnvironment(env, target, host) {
  if (env.CGO_ENABLED === '0' || host.CGO_ENABLED === '0') {
    throw new Error('CGO_ENABLED=0 is not supported: carto-agent uses go-sqlite3. Enable CGO and install a target-compatible C compiler.')
  }
  const compiler = env[`CC_${target.target.replaceAll('-', '_')}`] || env.CC
  const cross = target.goos !== host.GOHOSTOS || target.goarch !== host.GOHOSTARCH || target.target.endsWith('-musl')
  if (cross && !compiler) {
    throw new Error(`Cross-compiling ${target.target} requires an explicit CC (or CC_${target.target.replaceAll('-', '_')}) for that target. Refusing to produce a SQLite-disabled sidecar.`)
  }
  return { ...env, GOOS: target.goos, GOARCH: target.goarch, CGO_ENABLED: '1', ...(compiler ? { CC: compiler } : {}) }
}

function run(command, args, options = {}) {
  const result = spawnSync(command, args, { ...options, encoding: 'utf8', stdio: options.capture ? ['ignore', 'pipe', 'pipe'] : 'inherit' })
  if (result.error) throw new Error(`Cannot run ${command}: ${result.error.message}`)
  if (result.status !== 0) throw new Error(`${command} ${args.join(' ')} failed (${result.status ?? result.signal}). ${result.stderr || ''}`)
  return result.stdout || ''
}

async function hashFile(filename) {
  const hash = createHash('sha256')
  for await (const chunk of createReadStream(filename)) hash.update(chunk)
  return hash.digest('hex')
}

// Build into OS temporary storage; only publish a verified, complete binary.
// Preserve mtime if unchanged, otherwise Tauri's file watcher would rebuild forever.
export async function publishBinary(source, destination) {
  const expected = await hashFile(source)
  try {
    if (await hashFile(destination) === expected) return false
  } catch (error) {
    if (error.code !== 'ENOENT') throw error
  }
  await mkdir(path.dirname(destination), { recursive: true })
  const staged = `${destination}.building-${process.pid}`
  try {
    await copyFile(source, staged)
    if (process.platform !== 'win32') await chmod(staged, 0o755)
    await rename(staged, destination)
  } finally {
    await rm(staged, { force: true })
  }
  return true
}

export async function buildAgent({ argv = process.argv.slice(2), env = process.env, root = webRoot, runner = run, log = console.log } = {}) {
  const goDir = path.resolve(root, '..', 'carto_agent')
  // Resolve relative paths against this script/project, never the caller's cwd.
  await readFile(path.join(goDir, 'go.mod'), 'utf8')
  const target = resolveTarget(argv, env, () => runner('rustc', ['-vV'], { cwd: root, env, capture: true }))
  const host = JSON.parse(runner('go', ['env', '-json', 'GOHOSTOS', 'GOHOSTARCH', 'CGO_ENABLED', 'GOVERSION'], { cwd: goDir, env, capture: true }))
  const buildEnv = buildEnvironment(env, target, host)
  const destination = path.join(root, 'src-tauri', 'binaries', `carto-agent-${target.target}${target.extension}`)
  const temporary = await mkdtemp(path.join(os.tmpdir(), 'autocarto-agent-build-'))
  try {
    const binary = path.join(temporary, `carto-agent${target.extension}`)
    log(`[agent:build] ${target.target} using ${host.GOVERSION}; CGO_ENABLED=1`)
    // Running inside the module honors go.mod's Go/toolchain requirements.
    runner('go', ['build', '-trimpath', '-o', binary, './cmd/agent'], { cwd: goDir, env: buildEnv })
    if (!(await stat(binary)).isFile()) throw new Error('Go build did not produce a sidecar file')
    const metadata = runner('go', ['version', '-m', binary], { cwd: goDir, env: buildEnv, capture: true })
    for (const setting of [`GOOS=${target.goos}`, `GOARCH=${target.goarch}`, 'CGO_ENABLED=1']) {
      if (!metadata.split(/\r?\n/).some(line => line.trim() === `build\t${setting}`)) {
        throw new Error(`Built sidecar metadata is missing ${setting}; refusing to publish ${destination}`)
      }
    }
    const changed = await publishBinary(binary, destination)
    log(`[agent:build] ${changed ? 'Updated' : 'Verified unchanged'} ${destination}`)
    return destination
  } finally {
    await rm(temporary, { recursive: true, force: true })
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  buildAgent().catch(error => {
    console.error(`[agent:build] ${error.message}\nThe desktop build is stopped; an old sidecar must not be used. Check the Go toolchain from carto_agent/go.mod and the target C compiler.`)
    process.exitCode = 1
  })
}
