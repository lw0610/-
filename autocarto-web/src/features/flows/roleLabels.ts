import type { RoleResolution } from './types'
const reasons: Record<string, string> = {
  FLOW_ROLE_VISION_UNAVAILABLE: '需要配置支持读图的模型',
  FLOW_ROLE_NOT_FOUND: '没有可用的对应角色',
  FLOW_EXECUTOR_ROLE_MISSING: '没有可用的系统执行器',
  FLOW_ROLE_PACKAGE_AMBIGUOUS: '所需工具包名称不唯一',
  FLOW_CAPABILITY_MISSING_IN_ROLE: '角色缺少所需能力',
  FLOW_MODEL_VISION_REQUIRED: '需要配置支持读图的模型',
  FLOW_VISION_MODEL_MISSING: '需要配置支持读图的模型',
}
export function roleSourceLabel(
  value: RoleResolution,
  packages: Map<string, string> = new Map(),
) {
  if (value.reason) return `无法执行：${reasons[value.reason] || value.reason}`
  if (value.granted?.length)
    return `临时补齐：${value.granted.map((id) => packages.get(id) || id).join('、')}`
  return (
    (
      {
        mapping: '方案角色（已指定）',
        scheme_name: '方案角色',
        scheme_capability: '按工具包匹配',
        scheme_executor: '方案角色',
        flow_role: '流程自带',
        executor_default: '系统默认执行器',
        unresolved: '无法执行',
      } as Record<string, string>
    )[value.source] || '方案角色'
  )
}
