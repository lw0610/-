"use client";

import { useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
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
import TagSelector, { TagOption } from "@/components/settings/TagSelector";
import {
  EditorSection,
} from "@/components/settings/shared/AgentFormParts";
import { confirmDiscardChanges } from "@/hooks/useUnsavedChangesGuard";
import { AgentDefData } from "@/lib/api";
import { collectSkillSelections } from "@/app/settings/agents/_components/normalizers";

interface AgentCreateDialogProps {
  open: boolean;
  skillOptions: TagOption[];
  modelOptions: Array<{ value: string; label: string }>;
  onClose: () => void;
  onSubmit: (agentName: string, agent: AgentDefData) => Promise<void> | void;
  onDirtyChange?: (dirty: boolean) => void;
}

const initialPrompts = {
  static_instructions: "",
  user_prompt_template: "{{ user_input }}",
};

export default function AgentCreateDialog({
  open,
  skillOptions,
  modelOptions,
  onClose,
  onSubmit,
  onDirtyChange,
}: AgentCreateDialogProps) {
  const [agentName, setAgentName] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [description, setDescription] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [llmProvider, setLLMProvider] = useState("");
  const [skillPackages, setSkillPackages] = useState<string[]>([]);
  const [staticInstructions, setStaticInstructions] = useState("");
  const [userPromptTemplate, setUserPromptTemplate] = useState(
    initialPrompts.user_prompt_template,
  );
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) {
      return;
    }
    setAgentName("");
    setDisplayName("");
    setDescription("");
    setEnabled(true);
    setLLMProvider("");
    setSkillPackages([]);
    setStaticInstructions("");
    setUserPromptTemplate(initialPrompts.user_prompt_template);
    setSubmitting(false);
    setError("");
  }, [open]);

  const isDirty = useMemo(() => {
    return Boolean(
      agentName.trim() ||
      displayName.trim() ||
      description.trim() ||
      !enabled ||
      llmProvider.trim() ||
      skillPackages.length ||
      staticInstructions.trim() ||
      userPromptTemplate !== initialPrompts.user_prompt_template,
    );
  }, [
    agentName,
    description,
    displayName,
    enabled,
    llmProvider,
    skillPackages,
    staticInstructions,
    userPromptTemplate,
  ]);

  useEffect(() => {
    onDirtyChange?.(open ? isDirty : false);
  }, [isDirty, onDirtyChange, open]);

  if (!open || typeof document === "undefined") {
    return null;
  }

  const requestClose = () => {
    if (submitting) {
      return;
    }
    if (
      isDirty &&
      !confirmDiscardChanges("当前新建智能体有未保存修改，确定关闭吗？")
    ) {
      return;
    }
    onClose();
  };

  return createPortal(
    <div
      className="fixed inset-0 z-[75] flex items-center justify-center bg-zinc-950/55 p-3 backdrop-blur-sm"
      onClick={() => {
        requestClose();
      }}
    >
      <div
        className="max-h-[92vh] w-full max-w-[1320px] overflow-auto rounded border border-zinc-200 bg-zinc-50 p-3 shadow-[0_2px_8px_rgba(0,0,0,0.08)] dark:border-zinc-800 dark:bg-zinc-950 dark:shadow-black/30 lg:p-3"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="space-y-3">
          <div className="rounded border border-zinc-200 bg-white px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-900">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="min-w-0 flex items-center gap-1.5">
                <div className="text-[11px] font-semibold text-zinc-900 dark:text-zinc-100">
                  新建 Runtime 智能体
                </div>
                <InfoTooltip content="使用统一的运维式配置面板创建执行智能体。" />
              </div>
              <div className="flex flex-wrap items-center gap-2 text-[11px]">
                <StatusBadge tone="accent">类型：Runtime</StatusBadge>
                <StatusBadge tone={enabled ? "success" : "neutral"}>
                  状态：{enabled ? "启用" : "停用"}
                </StatusBadge>
              </div>
            </div>
          </div>

          <ActionBar
            dirty={isDirty}
            left={
              <>
                <ActionButton onClick={requestClose} disabled={submitting}>
                  关闭
                </ActionButton>
                <InfoTooltip content="配置基础信息、技能范围与提示词。" />
              </>
            }
            right={
              <ActionButton
                variant="primary"
                onClick={() => {
                  const key = agentName.trim();
                  if (!key) {
                    setError("智能体标识不能为空");
                    return;
                  }

                  const agent: AgentDefData = {
                    display_name: displayName.trim() || key,
                    description: description.trim(),
                    enabled,
                    llm: {
                      provider: llmProvider.trim(),
                    },
                    skill_packages: collectSkillSelections(skillPackages),
                    prompts: {
                      ...initialPrompts,
                      static_instructions: staticInstructions,
                      user_prompt_template: userPromptTemplate,
                    },
                  };

                  setSubmitting(true);
                  setError("");
                  void Promise.resolve(onSubmit(key, agent))
                    .then(() => {
                      onClose();
                    })
                    .catch((submitError) => {
                      setError(
                        submitError instanceof Error
                          ? submitError.message
                          : "创建失败",
                      );
                    })
                    .finally(() => setSubmitting(false));
                }}
                disabled={submitting}
              >
                {submitting ? "创建中..." : "创建"}
              </ActionButton>
            }
          />

          {error && (
            <AlertPanel tone="error" title="创建失败" message={error} />
          )}

          <div className="space-y-3">
              <EditorSection
                title="基础信息"
                description="设置标识、显示名称、启用状态和模型覆盖。"
              >
                <div className="grid gap-3 md:grid-cols-2">
                  <Field label="智能体标识 (key)">
                    <TextInput
                      value={agentName}
                      onChange={(event) => setAgentName(event.target.value)}
                      placeholder="例如：vector_helper"
                    />
                  </Field>
                  <Field label="显示名称">
                    <TextInput
                      value={displayName}
                      onChange={(event) => setDisplayName(event.target.value)}
                      placeholder="例如：矢量助手"
                    />
                  </Field>
                  <Field label="描述">
                    <TextInput
                      value={description}
                      onChange={(event) => setDescription(event.target.value)}
                        placeholder="一句话描述当前智能体职责"
                      />
                  </Field>
                  <Field label="独立模型（可选）">
                    <SelectInput
                      value={llmProvider}
                      onChange={(event) => setLLMProvider(event.target.value)}
                    >
                      <option value="">继承全局模型</option>
                      {modelOptions.map((option) => (
                        <option key={option.value} value={option.value}>
                          {option.label}
                        </option>
                      ))}
                    </SelectInput>
                  </Field>
                  <div className="md:col-span-2">
                    <CheckboxField
                      label="启用该智能体"
                      description="关闭后仍会创建配置，但默认不参与执行。"
                      checked={enabled}
                      onChange={setEnabled}
                    />
                  </div>
                </div>
              </EditorSection>

              <EditorSection
                title="技能包"
                description="为当前智能体配置专属技能包；全局技能包在方案层统一维护。"
              >
                <div className="space-y-3">
                  <div>
                    <div className="mb-1 flex items-center gap-1 text-xs text-zinc-700 dark:text-zinc-200">
                      <span>智能体技能包</span>
                      <InfoTooltip content="只对当前智能体生效的技能包列表。" />
                    </div>
                    <TagSelector
                      options={skillOptions}
                      value={skillPackages}
                      onChange={setSkillPackages}
                    />
                  </div>
                </div>
              </EditorSection>

              <EditorSection
                title="提示词与模板"
                description="静态提示词决定角色行为，输入模板定义用户请求的注入方式。"
              >
                <div className="space-y-3">
                  <Field label="系统提示词">
                    <TextAreaInput
                      className="min-h-[140px] text-xs"
                      value={staticInstructions}
                      onChange={(event) =>
                        setStaticInstructions(event.target.value)
                      }
                    />
                  </Field>
                  <Field label="用户输入模板">
                    <TextInput
                      value={userPromptTemplate}
                      onChange={(event) =>
                        setUserPromptTemplate(event.target.value)
                      }
                    />
                  </Field>
                </div>
              </EditorSection>
          </div>
        </div>
      </div>
    </div>,
    document.body,
  );
}
