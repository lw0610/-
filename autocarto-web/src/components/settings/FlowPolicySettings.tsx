import type { FlowPolicyConfig } from '@/lib/api'
import { EditorSection } from './shared/AgentFormParts'
export default function FlowPolicySettings({
  value,
  onChange,
}: {
  value?: FlowPolicyConfig
  onChange: (next: FlowPolicyConfig) => void
}) {
  return (
    <EditorSection
      title="流程策略"
      description="策略应用于此方案的流程，角色对应与其他设置会保留。"
    >
      <div className="space-y-3 text-xs">
        <label className="block space-y-1">
          <span>流程缺少角色或能力时</span>
          <select
            className="block w-full rounded border bg-transparent p-2"
            value={value?.role_fill || 'auto'}
            onChange={(event) =>
              onChange({
                ...value,
                role_fill: event.target.value as FlowPolicyConfig['role_fill'],
              })
            }
          >
            <option value="auto">自动补齐（推荐）</option>
            <option value="flow_role">使用流程自带角色，不改方案角色</option>
            <option value="strict">不补齐，提示我修改方案</option>
          </select>
        </label>
        <label className="block space-y-1">
          <span>对话中流程缺少数据时</span>
          <select
            className="block w-full rounded border bg-transparent p-2"
            value={value?.auto_acquire || 'ask'}
            onChange={(event) =>
              onChange({
                ...value,
                auto_acquire: event.target
                  .value as FlowPolicyConfig['auto_acquire'],
              })
            }
          >
            <option value="ask">先问我</option>
            <option value="allow">自动获取</option>
            <option value="deny">不自动获取</option>
          </select>
        </label>
        {value?.auto_acquire === 'deny' && (
          <p className="text-zinc-500">表单中的自动获取和补齐也会关闭。</p>
        )}
      </div>
    </EditorSection>
  )
}
