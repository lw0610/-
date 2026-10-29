'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import api from '@/lib/api'
import { isDesktopRuntime, pickDirectoryPath, pickFilePath } from '@/lib/config-form-helpers'
import type { ChatAttachment, ChatInputMetadata, ChatLayerReference } from '@/types/message'

type GuideOption = { id: string; name: string; description?: string; enabled: boolean }
export function useMessageInputs(sessionId?: string, instanceId?: string) {
  const [attachments, setAttachments] = useState<ChatAttachment[]>([])
  const [layers, setLayers] = useState<ChatLayerReference[]>([])
  const [guide, setGuide] = useState<GuideOption | null>(null)
  const [guides, setGuides] = useState<GuideOption[]>([])
  const [availableLayers, setAvailableLayers] = useState<ChatLayerReference[]>([])
  const [menu, setMenu] = useState<'guides' | 'layers' | ''>('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const context = useRef({ sessionId, instanceId })
  if (context.current.sessionId !== sessionId || context.current.instanceId !== instanceId) context.current = { sessionId, instanceId }
  const acceptPaths = useCallback(async (paths: string[]) => {
    if (!paths.length) return
    const origin = context.current
    setLoading(true); setError('')
    try {
      const result = await api.post<{ attachments: ChatAttachment[] }>('/api/chat/attachments/check', { paths })
      if (origin !== context.current) return
      setAttachments(current => [...current, ...result.attachments.filter(item => !current.some(old => old.path === item.path))])
    } catch (reason) { if (origin === context.current) setError(reason instanceof Error ? reason.message : '无法添加附件') }
    finally { if (origin === context.current) setLoading(false) }
  }, [])
  const browse = async (folder: boolean) => {
    try { const path = await (folder ? pickDirectoryPath() : pickFilePath()); if (path) await acceptPaths([path]) }
    catch (reason) { setError(reason instanceof Error ? reason.message : '选择附件失败') }
  }
  const openMenu = async (next: 'guides' | 'layers') => {
    const origin = context.current
    setMenu(next); setLoading(true); setError('')
    try {
      if (next === 'guides') {
        const values = await api.get<GuideOption[]>('/api/guides')
        if (origin === context.current) setGuides(values.filter(value => value.enabled !== false))
      } else {
        if (!instanceId) throw new Error('请先连接 QGIS 工程')
        const result = await api.get<{ items: ChatLayerReference[] }>(`/api/instances/${encodeURIComponent(instanceId)}/layers`)
        if (origin === context.current) setAvailableLayers(result.items || [])
      }
    } catch (reason) { if (origin === context.current) setError(reason instanceof Error ? reason.message : '读取选项失败') }
    finally { if (origin === context.current) setLoading(false) }
  }
  useEffect(() => {
    setAttachments([]); setLayers([]); setGuide(null); setMenu(''); setError(''); setLoading(false)
  }, [sessionId, instanceId])
  useEffect(() => {
    const id = new URLSearchParams(window.location.search).get('guide')
    if (!id) return
    let active = true
    const origin = context.current
    void api.get<GuideOption[]>('/api/guides').then(values => {
      if (!active || origin !== context.current) return
      const selected = values.find(value => value.id === id && value.enabled !== false)
      if (selected) {
        setGuide(selected)
        const url = new URL(window.location.href); url.searchParams.delete('guide'); window.history.replaceState(window.history.state, '', url.pathname + url.search + url.hash)
      }
      else setError('指定的指南已停用或不存在')
    }).catch(reason => { if (active) setError(reason instanceof Error ? reason.message : '读取指南失败') })
    return () => { active = false }
  }, [sessionId, instanceId])
  useEffect(() => {
    if (!isDesktopRuntime()) return
    let active = true
    let unlisten: (() => void) | undefined
    void import('@tauri-apps/api/webview').then(async ({ getCurrentWebview }) => {
      const stop = await getCurrentWebview().onDragDropEvent(event => {
        if (active && event.payload.type === 'drop') void acceptPaths(event.payload.paths)
      })
      if (active) unlisten = stop
      else stop()
    }).catch(() => { if (active) setError('拖入附件不可用，请使用添加文件或文件夹按钮。') })
    return () => { active = false; unlisten?.() }
  }, [acceptPaths])
  const metadata = (): ChatInputMetadata => ({ ...(attachments.length ? { attachments } : {}), ...(layers.length ? { layers } : {}), ...(guide ? { guide_id: guide.id } : {}) })
  return { attachments, layers, guide, guides, availableLayers, menu, error, loading, browse, acceptPaths, openMenu, setMenu, setError, metadata,
    isCurrent: () => context.current.sessionId === sessionId && context.current.instanceId === instanceId,
    selectGuide: (value: GuideOption) => { setGuide(value); setMenu('') },
    selectLayer: (value: ChatLayerReference) => { setLayers(current => current.some(item => item.id === value.id) ? current : [...current, value]); setMenu('') },
    removeAttachment: (path: string) => setAttachments(current => current.filter(item => item.path !== path)),
    removeLayer: (id: string) => setLayers(current => current.filter(item => item.id !== id)),
    removeGuide: () => setGuide(null),
    clear: () => { setAttachments([]); setLayers([]); setGuide(null); setMenu('') },
  }
}
