'use client'
import PlanStatusBadge from '@/features/flows/PlanStatusBadge'
import { useState } from 'react'
import { isDesktopRuntime, pickDirectoryPath, pickFilePath } from '@/lib/config-form-helpers'
import type { ExecutionMode } from '@/types/message'
import type { useFlowComposer } from './useFlowComposer'
import { choiceValues } from './model'

const field = 'w-full rounded border border-zinc-300 bg-surface-raised px-2 py-1 text-xs dark:border-zinc-700'
export default function FlowComposer({ composer, disabled, mode }: {
  composer: ReturnType<typeof useFlowComposer>; disabled?: boolean; mode: ExecutionMode
}) {
  const [pathError, setPathError] = useState('')
  const browse = async (key: string, folder: boolean) => {
    try {
      const path = await (folder ? pickDirectoryPath() : pickFilePath())
      if (path) composer.setValue(key, path)
    } catch (error) { setPathError(error instanceof Error ? error.message : '选择路径失败') }
  }
  return <div className="mb-2 space-y-2 text-xs">
    <label className="flex items-center gap-2"><span className="shrink-0">流程</span>
      <select aria-label="选择流程" className={field} disabled={disabled} value={composer.id} onChange={event => composer.select(event.target.value)}>
        <option value="">自动</option>
        {composer.unavailable && <option value={composer.id}>{composer.id}（当前未启用或未找到）</option>}
        {composer.flows.map(flow => <option key={flow.id} value={flow.id}>{flow.name}</option>)}
      </select>
    </label>
    {composer.id && mode === 'plan' && <p className="text-amber-700">本次仅预览流程，切换到 Build 后可执行。</p>}
    {composer.loading && <p>正在读取流程…</p>}
    {composer.error && <p role="alert" className="text-amber-700">{composer.error} <button type="button" onClick={composer.retry}>重试</button></p>}
    {composer.unavailable && <p role="alert" className="text-amber-700">所选流程未在当前方案中启用，发送前请更换流程或选择自动。</p>}
    {composer.detail?.plan && <PlanStatusBadge plan={composer.detail.plan} diagnostics={composer.detail.diagnostics} />}
    {!!composer.detail && <div className="max-h-64 space-y-2 overflow-y-auto rounded border border-zinc-200 p-2 dark:border-zinc-700" aria-label="流程输入">
      {composer.detail.plan?.inputs.filter(input => input.type !== 'conversation_text').map(input => {
        const value = composer.values[input.key]
        const label = `${input.label}${input.required ? '（必填）' : ''}`
        return <div key={input.key}>
          {input.type === 'boolean' ? <label className="flex items-center gap-2"><input type="checkbox" disabled={disabled} checked={Boolean(value)} onChange={event => composer.setValue(input.key, event.target.checked)}/>{label}</label> : <label className="block space-y-1"><span>{label}</span>
            {input.type === 'choice' ? <select className={field} disabled={disabled} value={String(value ?? '')} onChange={event => composer.setValue(input.key, event.target.value)}><option value="">请选择</option>{choiceValues(input).map(option => <option key={option} value={option}>{option}</option>)}</select> : input.type === 'layer' && composer.layers.length ? <select className={field} disabled={disabled} value={String(value ?? '')} onChange={event => composer.setValue(input.key, event.target.value)}><option value="">请选择当前工程图层</option>{value && !composer.layers.some(layer => layer.id === String(value)) ? <option value={String(value)}>{String(value)}（当前输入）</option> : null}{composer.layers.map(layer => <option key={layer.id} value={layer.id}>{layer.name}</option>)}</select> : <input className={field} type={input.type === 'number' ? 'number' : 'text'} step={input.type === 'number' ? 'any' : undefined} disabled={disabled} value={String(value ?? '')} list={input.type === 'layer' ? `flow-layer-${input.key}` : undefined} onChange={event => composer.setValue(input.key, event.target.value)}/>}
          </label>}
          {input.type === 'layer' && !composer.layers.length && <datalist id={`flow-layer-${input.key}`}>{(input.options || []).map(option => <option key={option} value={option}/>)}</datalist>}
          {['file', 'folder'].includes(input.type) && isDesktopRuntime() && <button type="button" disabled={disabled} className="mt-1 text-blue-600" onClick={() => void browse(input.key, input.type === 'folder')}>选择{input.type === 'folder' ? '文件夹' : '文件'}</button>}
          {composer.inputProblems.filter(problem => problem.label === input.label || problem.label === input.key).map((problem, index) => <p key={index} role={problem.level === 'error' ? 'alert' : undefined} className={problem.level === 'error' ? 'mt-1 text-red-700' : 'mt-1 text-amber-700'}>{problem.message}</p>)}
          {input.description && <p className="mt-0.5 text-text-muted">{input.description}</p>}
        </div>
      })}
      {composer.detail.plan?.inputs.every(input => input.type === 'conversation_text') && <p className="text-text-muted">输入将自动使用本条对话原文。</p>}
      {composer.detail.diagnostics.filter(item => item.level === 'error').map((item, index) => <p key={index} role="alert" className="text-red-700">{item.message}</p>)}
    </div>}
    {composer.checking && <p className="text-text-muted">正在校验输入…</p>}
    {composer.inputProblems.filter(problem => !composer.detail?.plan?.inputs.some(input => input.key === problem.label || input.label === problem.label)).map((problem, index) => <p role="alert" key={index}>{problem.message}</p>)}
    {composer.memoryError && <p className="text-amber-700">{composer.memoryError}</p>}
    {composer.layerError && <p className="text-amber-700">{composer.layerError}</p>}
    {pathError && <p role="alert" className="text-amber-700">{pathError}</p>}
    {composer.errors.map(error => <p role="alert" key={error} className="text-red-700">{error}</p>)}
  </div>
}
