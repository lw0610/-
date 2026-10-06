import type { Metadata } from 'next'
import './globals.css'
import WebSocketProvider from '@/components/providers/WebSocketProvider'
import { Toaster } from 'sonner'

export const metadata: Metadata = {
  title: 'AutoCarto AI',
  description: 'AI-powered GIS assistant',
  icons: {
    icon: '/logo.svg',
    shortcut: '/logo.svg',
    apple: '/logo.svg',
  },
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="zh-CN">
      <body className="antialiased">
        <WebSocketProvider>
          {children}
          <Toaster position="top-right" richColors closeButton theme="system" />
        </WebSocketProvider>
      </body>
    </html>
  )
}
