'use client'
import { useEffect, useState } from 'react'
import { loadResourcePreview } from './api'
import type { ResolvedResource } from './types'
function SymbolThumbnail({
  resource,
  index,
}: {
  resource: ResolvedResource
  index: number
}) {
  const [url, setURL] = useState('')
  useEffect(() => {
    setURL('')
    let active = true,
      objectURL = ''
    if (!resource.id) return
    void loadResourcePreview(resource.id, index)
      .then((blob) => {
        if (!active) return
        objectURL = URL.createObjectURL(blob)
        setURL(objectURL)
      })
      .catch(() => {})
    return () => {
      active = false
      if (objectURL) URL.revokeObjectURL(objectURL)
    }
  }, [resource.id, resource.digest, index])
  return url ? (
    <figure className="rounded border border-zinc-200 p-2 dark:border-zinc-800">
      {/* Backend-checked symbol images use a revocable Blob URL. */}
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img
        src={url}
        alt={resource.entries[index].name}
        className="h-16 w-16 object-contain"
      />
      <figcaption className="mt-1 max-w-24 truncate text-xs">
        {resource.entries[index].name}
      </figcaption>
    </figure>
  ) : null
}
export default function ResourceMedia({
  resource,
}: {
  resource: ResolvedResource
}) {
  const [text, setText] = useState(''),
    [pdfURL, setPDFURL] = useState(''),
    [error, setError] = useState('')
  useEffect(() => {
    setText('')
    setPDFURL('')
    setError('')
    let active = true,
      objectURL = ''
    if (resource.type === 'doc' && resource.id)
      void loadResourcePreview(resource.id)
        .then(async (blob) => {
          if (!active) return
          if (blob.type === 'application/pdf') {
            objectURL = URL.createObjectURL(blob)
            setPDFURL(objectURL)
          } else {
            const value = await blob.text()
            if (active) setText(value)
          }
        })
        .catch((err) => {
          if (active) setError(String(err))
        })
    return () => {
      active = false
      if (objectURL) URL.revokeObjectURL(objectURL)
    }
  }, [resource.type, resource.id, resource.digest])
  if (resource.type === 'symbols')
    return (
      <div className="mt-3 flex flex-wrap gap-2">
        {resource.entries.slice(0, 24).map((_item, index) => (
          <SymbolThumbnail key={index} resource={resource} index={index} />
        ))}
      </div>
    )
  if (resource.type === 'doc' && pdfURL)
    return (
      <iframe
        title={`${resource.label}预览`}
        src={pdfURL}
        sandbox=""
        referrerPolicy="no-referrer"
        className="mt-3 h-96 w-full rounded border border-zinc-200"
      />
    )
  if (resource.type === 'doc')
    return (
      <div className="mt-3">
        <pre className="max-h-80 overflow-auto whitespace-pre-wrap rounded bg-zinc-50 p-3 text-xs dark:bg-zinc-950">
          {text || error || '正在加载预览…'}
        </pre>
      </div>
    )
  return null
}
