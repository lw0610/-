'use client'
import { useRef, useState } from 'react'
import { buttonClass } from './PreviewPanels'

/** A scrollable, natural-aspect image. Zoom changes only the view, never the style. */
export default function PreviewImage({ url, alt }: { url: string; alt: string }) {
  const image = useRef<HTMLImageElement>(null)
  const [zoom, setZoom] = useState<number | null>(null)
  const [width, setWidth] = useState(0)
  const changeZoom = (factor: number) => {
    const current = zoom ?? (image.current?.naturalWidth ? image.current.getBoundingClientRect().width / image.current.naturalWidth * 100 : 100)
    setZoom(Math.min(400, Math.max(10, Math.round((current || 100) * factor))))
  }
  return <div className="space-y-2">
    <div className="flex flex-wrap items-center gap-2" aria-label="预览缩放">
      <button type="button" className={buttonClass} onClick={() => changeZoom(0.8)} aria-label="缩小预览">−</button>
      <span className="min-w-16 text-center text-xs">{zoom === null ? '适合窗口' : `${zoom}%`}</span>
      <button type="button" className={buttonClass} onClick={() => changeZoom(1.25)} aria-label="放大预览">+</button>
      <button type="button" className={buttonClass} onClick={() => setZoom(null)}>适合窗口</button>
      <button type="button" className={buttonClass} onClick={() => setZoom(100)}>原始大小</button>
    </div>
    <div className="h-[58vh] overflow-auto rounded-lg border bg-zinc-100 p-3 dark:bg-zinc-800" aria-label="预览图像区域">
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img ref={image} src={url} alt={alt} onLoad={event => setWidth(event.currentTarget.naturalWidth)} className="mx-auto block" style={zoom === null ? {maxWidth: '100%', maxHeight: '100%', width: 'auto', height: 'auto'} : {width: width ? `${width * zoom / 100}px` : `${zoom}%`, maxWidth: 'none', height: 'auto'}} />
    </div>
  </div>
}
