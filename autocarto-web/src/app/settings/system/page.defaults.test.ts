import fs from 'node:fs'
import path from 'node:path'

import { describe, expect, it } from 'vitest'

describe('SystemSettingsPage defaults', () => {
  it('uses plugin-relative log and data directories', () => {
    const source = fs.readFileSync(path.join(__dirname, 'page.tsx'), 'utf8')

    expect(source).toContain('logDir: "data/logs"')
    expect(source).toContain('dataDir: "data"')
    expect(source).not.toContain('logDir: "~/.autocarto/logs"')
    expect(source).not.toContain('dataDir: "~/.autocarto"')
  })

  it('keeps log and data directories fixed to plugin-managed paths', () => {
    const source = fs.readFileSync(path.join(__dirname, 'page.tsx'), 'utf8')

    expect(source).toContain('logDir: defaultForm.logDir')
    expect(source).toContain('dataDir: defaultForm.dataDir')
    expect(source).toContain('{ path: "log.dir", value: defaultForm.logDir }')
    expect(source).toContain('{ path: "data_dir", value: defaultForm.dataDir }')
  })

  it('renders log and data directories as read-only fields without browse actions', () => {
    const source = fs.readFileSync(path.join(__dirname, 'page.tsx'), 'utf8')

    expect(source).toContain('<Field label="日志目录" description="固定写入插件目录下的 data/logs">')
    expect(source).toContain('readOnly')
    expect(source).toContain('aria-readonly')
    expect(source).not.toContain('void handlePickDirectory("logDir", "选择日志目录失败")')
    expect(source).not.toContain('void handlePickDirectory("dataDir", "选择数据目录失败")')
  })
})
