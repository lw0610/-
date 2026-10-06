'use client'

import { useCallback, useEffect, useMemo, useRef, useState, type SetStateAction } from 'react'
import { ApiError, getApiErrorMessage } from '@/lib/api'
import { pickFilePath } from '@/lib/config-form-helpers'
import { confirmDiscardChanges, useUnsavedChangesGuard } from '@/hooks/useUnsavedChangesGuard'
import { useChatStore } from '@/stores/chatStore'
import { useSessionStore } from '@/stores/sessionStore'
import { reviewApi } from './api'
import { editable, isPendingReviewDiagnostic, record, reviewCounts, reviewKey, routeReferences, validateGraph, visibleReviewIssues, type KnowledgeGraph, type ReviewDocument, type ReviewIssue, type ReviewReference, type ReviewSummary } from './model'

export function useKnowledgeReview() {
  const sessionId = useSessionStore((state) => state.activeSessionId) || ''
  const [target, setTarget] = useState<ReviewReference | null>(null)
  const [document, setDocument] = useState<ReviewDocument | null>(null)
  const [draft, updateDraft] = useState<KnowledgeGraph | null>(null)
  const [open, setOpen] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [geocoding, setGeocoding] = useState(false)
  const [importing, setImporting] = useState(false)
  const [importWarnings, setImportWarnings] = useState<string[]>([])
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [issues, setIssues] = useState<ReviewIssue[]>([])
  const [summaries, setSummaries] = useState<Record<string, ReviewSummary>>({})
  const requestSequence = useRef(0)
  const mutationInFlight = useRef(false)
  const importRequest = useRef<AbortController | null>(null)
  const lifecycle = useRef({ active: true })
  useEffect(() => {
    const current = { active: true }
    lifecycle.current = current
    setImporting(false)
    return () => {
      current.active = false
      if (importRequest.current) {
        importRequest.current.abort()
        importRequest.current = null
        mutationInFlight.current = false
      }
    }
  }, [sessionId])
  const setDraft = useCallback((next: SetStateAction<KnowledgeGraph | null>) => {
    if (!mutationInFlight.current) updateDraft(next)
  }, [])
  const confirmRequest = useRef<{ key: string; id: string } | null>(null)
  const dirty = useMemo(() => Boolean(document && draft && JSON.stringify(document.graph) !== JSON.stringify(draft)), [document, draft])
  useUnsavedChangesGuard(dirty || geocoding || importing)

  const apply = useCallback((next: ReviewDocument, ref: ReviewReference) => {
    if (!next.graph || !Number.isInteger(next.revision) || next.review_id !== ref.review_id || next.session_id !== ref.session_id) throw new Error('后端返回了不匹配的知识草稿，已停止更新。')
    setDocument(next)
    updateDraft(structuredClone(next.graph))
    setIssues(visibleReviewIssues(next.errors))
    setSummaries((prev) => ({ ...prev, [reviewKey(ref)]: { revision: next.revision, status: next.status, counts: reviewCounts(next.graph) } }))
  }, [])
  const report = useCallback((failure: unknown) => {
    const data = failure instanceof ApiError ? record(failure.data) : {}
    const nextIssues = Array.isArray(data.errors) ? data.errors as ReviewIssue[] : []
    const visibleIssues = visibleReviewIssues(nextIssues)
    const legacyRejection = nextIssues.some(isPendingReviewDiagnostic) || (failure instanceof ApiError && isPendingReviewDiagnostic({ code: failure.code, message: '' }))
    setIssues(visibleIssues)
    setError(failure instanceof ApiError && failure.status === 409
      ? '草稿版本已变化。当前修改仍保留；请关闭并重新打开最新版本，不会自动覆盖。'
      : legacyRejection
        ? visibleIssues[0]?.message || '当前服务仍在使用旧版核验规则，请重启应用后重试制图；无需重新抽取。'
        : getApiErrorMessage(failure, '知识结果操作失败，请重试。'))
  }, [])
  const registerAsset = useCallback((asset: { image_path: string; preview_url: string }) => {
    setDocument((current) => current ? {
      ...current,
      assets: [...(Array.isArray(current.assets) ? current.assets : []).filter((entry) => record(entry).image_path !== asset.image_path), asset],
    } : current)
  }, [])
  const show = useCallback(async (ref: ReviewReference, confirm = false) => {
    if (mutationInFlight.current) return
    const normalized = { ...ref, session_id: ref.session_id || sessionId }
    if (!normalized.session_id) { setError('缺少所属会话，无法读取草稿。'); return }
    if (open && target && reviewKey(target) === reviewKey(normalized) && draft) {
      if (confirm) {
        if (dirty) { setNotice('请先保存修改，再开始制图。'); return }
        setConfirmOpen(true)
      }
      return
    }
    if (dirty && !confirmDiscardChanges()) return
    const sequence = ++requestSequence.current
    setTarget(normalized); setOpen(!confirm); setConfirmOpen(confirm)
    setDocument(null); updateDraft(null); setLoading(true); setError(''); setNotice(''); setIssues([]); setImportWarnings([])
    try {
      const next = await reviewApi.get(normalized)
      if (sequence !== requestSequence.current) return
      apply(next, normalized)
      if (confirm && !editable(next.status)) {
        setConfirmOpen(false); setOpen(true)
        setNotice('该版本已提交或处理完成，当前仅供查看。')
      }
    } catch (failure) { if (sequence === requestSequence.current) report(failure) }
    finally { if (sequence === requestSequence.current) setLoading(false) }
  }, [apply, dirty, draft, open, report, sessionId, target])
  const close = useCallback(() => {
    if (mutationInFlight.current || (dirty && !confirmDiscardChanges())) return
    ++requestSequence.current
    setOpen(false); setConfirmOpen(false); setTarget(null); setDocument(null); updateDraft(null); setLoading(false); setError('')
  }, [dirty])
  const save = useCallback(async () => {
    if (!target || !document || !draft || mutationInFlight.current || !editable(document.status)) return
    const problems = validateGraph(draft)
    mutationInFlight.current = true; setSaving(true); setError(''); setNotice('')
    try {
      const next = await reviewApi.save(target, document.revision, draft)
      apply(next, target)
      if (problems.length) setIssues([...visibleReviewIssues(next.errors), ...problems])
      setNotice('修改已保存，开始制图时由后台自动适配当前结果。')
    } catch (failure) { report(failure) }
    finally { mutationInFlight.current = false; setSaving(false) }
  }, [apply, document, draft, report, target])
  const geocode = useCallback(async () => {
    if (!target || !document || !draft || loading || mutationInFlight.current || !editable(document.status)) return
    if (dirty) { setNotice('请先保存修改，再自动补全坐标；当前修改已保留。'); return }
    mutationInFlight.current = true; setGeocoding(true); setError(''); setNotice('')
    try {
      const next = await reviewApi.geocode(target, document.revision)
      apply(next, target)
      setNotice(`${routeReferences(next.graph).length > 0 ? '地点及路线参照坐标' : '地点坐标'}已自动补全并保存到知识草稿（版本 ${next.revision}）。`)
    } catch (failure) { report(failure) }
    finally { mutationInFlight.current = false; setGeocoding(false) }
  }, [apply, dirty, document, draft, loading, report, target])
  const importCallouts = useCallback(async () => {
    if (!target || target.session_id !== sessionId || !document || !draft || loading || mutationInFlight.current || !editable(document.status)) return
    const current = lifecycle.current
    const controller = new AbortController()
    importRequest.current = controller
    const isCurrent = () => current.active && importRequest.current === controller && !controller.signal.aborted
    mutationInFlight.current = true; setImporting(true); setError(''); setNotice(''); setImportWarnings([])
    try {
      const sourcePath = await pickFilePath()
      if (!sourcePath || !isCurrent()) return
      if (!/\.xlsx$/i.test(sourcePath)) throw new Error('请选择 .xlsx 文件；旧版 .xls 请先另存为 .xlsx。')
      const result = await reviewApi.importCallouts(target, document.revision, sourcePath, draft, controller.signal)
      if (!isCurrent()) return
      apply(result.review, target)
      setImportWarnings(result.warnings || [])
      setNotice(`Excel 已导入并保存：新增 ${result.imported_count} 条，更新 ${result.updated_count} 条，导入 ${result.image_count} 张图片。${result.new_place_count ? `已补充 ${result.new_place_count} 个注记地点。` : ''}`)
    } catch (failure) { if (isCurrent()) report(failure) }
    finally {
      if (importRequest.current === controller) {
        importRequest.current = null
        mutationInFlight.current = false
        if (current.active) setImporting(false)
      }
    }
  }, [apply, document, draft, loading, report, sessionId, target])
  const confirm = useCallback(async () => {
    if (!target || !document || loading || dirty || mutationInFlight.current || !editable(document.status)) return
    const key = `${reviewKey(target)}/${document.revision}`
    if (confirmRequest.current?.key !== key) confirmRequest.current = { key, id: crypto.randomUUID() }
    mutationInFlight.current = true; setConfirming(true); setError(''); setNotice('')
    try {
      const next = await reviewApi.confirm(target, document.revision, confirmRequest.current.id)
      apply(next, target)
      if (editable(next.status)) { setError('后台尚未启动制图，当前结果已保留，请重试。'); return }
      // Resolve only after the server has accepted this exact revision. Never send a second WS approval.
      useChatStore.getState().resolveSessionInterrupt(target.session_id, target.checkpoint_id)
      setConfirmOpen(false); setOpen(false); setNotice('已提交制图，后台继续执行。')
    } catch (failure) { report(failure) }
    finally { mutationInFlight.current = false; setConfirming(false) }
  }, [apply, dirty, document, loading, report, target])
  return {
    sessionId, target, document, draft, setDraft, open, confirmOpen, setConfirmOpen,
    loading, saving, confirming, geocoding, importing, importWarnings, dirty, error, notice, issues, summaries,
    show, close, save, geocode, importCallouts, confirm, registerAsset,
  }
}
export type KnowledgeReviewController = ReturnType<typeof useKnowledgeReview>
