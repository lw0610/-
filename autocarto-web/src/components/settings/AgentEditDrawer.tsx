"use client";

import { useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { AgentDefData, PlanAgentDefData, SkillPackageSummary, toolPackageSelections } from "@/lib/api";
import ActionBar, { ActionButton } from "@/components/settings/ActionBar";
import AlertPanel from "@/components/settings/AlertPanel";
import InfoTooltip from "@/components/settings/InfoTooltip";
import StatusBadge from "@/components/settings/StatusBadge";
import {
  CheckboxField,
  Field,
  SelectInput,
  TextAreaInput,
  TextInput,
} from "@/components/settings/FormField";
import {
  EditorSection,
} from "@/components/settings/shared/AgentFormParts";
import ToolPackageSelector from "./ToolPackageSelector";
import AgentRoleTypeFields from "./AgentRoleTypeFields";
import { confirmDiscardChanges } from "@/hooks/useUnsavedChangesGuard";
import {
  normalizeRuntimeAgent, normalizePlanAgent,
} from "@/app/settings/agents/_components/normalizers";

interface AgentEditDrawerProps {
  open: boolean;
  mode: "runtime" | "plan";
  agentName: string;
  agent: AgentDefData | PlanAgentDefData | null;
  toolPackages: SkillPackageSummary[];
  modelOptions: Array<{ value: string; label: string }>;
  onClose: () => void;
  onSave: (agent: AgentDefData | PlanAgentDefData) => Promise<void> | void;
  onDirtyChange?: (dirty: boolean) => void;
}

function cloneAgent<T extends AgentDefData | PlanAgentDefData>(agent: T): T {
  return JSON.parse(JSON.stringify(agent)) as T;
}

const ensureRuntimeAgent = normalizeRuntimeAgent;
const ensurePlanAgent = normalizePlanAgent;

export default function AgentEditDrawer({
  open,
  mode,
  agentName,
  agent,
  toolPackages,
  modelOptions,
  onClose,
  onSave,
  onDirtyChange,
}: AgentEditDrawerProps) {
  const [draft, setDraft] = useState<AgentDefData | PlanAgentDefData | null>(
    null,
  );
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open || !agent) {
      setDraft(null);
      setSaving(false);
      setError("");
      return;
    }
    setDraft(cloneAgent(agent));
    setSaving(false);
    setError("");
  }, [agent, open]);

  const runtimeDraft = useMemo(() => {
    if (mode !== "runtime" || !draft) {
      return null;
    }
    return ensureRuntimeAgent(draft as AgentDefData);
  }, [draft, mode]);

  const planDraft = useMemo(() => {
    if (mode !== "plan" || !draft) {
      return null;
    }
    return ensurePlanAgent(draft as PlanAgentDefData);
  }, [draft, mode]);

  const normalizedInitial = useMemo(() => {
    if (!agent) {
      return "";
    }
    return JSON.stringify(
      mode === "runtime"
        ? ensureRuntimeAgent(cloneAgent(agent as AgentDefData))
        : ensurePlanAgent(cloneAgent(agent as PlanAgentDefData)),
    );
  }, [agent, mode]);

  const normalizedDraft = useMemo(() => {
    if (!draft) {
      return "";
    }
    return JSON.stringify(
      mode === "runtime"
        ? ensureRuntimeAgent(draft as AgentDefData)
        : ensurePlanAgent(draft as PlanAgentDefData),
    );
  }, [draft, mode]);

  const isDirty = Boolean(
    normalizedDraft && normalizedInitial !== normalizedDraft,
  );

  useEffect(() => {
    onDirtyChange?.(open ? isDirty : false);
  }, [isDirty, onDirtyChange, open]);

  const updateRuntimeDraft = (
    updater: (current: AgentDefData) => AgentDefData,
  ) => {
    setDraft((prev) => {
      if (!prev) {
        return prev;
      }
      return updater(ensureRuntimeAgent(prev as AgentDefData));
    });
  };

  const updatePlanDraft = (
    updater: (current: PlanAgentDefData) => PlanAgentDefData,
  ) => {
    setDraft((prev) => {
      if (!prev) {
        return prev;
      }
      return updater(ensurePlanAgent(prev as PlanAgentDefData));
    });
  };

  if (!open || !draft || typeof document === "undefined") {
    return null;
  }

  const drawerTitle = mode === "runtime" ? "Runtime 智能体" : "Plan 智能体";

  const requestClose = () => {
    if (saving) {
      return;
    }
    if (
      isDirty &&
      !confirmDiscardChanges("当前智能体有未保存修改，确定关闭吗？")
    ) {
      return;
    }
    onClose();
  };

  return createPortal(
    <div className="fixed inset-0 z-[80]">
      <div
        className="absolute inset-0 bg-black/35"
        onClick={() => {
          requestClose();
        }}
      />
      <div role="dialog" aria-modal="true" aria-label={`编辑角色 ${draft.display_name || agentName}`} className="absolute inset-y-0 right-0 w-full max-w-[1360px] overflow-auto border-l border-zinc-200 bg-zinc-50 p-3 shadow-[0_2px_8px_rgba(0,0,0,0.08)] dark:border-zinc-800 dark:bg-zinc-950 lg:p-3">
        <div className="space-y-3">
          <div className="rounded border border-zinc-200 bg-white px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-900">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="flex items-center gap-1.5">
                  <div className="text-[11px] font-semibold text-zinc-900 dark:text-zinc-100">
                    编辑 {drawerTitle}
                  </div>
                  <InfoTooltip
                    content={
                      mode === "runtime"
                        ? "调整 Runtime 智能体的执行能力、工具包范围与提示词。"
                        : "调整 Plan 智能体的工具包范围、提示词与独立模型覆盖。"
                    }
                  />
                </div>
                <div className="mt-0.5 break-all text-[11px] leading-4 text-zinc-500 dark:text-zinc-400">
                  {agentName}
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-2 text-[11px]">
                <StatusBadge>
                  类型：{mode === "runtime" ? "Runtime" : "Plan"}
                </StatusBadge>
                {mode === "runtime" && runtimeDraft && (
                  <StatusBadge
                    tone={runtimeDraft.enabled ? "success" : "neutral"}
                  >
                    状态：{runtimeDraft.enabled ? "启用" : "停用"}
                  </StatusBadge>
                )}
                {Boolean(draft.is_main) && (
                  <StatusBadge tone="accent">
                    {mode === "runtime" ? "Runtime 主控" : "Plan 主规划"}
                  </StatusBadge>
                )}
              </div>
            </div>
          </div>

          <ActionBar
            dirty={isDirty}
            left={
              <>
                <ActionButton onClick={requestClose} disabled={saving}>
                  关闭
                </ActionButton>
                <InfoTooltip
                  content={
                    mode === "runtime"
                      ? "调整 Runtime 智能体的执行能力、工具包范围与提示词。"
                      : "调整 Plan 智能体的工具包范围、提示词与独立模型覆盖。"
                  }
                />
              </>
            }
            right={
              <ActionButton
                variant="primary"
                onClick={() => {
                  setSaving(true);
                  setError("");
                  const nextDraft =
                    mode === "runtime"
                      ? ensureRuntimeAgent(draft as AgentDefData)
                      : ensurePlanAgent(draft as PlanAgentDefData);
                  void Promise.resolve(onSave(nextDraft))
                    .then(() => {
                      onClose();
                    })
                    .catch((saveError) => {
                      setError(
                        saveError instanceof Error
                          ? saveError.message
                          : "保存失败",
                      );
                    })
                    .finally(() => setSaving(false));
                }}
                disabled={saving}
              >
                {saving ? "保存中..." : "保存"}
              </ActionButton>
            }
          />

          {error && (
            <AlertPanel tone="error" title="保存失败" message={error} />
          )}

          <div className="grid gap-3">
            <div className="space-y-4">
              <EditorSection
                title="基础信息"
                description={
                  mode === "runtime"
                    ? "设置 Runtime 智能体的名称、启用状态与独立模型覆盖。"
                    : "设置 Plan 智能体名称与独立模型覆盖。"
                }
              >
                {mode === "runtime" && runtimeDraft && <AgentRoleTypeFields agent={runtimeDraft} onChange={(next) => setDraft(next)} />}
                <div className="mt-3 grid gap-3 md:grid-cols-2">
                  <Field label="显示名称">
                    <TextInput
                      value={
                        mode === "runtime"
                          ? runtimeDraft?.display_name || ""
                          : planDraft?.display_name || ""
                      }
                      onChange={(event) => {
                        const value = event.target.value;
                        if (mode === "runtime") {
                          updateRuntimeDraft((current) => ({
                            ...current,
                            display_name: value,
                          }));
                          return;
                        }
                        updatePlanDraft((current) => ({
                          ...current,
                          display_name: value,
                        }));
                      }}
                    />
                  </Field>

                  <Field label="独立模型（可选）">
                    <SelectInput
                      value={
                        mode === "runtime"
                          ? runtimeDraft?.llm?.provider || ""
                          : planDraft?.llm?.provider || ""
                      }
                      onChange={(event) => {
                        const value = event.target.value;
                        if (mode === "runtime") {
                          updateRuntimeDraft((current) => ({
                            ...current,
                            llm: { ...current.llm, provider: value },
                          }));
                          return;
                        }
                          updatePlanDraft((current) => ({
                            ...current,
                            llm: { ...current.llm, provider: value },
                          }));
                        }}
                    >
                      <option value="">继承全局模型</option>
                      {modelOptions.map((option) => (
                        <option key={option.value} value={option.value}>
                          {option.label}
                        </option>
                      ))}
                    </SelectInput>
                  </Field>

                  {mode === "runtime" && runtimeDraft && (
                    <>
                      <Field label="职责摘要">
                        <TextAreaInput
                          value={runtimeDraft.description || ""}
                          onChange={(event) => {
                            const value = event.target.value;
                            updateRuntimeDraft((current) => ({
                              ...current,
                              description: value,
                            }));
                          }}
                        />
                      </Field>
                      <div className="flex items-end">
                        <CheckboxField
                          className="w-full"
                          label="启用智能体"
                          description="关闭后保留配置，但不参与执行。"
                          checked={Boolean(runtimeDraft.enabled)}
                          onChange={(next) => {
                            updateRuntimeDraft((current) => ({
                              ...current,
                              enabled: next,
                            }));
                          }}
                        />
                      </div>
                    </>
                  )}
                </div>
              </EditorSection>

              {(mode === "plan" || runtimeDraft?.kind !== "executor") && <EditorSection title="工具包" description="选择当前角色可用的工具包；系统基础包自动挂载。">
                <ToolPackageSelector packages={toolPackages} value={toolPackageSelections(mode === "runtime" ? runtimeDraft : planDraft)} onChange={(next) => {
                  if (mode === "runtime") updateRuntimeDraft((current) => ({...current,tool_packages:next}));
                  else updatePlanDraft((current) => ({...current,tool_packages:next}));
                }} />
              </EditorSection>}
              {(draft.legacy_unresolved_packages || []).length > 0 && <AlertPanel tone="warning" title="旧配置中有未识别的工具包" message={(draft.legacy_unresolved_packages || []).join("、")} />}

              <EditorSection
                title="职责说明与高级设置"
                description={mode === "runtime" && runtimeDraft?.kind === "executor" ? "专用执行器使用流程步骤中的规则；职责说明保留为备用规则。" : "定义静态提示词、用户输入模板，以及 Plan 模式补充指令。"}
              >
                <div className="space-y-3">
                  <Field label="职责说明">
                    <TextAreaInput
                      className="min-h-[180px] text-xs"
                      value={String(draft.prompts?.static_instructions || "")}
                      onChange={(event) => {
                        const value = event.target.value;
                        setDraft((prev) => {
                          if (!prev) {
                            return prev;
                          }
                          return {
                            ...prev,
                            prompts: {
                              ...(prev.prompts || {}),
                              static_instructions: value,
                            },
                          } as AgentDefData | PlanAgentDefData;
                        });
                      }}
                    />
                  </Field>

                  {mode === "plan" && (
                    <Field label="运行模式提示词">
                      <TextAreaInput
                        className="min-h-[140px] text-xs"
                        value={String(
                          planDraft?.prompts?.runtime_mode_instruction || "",
                        )}
                        onChange={(event) => {
                          const value = event.target.value;
                          updatePlanDraft((current) => ({
                            ...current,
                            prompts: {
                              ...(current.prompts || {}),
                              runtime_mode_instruction: value,
                            },
                          }));
                        }}
                      />
                    </Field>
                  )}

                  {mode === "runtime" && runtimeDraft?.kind === "executor" ? <p className="text-xs text-zinc-500">专用执行器接收流程步骤提供的输入，保留原有高级字段。</p> : <Field label="用户输入模板">
                    <TextInput
                      value={String(
                        draft.prompts?.user_prompt_template ||
                          "{{ user_input }}",
                      )}
                      onChange={(event) => {
                        const value = event.target.value;
                        setDraft((prev) => {
                          if (!prev) {
                            return prev;
                          }
                          return {
                            ...prev,
                            prompts: {
                              ...(prev.prompts || {}),
                              user_prompt_template: value,
                            },
                          } as AgentDefData | PlanAgentDefData;
                        });
                      }}
                    />
                  </Field>}
                </div>
              </EditorSection>
            </div>
          </div>
        </div>
      </div>
    </div>,
    document.body,
  );
}
