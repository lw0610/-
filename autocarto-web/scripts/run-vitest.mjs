import { spawnSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)
const passthroughArgs = process.argv.slice(2).filter((arg) => arg !== '--runInBand')
const vitestCliPath = path.resolve(__dirname, '..', 'node_modules', 'vitest', 'vitest.mjs')

if (!existsSync(vitestCliPath)) {
  throw new Error(`Cannot find vitest CLI at ${vitestCliPath}. Run npm ci first.`)
}

const result = spawnSync(process.execPath, [vitestCliPath, '--run', ...passthroughArgs], {
  stdio: 'inherit',
  env: process.env,
})

if (typeof result.status === 'number') {
  process.exit(result.status)
}

if (result.error) {
  throw result.error
}

process.exit(1)
