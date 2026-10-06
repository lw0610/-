'use client'

import { RefObject, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { getAnchoredOverlayPosition, type AnchoredOverlayPlacement } from '@/lib/anchoredOverlay'
import ProjectLaunchMenu, { PROJECT_LAUNCH_MENU_WIDTH, type ProjectLaunchMenuProps } from './ProjectLaunchMenu'

const VIEWPORT_MARGIN = 8
const PANEL_GAP = 8

interface PanelState {
  top: number
  left: number
  ready: boolean
}

interface ProjectLaunchPopoverProps extends Omit<ProjectLaunchMenuProps, 'className' | 'onRequestClose'> {
  open: boolean
  anchorRef: RefObject<HTMLElement | null>
  placement?: AnchoredOverlayPlacement
  onRequestClose: (restoreFocus?: boolean) => void
}

export default function ProjectLaunchPopover({
  open,
  anchorRef,
  placement = 'bottom-end',
  onRequestClose,
  ...menuProps
}: ProjectLaunchPopoverProps) {
  const panelRef = useRef<HTMLDivElement | null>(null)
  const [mounted, setMounted] = useState(false)
  const [panelState, setPanelState] = useState<PanelState>({
    top: VIEWPORT_MARGIN,
    left: VIEWPORT_MARGIN,
    ready: false,
  })

  useEffect(() => {
    setMounted(true)
  }, [])

  useEffect(() => {
    if (open) {
      return
    }
    setPanelState((current) => ({ ...current, ready: false }))
  }, [open])

  useLayoutEffect(() => {
    if (!mounted || !open) {
      return
    }

    const updatePosition = () => {
      const anchor = anchorRef.current
      const panel = panelRef.current
      if (!anchor || !panel || typeof window === 'undefined') {
        return
      }

      const anchorRect = anchor.getBoundingClientRect()
      const panelRect = panel.getBoundingClientRect()
      const nextPosition = getAnchoredOverlayPosition({
        anchorRect,
        overlayWidth: panelRect.width || PROJECT_LAUNCH_MENU_WIDTH,
        overlayHeight: panelRect.height || panel.offsetHeight,
        viewportWidth: window.innerWidth,
        viewportHeight: window.innerHeight,
        placement,
        margin: VIEWPORT_MARGIN,
        gap: PANEL_GAP,
      })

      setPanelState({
        ...nextPosition,
        ready: true,
      })
    }

    updatePosition()
    const rafId = window.requestAnimationFrame(updatePosition)
    const observer = typeof ResizeObserver !== 'undefined' && panelRef.current
      ? new ResizeObserver(() => {
        updatePosition()
      })
      : null

    if (observer && panelRef.current) {
      observer.observe(panelRef.current)
    }

    window.addEventListener('resize', updatePosition)
    window.addEventListener('scroll', updatePosition, true)

    return () => {
      window.cancelAnimationFrame(rafId)
      observer?.disconnect()
      window.removeEventListener('resize', updatePosition)
      window.removeEventListener('scroll', updatePosition, true)
    }
  }, [anchorRef, mounted, open, placement])

  useEffect(() => {
    if (!mounted || !open) {
      return
    }

    const handlePointerDown = (event: MouseEvent) => {
      const target = event.target as Node | null
      if (!target) {
        return
      }
      if (anchorRef.current?.contains(target) || panelRef.current?.contains(target)) {
        return
      }
      onRequestClose(false)
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        onRequestClose(true)
      }
    }

    document.addEventListener('mousedown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)

    return () => {
      document.removeEventListener('mousedown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [anchorRef, mounted, onRequestClose, open])

  if (!mounted || !open || typeof document === 'undefined') {
    return null
  }

  return createPortal(
    <div
      ref={panelRef}
      style={{
        top: panelState.top,
        left: panelState.left,
        width: PROJECT_LAUNCH_MENU_WIDTH,
        maxWidth: `calc(100vw - ${VIEWPORT_MARGIN * 2}px)`,
        visibility: panelState.ready ? 'visible' : 'hidden',
      }}
      className="fixed z-50"
    >
      <ProjectLaunchMenu
        {...menuProps}
        onRequestClose={() => onRequestClose(true)}
      />
    </div>,
    document.body,
  )
}
