export function resolvePlanUnavailableReason(
  planEnabled: boolean,
  planIssue?: string | null,
) {
  const trimmedPlanIssue = String(planIssue || '').trim()
  if (!planEnabled) {
    return '当前激活智能体配置未开启 Plan 模式。'
  }
  return trimmedPlanIssue
}
