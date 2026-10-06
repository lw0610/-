"use client";

import { AgentConfigMeta } from "@/lib/api";
import {
  CheckboxField,
  Field,
  SelectInput,
  TextInput,
} from "@/components/settings/FormField";
import InfoTooltip from "@/components/settings/InfoTooltip";
import StatusBadge from "@/components/settings/StatusBadge";

interface MetaConfigSectionProps {
  meta: AgentConfigMeta;
  saving: boolean;
  onChange: (meta: AgentConfigMeta) => void;
}

export default function MetaConfigSection({
  meta,
  saving,
  onChange,
}: MetaConfigSectionProps) {
  return (
    <div className="rounded border border-zinc-200/60 bg-zinc-50 px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-950/60 dark:shadow-zinc-950/30">
      <div className="mb-2 flex flex-wrap items-start justify-between gap-3">
        <div className="flex items-center gap-1.5">
          <div className="text-xs font-semibold uppercase tracking-[0.12em] text-zinc-700 dark:text-zinc-200">
            方案设置
          </div>
          <InfoTooltip content="控制默认运行模式、方案说明与 Plan 开关。" />
        </div>
        <div className="flex flex-wrap items-center gap-2 text-[11px]">
          <StatusBadge>
            运行模式：{meta.default_execution_mode.toUpperCase()}
          </StatusBadge>
          <StatusBadge tone={meta.plan_enabled ? "success" : "warning"}>
            Plan：{meta.plan_enabled ? "开启" : "关闭"}
          </StatusBadge>
        </div>
      </div>
      <div className="grid gap-2.5 md:grid-cols-2 xl:grid-cols-3">
        <Field label="默认运行模式">
          <SelectInput
            value={meta.default_execution_mode}
            disabled={saving}
            onChange={(event) =>
              onChange({
                ...meta,
                default_execution_mode: event.target.value as
                  | "plan"
                  | "build",
              })
            }
          >
            <option value="plan">优先规划</option>
            <option value="build">直接进入 Runtime</option>
          </SelectInput>
        </Field>

        <Field label="方案描述">
          <TextInput
            value={meta.description}
            disabled={saving}
            onChange={(event) => onChange({ ...meta, description: event.target.value })}
          />
        </Field>

        <div className="flex items-end">
          <CheckboxField
            className="w-full"
            label="启用 Plan"
            checked={Boolean(meta.plan_enabled)}
            disabled={saving}
            onChange={(next) => onChange({ ...meta, plan_enabled: next })}
          />
        </div>
      </div>
    </div>
  );
}
