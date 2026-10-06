'use client'

import AppShell from '@/components/layout/AppShell'
import ChatPanel from '@/components/chat/ChatPanel'
import ProjectNotConnected from '@/components/chat/ProjectNotConnected'
import { useConnectionStore } from '@/stores/connectionStore'
import { useInstanceStore } from '@/stores/instanceStore'
import { useChatStore } from '@/stores/chatStore'

export default function ChatPage() {
  const connected = useConnectionStore((s) => s.connected)
  const projectConnected = useInstanceStore((s) => s.projectConnected)
  const messages = useChatStore((s) => s.messages)
  const keepChatVisible = connected || projectConnected || messages.length > 0

  return (
    <AppShell>
      {projectConnected || keepChatVisible ? <ChatPanel /> : <ProjectNotConnected />}
    </AppShell>
  )
}
