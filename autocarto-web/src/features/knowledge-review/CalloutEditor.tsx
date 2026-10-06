'use client'

import { useEffect, useRef, useState } from 'react'
import { getApiErrorMessage } from '@/lib/api'
import { isDesktopRuntime, pickFilePath } from '@/lib/config-form-helpers'
import { assetPreviewURL, reviewApi } from './api'
import { Field, SelectField, TextField, type EditorProps } from './EntityEditors'
import { buttonClass, fieldClass, itemId, itemOptions, record, visibleItems, type ReviewDocument, type ReviewReference } from './model'

export default function CalloutEditor({ item, graph, onChange, disabled, reference, document, onBusyChange, onAssetImported }: EditorProps & {
  reference: ReviewReference; document: ReviewDocument; onBusyChange: (busy: boolean) => void
  onAssetImported: (asset: { image_path: string; preview_url: string }) => void
}) {
  const [importing, setImporting] = useState(false)
  const [error, setError] = useState('')
  const lifecycle = useRef({ active: true })
  useEffect(() => {
    const current = { active: true }
    lifecycle.current = current
    return () => { current.active = false }
  }, [reference.review_id, item.callout_id])
  const imagePath = String(item.image_path || '')
  const assets = Array.isArray(document.assets) ? document.assets : []
  const asset = assets.map(record).find((candidate) => candidate.image_path === imagePath)
  const previewURL = assetPreviewURL(String(asset?.preview_url || ''))
  const chooseImage = async () => {
    const current = lifecycle.current
    setImporting(true); onBusyChange(true); setError('')
    try {
      const source = await pickFilePath()
      if (!source || !current.active) return
      if (!/\.(png|jpe?g|gif)$/i.test(source)) throw new Error('请选择 PNG、JPEG 或 GIF 图片。')
      const result = await reviewApi.importAsset(reference, source)
      if (!current.active) return
      onAssetImported(result)
      onChange({ image_path: result.image_path, image_enabled: true, template: !item.template || item.template === 'text_only' ? 'image_top_text_bottom' : item.template })
    } catch (failure) { if (current.active) setError(getApiErrorMessage(failure, '图片导入失败')) }
    finally { if (current.active) setImporting(false); onBusyChange(false) }
  }
  const template = String(item.template || 'text_only')
  const imageEnabled = Boolean(item.image_enabled)
  return <fieldset
    disabled={disabled || importing}
    className="space-y-4"
  >

    <SelectField
      title="锚点类型"
      value={item.anchor_ref_type}
      options={[['event', '事件'], ['spatial_entity', '地点']]}
      onChange={(anchor_ref_type) => onChange({ anchor_ref_type, anchor_ref: '' })}
    />

    <SelectField
      title="关联锚点"
      value={item.anchor_ref}
      onChange={(anchor_ref) => onChange({ anchor_ref })}
      options={itemOptions(graph, item.anchor_ref_type === 'spatial_entity' ? 'spatial_entities' : 'events').filter(([id]) => item.anchor_ref_type !== 'spatial_entity' || visibleItems(graph, 'spatial_entities').some((place) => itemId(place, 'spatial_entities') === id))}
    />

    <TextField
      title="注记正文（纯文本）"
      value={item.body}
      onChange={(body) => onChange({ body })}
      multiline
    />

    <div className="grid grid-cols-2 gap-4">

      <SelectField
        title="排版模板"
        value={item.template || 'text_only'}
        onChange={(template) => onChange({ template })}
        options={[['text_only', '纯文字'], ['image_top_text_bottom', '上图下文'], ['image_left_text_right', '左图右文'], ['image_only_ellipse', '椭圆图片'], ['ellipse_badge', '椭圆徽章']]}
      />

      <SelectField
        title="引线样式"
        value={item.leader_style || 'double'}
        onChange={(leader_style) => onChange({ leader_style })}
        options={[['double', '双线'], ['straight', '直线'], ['curved', '曲线']]}
      />

    </div>

    <div className="space-y-2 rounded border border-zinc-200 p-3 dark:border-zinc-700">

      <label className="flex items-center gap-2 text-xs"><input
        type="checkbox"
        checked={imageEnabled}
        onChange={(event) => onChange({ image_enabled: event.target.checked, template: event.target.checked ? (item.template === 'text_only' ? 'image_top_text_bottom' : item.template || 'image_top_text_bottom') : 'text_only' })}
      />使用图片</label>

      <p className="break-all text-[11px] text-zinc-500">
        {imagePath || '尚未选择图片'}
      </p>

      <div className="flex items-center gap-2">
        <button
          type="button"
          className={buttonClass}
          disabled={!isDesktopRuntime()}
          onClick={() => void chooseImage()}
        >
          {importing ? '正在导入…' : '选择本地图片'}
        </button>
        {imagePath && <button
          type="button"
          className={buttonClass}
          onClick={() => onChange({ image_path: '', image_enabled: false, template: 'text_only' })}
        >移除图片引用</button>}
      </div>

      <p className="text-[11px] text-zinc-500">图片将复制到项目素材目录；移除引用不会删除原文件。{!isDesktopRuntime() && '请在桌面端使用本地文件选择。'}</p>

      {error && <p
        role="alert"
        className="text-xs text-red-600"
      >
        {error}
      </p>}

    </div>

    <Field title="注记宽度（毫米）">
      <input
        className={fieldClass}
        type="number"
        min="1"
        value={String(item.width_mm ?? 42)}
        onChange={(event) => onChange({ width_mm: event.target.value === '' ? '' : Number(event.target.value) })}
      />
    </Field>

    <section
      className="rounded border border-dashed border-zinc-300 p-4 dark:border-zinc-700"
      aria-label="图文注记效果示意"
    >

      <h4 className="mb-3 text-xs font-medium">HTML 效果示意 · 最终版式以 QGIS 输出为准</h4>

      <div className={`mx-auto max-w-sm overflow-hidden border border-zinc-300 bg-white p-3 text-zinc-900 ${template === 'image_left_text_right' ? 'flex items-center gap-3' : ''} ${template.includes('ellipse') ? 'rounded-[50%] px-6 py-4 text-center' : 'rounded'}`}>

        {imageEnabled && template !== 'text_only' && (previewURL
          // eslint-disable-next-line @next/next/no-img-element
          ? <img
            src={previewURL}
            alt="注记图片"
            className={template === 'image_left_text_right' ? 'h-auto w-28 shrink-0 object-contain' : 'block h-auto w-full object-contain'}
          />
          : <div className="rounded bg-zinc-100 p-4 text-center text-xs text-zinc-500">
            {imagePath ? '图片已引用，暂无安全预览' : '未选择图片'}
          </div>)}

        {template !== 'image_only_ellipse' && <div className="min-w-0 break-words">
          <p className="mt-1 whitespace-pre-wrap text-xs leading-relaxed">
            {String(item.body || '注记正文')}
          </p>
        </div>}

      </div>

      <p className="mt-3 text-[10px] text-zinc-500">上图下文时图片与正文同宽，高度按原图比例调整。预览不模拟地图引线位置。</p>

    </section>

  </fieldset>
}
