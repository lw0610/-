"use client";

import { useMemo, useState } from "react";
import { toast } from "sonner";
import ActionBar, { ActionButton } from "@/components/settings/ActionBar";
import InfoTooltip from "@/components/settings/InfoTooltip";
import StatusBadge from "@/components/settings/StatusBadge";
import { TextInput } from "@/components/settings/FormField";
import { useSettingsActionStatus } from "@/hooks/useSettingsActionStatus";
import { useSettingsForm } from "@/hooks/useSettingsForm";
import api, {
  type Image2ConfigPayload,
  type Image2ConfigSavePayload,
} from "@/lib/api";

const DEFAULT_IMAGE2_CONFIG: Image2ConfigPayload = {
  enabled: true,
  model: "gpt-image-2",
  base_url: "https://api.openai.com/v1",
  api_key: "",
  has_api_key: false,
};

function normalizeImage2Config(
  value: Partial<Image2ConfigPayload> | null | undefined,
): Image2ConfigPayload {
  const apiKey = String(value?.api_key || "").trim();
  return {
    enabled: value?.enabled !== false,
    model: "gpt-image-2",
    base_url:
      String(value?.base_url || "").trim() || DEFAULT_IMAGE2_CONFIG.base_url,
    api_key: apiKey,
    has_api_key: Boolean(value?.has_api_key) || apiKey !== "",
  };
}

export default function Image2ConfigWorkspace() {
  const [showApiKey, setShowApiKey] = useState(false);
  const [testing, setTesting] = useState(false);
  const { actionStatus, publishActionStatus, showActionStatus } =
    useSettingsActionStatus();

  const {
    form,
    setForm,
    isDirty,
    loading,
    saving,
    reload,
    handleSave,
  } = useSettingsForm<Image2ConfigPayload>({
    defaultValues: DEFAULT_IMAGE2_CONFIG,
    load: async () => normalizeImage2Config(await api.getImage2Config()),
    save: async (values) => {
      const payload: Image2ConfigSavePayload = {
        enabled: values.enabled,
        base_url: values.base_url.trim(),
        api_key: values.api_key.trim(),
      };
      return api.saveImage2Config(payload);
    },
    onSaveFeedback: ({ message, warning }) => {
      publishActionStatus(message, warning, false);
    },
    saveSuccessMessage: "Image 2 配置已保存",
  });

  const hasApiKey = useMemo(
    () => Boolean(form.api_key.trim()) || form.has_api_key,
    [form.api_key, form.has_api_key],
  );

  const testConnection = async () => {
    const apiKey = form.api_key.trim();
    if (!apiKey) {
      toast.error("请先填写 OpenAI API Key");
      return;
    }
    setTesting(true);
    try {
      await api.testImage2Connection({
        base_url: form.base_url.trim(),
        api_key: apiKey,
      });
      showActionStatus("Image 2 连接正常");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Image 2 连接测试失败");
    } finally {
      setTesting(false);
    }
  };

  return (
    <>
      <ActionBar
        dirty={isDirty}
        status={actionStatus}
        left={
          <ActionButton
            onClick={() => void reload()}
            disabled={loading || saving || testing}
          >
            {loading ? "加载中..." : "刷新"}
          </ActionButton>
        }
        right={
          <ActionButton
            variant="primary"
            onClick={() => {
              void (async () => {
                const saved = await handleSave();
                if (saved) {
                  await reload();
                }
              })();
            }}
            disabled={loading || saving || testing || !isDirty}
          >
            {saving ? "保存中..." : "保存"}
          </ActionButton>
        }
      />

      <div className="space-y-3">
        <div className="flex min-h-11 items-center rounded-sm border border-zinc-200 bg-zinc-50 px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-950/55">
          <div className="flex items-center gap-2 text-sm font-semibold text-zinc-800 dark:text-zinc-100">
            <span>Image 2 工具服务</span>
            <InfoTooltip content="质量反馈工具使用的独立图像服务配置" />
          </div>
        </div>

        <section className="rounded-sm border border-zinc-200 bg-white p-4 dark:border-zinc-800 dark:bg-zinc-950/45">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex items-center gap-2">
              <h2 className="text-sm font-semibold text-zinc-900 dark:text-zinc-100">
                图像优化服务
              </h2>
              <StatusBadge tone={hasApiKey ? "success" : "warning"} size="xs">
                {hasApiKey ? "已配置" : "未配置"}
              </StatusBadge>
            </div>

            <label className="inline-flex items-center gap-2 text-xs text-zinc-700 dark:text-zinc-300">
              <input
                type="checkbox"
                className="h-4 w-4 accent-blue-600"
                checked={form.enabled}
                disabled={loading || saving || testing}
                onChange={(event) =>
                  setForm((current) => ({
                    ...current,
                    enabled: event.target.checked,
                  }))
                }
              />
              <span>启用 Image 2</span>
            </label>
          </div>

          <div className="my-4 border-t border-zinc-200 dark:border-zinc-800" />

          <div className="grid gap-4 sm:grid-cols-2">
            <label className="flex flex-col gap-1.5 sm:col-span-2">
              <span className="text-[11px] font-semibold text-zinc-700 dark:text-zinc-300">
                OpenAI API Key <span className="font-normal text-zinc-500">选填</span>
              </span>
              <span className="flex min-w-0">
                <TextInput
                  type={showApiKey ? "text" : "password"}
                  value={form.api_key}
                  autoComplete="off"
                  spellCheck={false}
                  disabled={loading || saving || testing}
                  className="min-w-0 flex-1 rounded-r-none font-mono"
                  aria-label="OpenAI API Key"
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      api_key: event.target.value,
                      has_api_key: event.target.value.trim() !== "",
                    }))
                  }
                />
                <button
                  type="button"
                  className="h-8 shrink-0 rounded-r-sm border border-l-0 border-zinc-300 bg-zinc-100 px-3 text-xs font-medium text-blue-600 hover:bg-zinc-200 disabled:cursor-not-allowed disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-blue-400 dark:hover:bg-zinc-800"
                  disabled={loading || saving || testing || !form.api_key}
                  onClick={() => setShowApiKey((current) => !current)}
                >
                  {showApiKey ? "隐藏" : "显示"}
                </button>
              </span>
            </label>

            <label className="flex flex-col gap-1.5">
              <span className="text-[11px] font-semibold text-zinc-700 dark:text-zinc-300">
                模型
              </span>
              <TextInput
                value={form.model}
                readOnly
                aria-label="Image 2 模型"
                className="font-mono"
              />
            </label>

            <label className="flex flex-col gap-1.5">
              <span className="text-[11px] font-semibold text-zinc-700 dark:text-zinc-300">
                服务地址
              </span>
              <TextInput
                value={form.base_url}
                disabled={loading || saving || testing}
                aria-label="Image 2 服务地址"
                className="font-mono"
                onChange={(event) =>
                  setForm((current) => ({
                    ...current,
                    base_url: event.target.value,
                  }))
                }
              />
            </label>
          </div>

          <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
            <div className="inline-flex items-center gap-2 text-xs text-zinc-600 dark:text-zinc-400">
              <span className="h-1.5 w-1.5 rounded-full bg-emerald-600" />
              <span>密钥由现有本机密钥机制加密保存</span>
            </div>
            <ActionButton
              onClick={() => void testConnection()}
              disabled={loading || saving || testing || !form.api_key.trim()}
            >
              {testing ? "测试中..." : "测试 Image 2 连接"}
            </ActionButton>
          </div>
        </section>
      </div>
    </>
  );
}
