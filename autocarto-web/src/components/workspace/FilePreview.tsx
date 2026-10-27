'use client'
/* eslint-disable @next/next/no-img-element -- 会话本地图片直接请求受限预览接口，不能交给图片优化代理。 */
import { useEffect, useState } from 'react'
import api from '@/lib/api'
import { resourceApi } from '@/features/flows/api'
import {
  backendURL,
  contentPath,
  fileApi,
  fileSize,
  kindLabels,
  localFileAction,
  textPreview,
  type SessionFile,
} from './files'
function RecordView({ value }: { value: unknown }) {
  return (
    <pre className="whitespace-pre-wrap break-all rounded bg-zinc-100 p-3 text-[11px] dark:bg-zinc-900">
      {JSON.stringify(value, null, 2)}
    </pre>
  )
}
function RunView({ value }: { value: Record<string, unknown> }) {
  const steps = Array.isArray(value.steps)
    ? (value.steps as Record<string, unknown>[])
    : []
  return (
    <div className="space-y-3 text-xs">
      <h3 className="font-semibold">
        {String(value.name || value.flow_id || '流程运行')}
      </h3>
      <p>
        状态：{String(value.status || '未知')} ·{' '}
        {String(value.started_at || '')}
      </p>
      <details open>
        <summary>输入</summary>
        <RecordView value={value.inputs || {}} />
      </details>
      <section>
        <h4 className="mb-2 font-medium">步骤</h4>
        {steps.map((step, index) => (
          <div key={index} className="border-b py-2">
            {index + 1}. {String(step.title || step.id)} ·{' '}
            {String(step.status || '待执行')}
          </div>
        ))}
      </section>
      <details open>
        <summary>成果与数据来源</summary>
        <RecordView
          value={{
            成果: value.result,
            数据获取:
              value.acquisition ||
              value.acquisition_state ||
              value.acquired_data,
          }}
        />
      </details>
    </div>
  )
}
export default function FilePreview({
  item,
  sessionId,
  projectPath,
  onBack,
  onFullscreen,
}: {
  item: SessionFile
  sessionId: string
  projectPath: string
  onBack: () => void
  onFullscreen?: () => void
}) {
  const [text, setText] = useState(''),
    [data, setData] = useState<Record<string, unknown> | null>(null),
    [thumbnail, setThumbnail] = useState(''),
    [error, setError] = useState(''),
    [feedback, setFeedback] = useState(''),
    [loading, setLoading] = useState(false),
    [truncated, setTruncated] = useState(false),
    [zoom, setZoom] = useState(100),
    [layer, setLayer] = useState('')
  const path = contentPath(sessionId, projectPath, item),
    url = backendURL(path),
    ext = item.name.split('.').pop()?.toLowerCase(),
    geo = item.kind === 'vector' || item.kind === 'raster'
  useEffect(() => {
    let live = true
    const controller = new AbortController()
    setText('')
    setData(null)
    setThumbnail('')
    setError('')
    setFeedback('')
    setTruncated(false)
    setLoading(true)
    const run = async () => {
      try {
        if (geo) {
          const info = await fileApi.action(
            sessionId,
            projectPath,
            item,
            'inspect',
            layer,
          )
          if (!live) return
          setData(info)
          try {
            const preview = await fileApi.action<{ url: string }>(
              sessionId,
              projectPath,
              item,
              'thumbnail',
              layer,
            )
            if (live) setThumbnail(backendURL(preview.url))
          } catch (error) {
            if (live)
              setFeedback(
                error instanceof Error ? error.message : String(error),
              )
          }
        } else if (['csv', 'tsv', 'xlsx', 'zip'].includes(ext || '')) {
          const value = await api.get<Record<string, unknown>>(path)
          if (live) {
            setData(value)
            setTruncated(value.truncated === true)
          }
        } else if (['text', 'json', 'run'].includes(item.kind)) {
          const value = await textPreview(path, controller.signal)
          if (live) {
            setText(value.text)
            setTruncated(value.truncated)
            if (item.kind === 'run') {
              try {
                setData(JSON.parse(value.text))
              } catch {
                setError('运行记录不完整，以下显示原始内容')
              }
            }
          }
        }
      } catch (error) {
        if (live)
          setError(error instanceof Error ? error.message : String(error))
      } finally {
        if (live) setLoading(false)
      }
    }
    void run()
    return () => {
      live = false
      controller.abort()
    }
  }, [item, sessionId, projectPath, path, ext, geo, layer])
  const action = async (
    name: 'open' | 'reveal' | 'copy' | 'load' | 'import',
  ) => {
    setFeedback('')
    try {
      if (name === 'load') {
        await fileApi.action(sessionId, projectPath, item, 'load', layer)
        setFeedback('已加载到本会话的 QGIS 工程')
      } else if (name === 'import') {
        await resourceApi.import(item.path)
        setFeedback('已导入模板与样式库')
      } else setFeedback(await localFileAction(item.path, name))
    } catch (error) {
      setFeedback(error instanceof Error ? error.message : String(error))
    }
  }
  const rows = Array.isArray(data?.rows) ? (data.rows as string[][]) : []
  const layers = Array.isArray(data?.layers)
    ? (data.layers as Record<string, unknown>[])
    : []
  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="space-y-2 border-b border-zinc-200 p-3 dark:border-zinc-800">
        <div className="flex items-center gap-2">
          <button onClick={onBack} className="text-xs text-blue-600">
            ← 返回
          </button>
          <h2
            className="min-w-0 flex-1 truncate text-sm font-semibold"
            title={item.name}
          >
            {item.name}
          </h2>
          {onFullscreen && (
            <button onClick={onFullscreen} className="text-xs">
              放大
            </button>
          )}
        </div>
        <p className="text-[11px] text-zinc-500">
          {item.origin.label || '会话文件'} · {kindLabels[item.kind] || '文件'}{' '}
          · {fileSize(item.size)}
        </p>
        <div className="flex flex-wrap gap-3 text-xs">
          <button onClick={() => void action('open')}>打开</button>
          <button onClick={() => void action('reveal')}>在文件夹中显示</button>
          <button onClick={() => void action('copy')}>复制路径</button>
          {geo && (
            <button
              className="text-blue-600"
              onClick={() => void action('load')}
            >
              加载到 QGIS
            </button>
          )}
          {['qgis_style', 'qgis_layout'].includes(item.kind) && (
            <button
              className="text-blue-600"
              onClick={() => void action('import')}
            >
              导入到模板与样式库
            </button>
          )}
        </div>
      </header>
      <div className="min-h-0 flex-1 overflow-auto p-3 space-y-3">
        {error && (
          <p role="alert" className="text-xs text-red-600">
            {error}
          </p>
        )}
        {feedback && (
          <p role="status" className="text-xs text-blue-700">
            {feedback}
          </p>
        )}
        {loading && <p className="text-xs text-zinc-500">正在读取文件…</p>}
        {truncated && (
          <p className="text-xs text-amber-700">
            内容较长，仅显示预览范围。完整内容可用系统程序打开。
          </p>
        )}
        {item.kind === 'image' && (
          <>
            <div className="flex gap-3 text-xs">
              <button onClick={() => setZoom(100)}>适应宽度</button>
              <button
                onClick={() => setZoom((value) => Math.max(25, value - 25))}
              >
                缩小
              </button>
              <span>{zoom}%</span>
              <button
                onClick={() => setZoom((value) => Math.min(400, value + 25))}
              >
                放大
              </button>
            </div>
            <img
              src={url}
              alt={item.name}
              style={{ width: `${zoom}%`, maxWidth: 'none' }}
              onError={() =>
                setError('图片无法预览或超过 50 MB，请用系统程序打开')
              }
            />
          </>
        )}
        {item.kind === 'pdf' && (
          <>
            <p className="text-xs text-zinc-500">
              若下方未显示 PDF，请点击“打开”使用系统程序查看。
            </p>
            <object
              data={url}
              type="application/pdf"
              className="h-[75vh] w-full"
            >
              <button onClick={() => void action('open')}>
                用系统程序打开 PDF
              </button>
            </object>
          </>
        )}
        {geo && data && (
          <>
            {layers.length > 1 && (
              <select
                aria-label="预览子图层"
                value={layer}
                onChange={(event) => setLayer(event.target.value)}
                className="w-full border p-1 text-xs"
              >
                <option value="">默认图层</option>
                {layers.map((entry) => (
                  <option key={String(entry.name)} value={String(entry.name)}>
                    {String(entry.name)}
                  </option>
                ))}
              </select>
            )}
            {thumbnail && (
              <img
                src={thumbnail}
                alt="地理数据缩略图"
                className="mx-auto max-w-full"
              />
            )}
            <h3 className="text-xs font-medium">坐标系、范围与数据字段</h3>
            <RecordView value={data} />
          </>
        )}
        {rows.length > 0 && (
          <table className="w-full border-collapse text-xs">
            <tbody>
              {rows.map((row, index) => (
                <tr key={index}>
                  {row.map((cell, column) =>
                    index === 0 ? (
                      <th
                        key={column}
                        className="whitespace-nowrap border bg-zinc-100 p-2 text-left dark:bg-zinc-800"
                      >
                        {cell}
                      </th>
                    ) : (
                      <td
                        key={column}
                        className="max-w-xs border p-2 align-top break-words"
                      >
                        {cell}
                      </td>
                    ),
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {data && ['zip', 'xlsx'].includes(ext || '') && (
          <>
            {Array.isArray(data.sheets) && data.sheets.length > 0 && (
              <p className="text-xs">工作表：{data.sheets.join('、')}</p>
            )}
            <RecordView value={data.entries} />
          </>
        )}
        {item.kind === 'run' && data ? (
          <RunView value={data} />
        ) : (
          text && (
            <pre className="whitespace-pre-wrap break-all text-xs leading-5">
              {text}
            </pre>
          )
        )}
        {!geo &&
          !['image', 'pdf', 'text', 'json', 'run'].includes(item.kind) &&
          !['csv', 'tsv', 'xlsx', 'zip'].includes(ext || '') && (
            <div className="text-xs leading-6">
              <p>
                路径：<span className="break-all">{item.path}</span>
              </p>
              <p>
                修改时间：{new Date(item.modified_at).toLocaleString('zh-CN')}
              </p>
              {item.members?.length > 0 && <RecordView value={item.members} />}
            </div>
          )}
      </div>
    </div>
  )
}
