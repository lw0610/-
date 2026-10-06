export type AnchoredOverlayPlacement = 'bottom-end' | 'right-start'

interface AnchoredOverlayOptions {
  anchorRect: DOMRect
  overlayWidth: number
  overlayHeight: number
  viewportWidth: number
  viewportHeight: number
  placement?: AnchoredOverlayPlacement
  margin?: number
  gap?: number
}

interface AnchoredOverlayPosition {
  top: number
  left: number
}

function clamp(value: number, min: number, max: number) {
  if (max <= min) {
    return min
  }
  return Math.min(max, Math.max(min, value))
}

export function getAnchoredOverlayPosition({
  anchorRect,
  overlayWidth,
  overlayHeight,
  viewportWidth,
  viewportHeight,
  placement = 'bottom-end',
  margin = 8,
  gap = 8,
}: AnchoredOverlayOptions): AnchoredOverlayPosition {
  const maxLeft = Math.max(margin, viewportWidth - overlayWidth - margin)
  const maxTop = Math.max(margin, viewportHeight - overlayHeight - margin)

  if (placement === 'right-start') {
    return {
      top: clamp(anchorRect.top, margin, maxTop),
      left: clamp(anchorRect.right + gap, margin, maxLeft),
    }
  }

  const aboveTop = anchorRect.top - gap - overlayHeight
  const nextTop = anchorRect.bottom + gap + overlayHeight <= viewportHeight - margin
    ? anchorRect.bottom + gap
    : clamp(aboveTop, margin, maxTop)

  return {
    top: nextTop,
    left: clamp(anchorRect.right - overlayWidth, margin, maxLeft),
  }
}
