import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

function readChatPanelSource() {
  return readFileSync(resolve(process.cwd(), 'src/components/chat/ChatPanel.tsx'), 'utf8')
}

describe('ChatPanel architecture', () => {
  it('导入拆分后的四个 hooks', () => {
    const source = readChatPanelSource()

    expect(source).toContain("@/hooks/useChatSession")
    expect(source).toContain("@/hooks/useChatMessaging")
    expect(source).toContain("@/hooks/useChatContext")
    expect(source).toContain("@/hooks/useChatRuntimeConfig")
  })

  it('移除主要业务回调并保持为薄布局壳', () => {
    const source = readChatPanelSource()
    const lineCount = source.split('\n').length

    expect(source).not.toContain('const ensureSession = useCallback')
    expect(source).not.toContain('const loadSessionMessages = useCallback')
    expect(source).not.toContain('const handleSend = useCallback')
    expect(source).not.toContain('const handleCancel = useCallback')
    expect(source).not.toContain('const handleRefreshContextStats = useCallback')
    expect(source).not.toContain('const handleCompressContext = useCallback')
    expect(source).not.toContain('const loadRuntimeConfig = useCallback')
    expect(source).not.toContain('const handleModelSwitch = useCallback')
    expect(source).not.toContain('const handleAgentConfigSwitch = useCallback')
    expect(lineCount).toBeLessThanOrEqual(220)
  })
})
