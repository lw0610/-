'use client'
import { useEffect, useRef, useState } from 'react'
import api from '@/lib/api'
import { flowApi } from '@/features/flows/api'
import type { FlowDetail, FlowSummary, InputProblem } from '@/features/flows/types'
import { useChatStore } from '@/stores/chatStore'
import { useConnectionStore } from '@/stores/connectionStore'
import { useInstanceStore } from '@/stores/instanceStore'
import { useSessionStore } from '@/stores/sessionStore'
import { inputDefaults, prepareFlowSubmission } from './model'
import type { FlowSubmission } from './types'

export function useFlowComposer(onSend: (content: string, flow?: FlowSubmission) => Promise<boolean | void>) {
  const activeScheme = useChatStore(state => state.activeAgentConfig)
  const executionMode = useChatStore(state => state.executionMode)
  const scheme = activeScheme || 'default'
  const ready = useConnectionStore(state => state.backendReady)
  const sid = useSessionStore(state => state.activeSessionId)
  const runtime = useChatStore(state => sid ? state.sessionRuntimeStates[sid] : undefined)
  const activeInstance = useInstanceStore(state => state.activeInstanceId)
  const instanceId = runtime?.bound_instance_id || activeInstance
  const [flows, setFlows] = useState<FlowSummary[]>([])
  const [id, setId] = useState(''), [detail, setDetail] = useState<FlowDetail | null>(null)
  const [loading, setLoading] = useState(false), [error, setError] = useState('')
  const [values, setValues] = useState<Record<string, unknown>>({})
  // A different input contract or destination invalidates its pending check.
  const inputContract = JSON.stringify([detail?.plan?.compile_key, detail?.plan?.inputs, detail?.plan?.input_constraints])
  const submissionState = useRef({ id, scheme, values, sid, instanceId, activeInstance, executionMode, inputContract })
  const previous = submissionState.current
  if (previous.id !== id || previous.scheme !== scheme || previous.values !== values || previous.sid !== sid || previous.instanceId !== instanceId || previous.activeInstance !== activeInstance || previous.executionMode !== executionMode || previous.inputContract !== inputContract) {
    submissionState.current = { id, scheme, values, sid, instanceId, activeInstance, executionMode, inputContract }
  }
  const [inputProblems, setInputProblems] = useState<InputProblem[]>([])
  const [checking, setChecking] = useState(false), [memoryError, setMemoryError] = useState('')
  const [layersKnown, setLayersKnown] = useState(false)
  const touched = useRef(new Set<string>())
  const [errors, setErrors] = useState<string[]>([]), [refresh, setRefresh] = useState(0)
  const [layers, setLayers] = useState<{ id: string; name: string }[]>([]), [layerError, setLayerError] = useState('')
  const previousScheme = useRef(scheme)
  const initialSchemeResolved = useRef(Boolean(activeScheme))
  useEffect(() => {
    const flow = new URLSearchParams(window.location.search).get('flow')
    if (flow) setId(flow)
    const changed = () => setRefresh(value => value + 1)
    window.addEventListener('autocarto:skill_catalog_changed', changed)
    return () => window.removeEventListener('autocarto:skill_catalog_changed', changed)
  }, [])
  useEffect(() => {
    if (previousScheme.current !== scheme) {
      if (initialSchemeResolved.current) setId('')
      previousScheme.current = scheme
    }
    if (activeScheme) initialSchemeResolved.current = true
    if (!ready) return
    let current = true
    void flowApi.list(scheme).then(items => {
      if (current) setFlows(items.filter(item => item.enabled))
    }).catch(() => { if (current) setError('流程列表读取失败；自动模式仍可使用普通对话。') })
    return () => { current = false }
  }, [ready, scheme, activeScheme, refresh])
  useEffect(() => {
    setDetail(null); setValues({}); setErrors([]); setError(''); setInputProblems([]); setMemoryError(''); setChecking(false); touched.current.clear()
    if (!id || !ready) { setLoading(false); return }
    let current = true
    setLoading(true)
    void flowApi.get(id, scheme).then(result => {
      if (current) {
        setDetail(result); setValues(inputDefaults(result.plan?.inputs || []))
        void flowApi.inputMemory(id).then(memory => {
          if (!current) return
          const remembered = Object.fromEntries(Object.entries(memory).filter(([key]) => result.plan?.inputs.some(input => input.key === key && input.remember && ['file', 'folder'].includes(input.type)) && !touched.current.has(key)))
          setValues(values => ({ ...values, ...remembered }))
        }).catch(() => { if (current) setMemoryError('上次选择读取失败，请本次重新选择文件或文件夹。') })
      }
    }).catch(reason => { if (current) setError(reason instanceof Error ? reason.message : '流程输入读取失败') })
      .finally(() => { if (current) setLoading(false) })
    return () => { current = false }
  }, [id, ready, scheme, refresh])
  const needsLayers = Boolean(detail?.plan?.inputs.some(input => input.type === 'layer'))
  useEffect(() => {
    setLayers([]); setLayerError(''); setLayersKnown(false)
    if (!needsLayers) return
    if (!instanceId) { setLayerError('未连接项目，可手工填写图层名或 ID。'); return }
    let current = true
    void api.get<{ items: { id: string; name: string }[]; instance_id?: string }>(`/api/instances/${encodeURIComponent(instanceId)}/layers`)
      .then(result => {
        if (!Array.isArray(result.items) || result.instance_id && result.instance_id !== instanceId) throw new Error('图层目录与绑定实例不符')
        if (current) {
          setLayersKnown(true)
          setLayers(result.items.filter(item => item && typeof item.id === 'string' && typeof item.name === 'string'))
          if (!result.items.length) setLayerError('当前工程暂无图层，请先添加所需图层。')
        }
      })
      .catch(() => { if (current) setLayerError('当前工程图层列表读取失败，可手工填写名称或 ID。') })
    return () => { current = false }
  }, [instanceId, needsLayers])
  useEffect(() => {
    if (!id || !detail?.plan) { setInputProblems([]); setChecking(false); return }
    let current = true
    setChecking(true)
    const timer = setTimeout(() => {
      const prepared = prepareFlowSubmission(id, detail.plan!.inputs, values)
      void flowApi.checkInputs(id, scheme, (prepared.flow.inputs || {})).then(problems => {
        if (current) setInputProblems(problems)
      }).catch(() => {
        if (current) setInputProblems([{ label: '', level: 'error', code: 'INPUT_CHECK_UNAVAILABLE', message: '输入校验暂不可用，请稍后重试。' }])
      }).finally(() => { if (current) setChecking(false) })
    }, 400)
    return () => { current = false; clearTimeout(timer) }
  }, [id, scheme, detail?.plan, values])
  const layerProblems = layersKnown ? (detail?.plan?.inputs || []).filter(input => input.type === 'layer' && values[input.key] && !layers.some(layer => layer.id === values[input.key] || layer.name === values[input.key])).map(input => ({ label: input.label, level: 'error', code: 'INPUT_LAYER_MISSING', message: '当前工程中未找到所选图层，请重新选择。' })) : []
  const problems = [...inputProblems, ...layerProblems]
  const unavailable = Boolean(id && !flows.some(flow => flow.id === id))
  const blocked = Boolean(id && (loading || error || unavailable || checking || problems.some(item => item.level === 'error') || !detail?.plan || detail.diagnostics.some(item => item.level === 'error')))
  const send = async (content: string) => {
    if (!id) return onSend(content)
    if (blocked || !detail?.plan) { setErrors(['当前流程尚不可用，请检查输入或切换为自动。']); return false }
    const prepared = prepareFlowSubmission(id, detail.plan.inputs, values)
    setErrors(prepared.errors)
    if (prepared.errors.length) return false
    const checkValues = { ...prepared.flow.inputs }
    const submission = submissionState.current
    for (const input of detail.plan.inputs) if (input.type === 'conversation_text') checkValues[input.key] = content
    try {
      const checked = await flowApi.checkInputs(id, scheme, checkValues)
      if (submission !== submissionState.current) return false
      setInputProblems(checked)
      if (checked.some(item => item.level === 'error')) return false
    } catch { if (submission === submissionState.current) setErrors(['输入校验失败，请重试。']); return false }
    return onSend(content, prepared.flow)
  }
  return { flows, id, detail, loading, error, unavailable, blocked, values, errors, layers, layerError, inputProblems: problems, checking, memoryError,
    retry: () => setRefresh(value => value + 1),
    select: setId, setValue: (key: string, value: unknown) => { touched.current.add(key); setValues(current => ({ ...current, [key]: value })) }, send }
}
