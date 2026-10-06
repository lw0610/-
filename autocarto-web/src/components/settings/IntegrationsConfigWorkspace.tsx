"use client";

import { useRef, useState } from "react";
import ActionBar, { ActionButton } from "@/components/settings/ActionBar";
import AlertPanel from "@/components/settings/AlertPanel";
import FormSection from "@/components/settings/FormSection";
import { CheckboxField, Field, NumberInput, SelectInput, TextAreaInput, TextInput } from "@/components/settings/FormField";
import { useSettingsActionStatus } from "@/hooks/useSettingsActionStatus";
import { useSettingsForm } from "@/hooks/useSettingsForm";
import api, { getApiErrorMessage, type IntegrationCheckResult, type IntegrationService } from "@/lib/api";
import { asBool, asNumber, asRecord, asString } from "@/lib/config-utils";
import type { IntegrationsConfig } from "@/types/config";

export const DEFAULT_INTEGRATIONS: IntegrationsConfig = {
  gee: { enabled: false, python: "python3", project: "", proxy_url: "", timeout_seconds: 300 },
  browser: { enabled: false, command: "npx", args: ["-y", "@playwright/mcp@0.0.83"], browser: "chrome", headless: false, timeout_seconds: 60 },
  web: { enabled: false, timeout_seconds: 60, max_download_mb: 100 },
};

type IntegrationForm = Omit<IntegrationsConfig, "browser"> & {
  browser: Omit<IntegrationsConfig["browser"], "args"> & { argsText: string };
};

function normalizeForm(raw: unknown): IntegrationForm {
  const config = asRecord(raw);
  const gee = asRecord(config.gee);
  const browser = asRecord(config.browser);
  const web = asRecord(config.web);
  const browserName = asString(browser.browser, DEFAULT_INTEGRATIONS.browser.browser);
  return {
    gee: {
      enabled: asBool(gee.enabled, false),
      python: asString(gee.python, DEFAULT_INTEGRATIONS.gee.python),
      project: asString(gee.project, ""),
      proxy_url: asString(gee.proxy_url, ""),
      timeout_seconds: asNumber(gee.timeout_seconds, DEFAULT_INTEGRATIONS.gee.timeout_seconds),
    },
    browser: {
      enabled: asBool(browser.enabled, false),
      command: asString(browser.command, DEFAULT_INTEGRATIONS.browser.command),
      argsText: Array.isArray(browser.args)
        ? browser.args.filter((arg): arg is string => typeof arg === "string").join("\n")
        : DEFAULT_INTEGRATIONS.browser.args.join("\n"),
      browser: browserName === "msedge" || browserName === "chromium" ? browserName : "chrome",
      headless: asBool(browser.headless, false),
      timeout_seconds: asNumber(browser.timeout_seconds, DEFAULT_INTEGRATIONS.browser.timeout_seconds),
    },
    web: {
      enabled: asBool(web.enabled, false),
      timeout_seconds: asNumber(web.timeout_seconds, DEFAULT_INTEGRATIONS.web.timeout_seconds),
      max_download_mb: asNumber(web.max_download_mb, DEFAULT_INTEGRATIONS.web.max_download_mb),
    },
  };
}

const DEFAULT_FORM = normalizeForm(DEFAULT_INTEGRATIONS);

function toConfig(form: IntegrationForm): IntegrationsConfig {
  const { argsText, ...browser } = form.browser;
  return {
    gee: { ...form.gee, python: form.gee.python.trim(), project: form.gee.project.trim(), proxy_url: form.gee.proxy_url.trim() },
    browser: { ...browser, command: browser.command.trim(), args: argsText.split(/\r?\n/).filter((arg) => arg.trim().length > 0) },
    web: { ...form.web },
  };
}

function validate(form: IntegrationForm): string | null {
  if (!form.gee.python.trim()) return "请填写 Python 解释器。";
  if (!form.browser.command.trim()) return "请填写浏览器启动命令。";
  const numbers = [form.gee.timeout_seconds, form.browser.timeout_seconds, form.web.timeout_seconds, form.web.max_download_mb];
  if (numbers.some((value) => !Number.isInteger(value) || value < 1)) return "超时和下载大小必须为大于等于 1 的整数。";
  if ([form.gee.timeout_seconds, form.browser.timeout_seconds, form.web.timeout_seconds].some((value) => value > 3600)) return "超时最长为 3600 秒。";
  if (form.web.max_download_mb > 1024) return "单文件下载上限不能超过 1024 MB。";
  if (form.gee.proxy_url.trim()) {
    try {
      const url = new URL(form.gee.proxy_url.trim());
      if (!["http:", "https:"].includes(url.protocol)) return "代理地址须使用 http 或 https。";
      if (url.username || url.password || url.search || url.hash) return "代理 URL 不能包含账号密码、查询参数或片段。";
    } catch { return "请填写完整的代理 URL。"; }
  }
  return null;
}

function CheckResult({ result, service }: { result?: IntegrationCheckResult; service: IntegrationService }) {
  if (!result) return null;
  const detail = asRecord(result.result);
  const environment = asRecord(detail.environment);
  const reportedOk = result.ok ?? result.success;
  const geeQueryReady = environment.ready === true;
  const geeDownloadReady = environment.download_ready === true;
  const geeRegionReady = environment.region_ready === true;
  const missingDownloader = service === "gee" && geeQueryReady && !geeDownloadReady;
  const missingRegionLoader = service === "gee" && geeQueryReady && !geeRegionReady;
  const ok = service === "gee"
    ? reportedOk === false || environment.ready === false || environment.download_ready === false || environment.region_ready === false
      ? false
      : geeQueryReady && geeDownloadReady && geeRegionReady ? reportedOk : undefined
    : detail.command_available === false ? false : reportedOk;
  const name = service === "gee" ? "GEE" : "浏览器";
  let message = result.error || asString(detail.command_error, "") || result.message || asString(detail.message, "") || asString(detail.note, "");
  if (!result.error && missingDownloader) {
    message = environment.download_ready === false
      ? "GEE 已连接，可查询；本地影像下载尚未就绪。"
      : "GEE 已连接，可查询；尚未确认本地下载依赖，请更新后台后重新检查。";
  } else if (!result.error && missingRegionLoader) {
    message = environment.region_ready === false
      ? "GEE 已连接；研究区边界读取与检查尚未就绪。"
      : "GEE 已连接；尚未确认研究区边界处理依赖，请更新后台后重新检查。";
  } else if (ok === true) {
    message = service === "browser"
      ? "已找到启动程序；这项检查不会打开网页，实际浏览任务会验证浏览器是否能启动。"
      : "GEE 已连接，边界处理与本地影像下载已就绪；实际任务仍需验证数据权限、网络和输出。";
  }
  return <AlertPanel
    tone={ok === false ? "warning" : ok === true ? "success" : "info"}
    title={`${name} 检查结果`}
    message={<div className="space-y-2">
      <p>{message || (ok === false ? "环境尚未就绪，请查看详情。" : "检查已完成。")}</p>
      {missingDownloader && environment.download_ready === false && <>
        <p>需要 geedim=={asString(environment.geedim_required_version, "2.0.0")}；当前版本：{asString(environment.geedim_version, "未检测到")}。请在上方配置的独立虚拟环境中安装依赖后重试。</p>
        {asString(environment.download_dependency_error, "") && <p>{asString(environment.download_dependency_error, "")}</p>}
      </>}
      {missingRegionLoader && environment.region_ready === false && <>
        <p>需要 GeoPandas 读取边界并检查几何有效性；请在上方配置的独立虚拟环境中补齐依赖后重试。</p>
        {asString(environment.region_dependency_error, "") && <p>{asString(environment.region_dependency_error, "")}</p>}
      </>}
      <details><summary className="cursor-pointer">诊断详情</summary><pre className="mt-2 max-h-56 overflow-auto whitespace-pre-wrap break-words font-mono text-[11px]">{JSON.stringify(result, null, 2)}</pre></details>
    </div>}
  />;
}

export default function IntegrationsConfigWorkspace() {
  const [loaded, setLoaded] = useState(false);
  const [legacyBackend, setLegacyBackend] = useState(false);
  const [error, setError] = useState("");
  const [saveFailed, setSaveFailed] = useState(false);
  const [checking, setChecking] = useState<IntegrationService | null>(null);
  const checkInFlight = useRef(false);
  const [results, setResults] = useState<Partial<Record<IntegrationService, IntegrationCheckResult>>>({});
  const { actionStatus, publishActionStatus } = useSettingsActionStatus();
  const { form, setForm, loading, saving, isDirty, reload, handleSave } = useSettingsForm<IntegrationForm>({
    defaultValues: DEFAULT_FORM,
    load: async () => {
      setLoaded(false);
      setLegacyBackend(false);
      setError("");
      setSaveFailed(false);
      setResults({});
      try {
        const config = await api.getConfig();
        setLegacyBackend(!config.integrations || typeof config.integrations !== "object" || Array.isArray(config.integrations));
        const values = normalizeForm(config.integrations);
        setLoaded(true);
        return values;
      } catch (failure) {
        setError(getApiErrorMessage(failure, "在线数据配置读取失败"));
        throw failure;
      }
    },
    validate: (values) => {
      const message = validate(values);
      if (message) {
        setError(message);
        setSaveFailed(true);
      }
      return message;
    },
    save: async (values) => {
      setError("");
      setSaveFailed(false);
      try {
        const result = await api.batchSetConfig([{ path: "integrations", value: toConfig(values) }]);
        setResults({});
        return result;
      } catch (failure) {
        setSaveFailed(true);
        setError(getApiErrorMessage(failure, "在线数据配置保存失败"));
        throw failure;
      }
    },
    saveSuccessMessage: "在线数据与浏览器配置已保存",
    onSaveFeedback: ({ message, warning, evidence_id, correlation_id }) => publishActionStatus(message, warning, false, evidence_id, correlation_id),
  });

  const busy = loading || saving || checking !== null;
  const update = <K extends keyof IntegrationForm>(service: K, patch: Partial<IntegrationForm[K]>) => {
    setForm((previous) => ({ ...previous, [service]: { ...previous[service], ...patch } }));
    setResults({});
    setError("");
    setSaveFailed(false);
  };
  const check = async (service: IntegrationService) => {
    if (busy || !loaded || legacyBackend || checkInFlight.current) return;
    checkInFlight.current = true;
    setChecking(service);
    setError("");
    setSaveFailed(false);
    setResults((previous) => ({ ...previous, [service]: undefined }));
    try {
      if (isDirty && !(await handleSave())) return;
      const result = await api.checkIntegration(service, form[service].timeout_seconds);
      setResults((previous) => ({ ...previous, [service]: result }));
    } catch (failure) {
      setError(getApiErrorMessage(failure, `${service === "gee" ? "GEE" : "浏览器"} 检查失败`));
    } finally {
      checkInFlight.current = false;
      setChecking(null);
    }
  };

  return <div className="space-y-3" aria-label="在线数据与浏览器配置">
    <ActionBar dirty={isDirty} status={actionStatus}
      left={<ActionButton disabled={busy} onClick={() => void reload()}>{loading ? "正在读取在线数据配置…" : "刷新在线数据配置"}</ActionButton>}
      right={<ActionButton variant="primary" disabled={busy || !loaded || legacyBackend || !isDirty} onClick={() => void handleSave()}>{saving ? "保存中…" : "保存在线数据配置"}</ActionButton>}
    />
    {legacyBackend && <AlertPanel tone="warning" title="后台服务需要更新" message="当前后台服务尚未更新，请重新启动更新后的桌面应用；仅刷新页面不会更新后台。更新前无法保存或检查在线数据配置。" />}
    {error && <AlertPanel tone="error" title={saveFailed ? "在线数据配置尚未保存" : "在线数据操作失败"} message={error} />}
    <p className="text-xs text-zinc-500 dark:text-zinc-400">各项连接默认关闭。修改后可保存配置，或直接保存并检查运行环境；未启用也可以检查依赖。打开设置页不会启动 Python 或浏览器。</p>
    {saveFailed ? <p className="text-xs text-amber-700 dark:text-amber-300">保存失败，当前输入已保留，本次未执行环境检查。修正上方错误后可再次保存并检查。</p> : isDirty && <p className="text-xs text-amber-700 dark:text-amber-300">当前修改尚未保存。点击“保存并检查”会先保存本卡片的全部配置，保存成功后再检查所选环境。</p>}
    <fieldset disabled={busy || !loaded || legacyBackend} className="min-w-0 space-y-3">
      <FormSection title="Google Earth Engine" description="使用本机 Python 连接 GEE，运行脚本并导出数据。">
        <div className="space-y-3">
          <CheckboxField label="启用 GEE" checked={form.gee.enabled} onChange={(enabled) => update("gee", { enabled })} />
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Python 解释器"><TextInput value={form.gee.python} onChange={(event) => update("gee", { python: event.target.value })} placeholder="python3 或解释器绝对路径" /></Field>
            <Field label="Google Cloud 项目 ID"><TextInput value={form.gee.project} onChange={(event) => update("gee", { project: event.target.value })} placeholder="已注册 Earth Engine 的项目 ID" /></Field>
            <Field label="GEE 代理 URL（可选）"><TextInput value={form.gee.proxy_url} onChange={(event) => update("gee", { proxy_url: event.target.value })} placeholder="http://127.0.0.1:7890" /></Field>
            <Field label="GEE 无输出超时（秒）"><NumberInput min={1} max={3600} step={1} value={form.gee.timeout_seconds} onChange={(event) => update("gee", { timeout_seconds: Number(event.target.value) })} /></Field>
          </div>
          <p className="text-xs text-zinc-500 dark:text-zinc-400">请使用独立 Python 虚拟环境安装依赖，避免修改系统或 QGIS 自带 Python；Google 账号授权由你完成，Cloud 项目须已开通 Earth Engine。以下为 macOS / Linux 手动命令示例，页面不会自动安装或授权。</p>
          {form.gee.enabled && !form.gee.project.trim() && <p className="text-xs text-amber-700 dark:text-amber-300">尚未填写 Google Cloud 项目 ID，GEE 初始化时需要已开通的项目。</p>}
          <pre className="overflow-x-auto rounded bg-zinc-100 p-3 text-xs dark:bg-zinc-950"><code>{"python3 -m venv ~/.venvs/autocarto-gee\n~/.venvs/autocarto-gee/bin/python -m pip install earthengine-api geedim==2.0.0\n~/.venvs/autocarto-gee/bin/python -c \"import ee; ee.Authenticate()\""}</code></pre>
          <p className="text-xs text-zinc-500 dark:text-zinc-400">将“Python 解释器”设为该虚拟环境解释器的绝对路径；已有独立环境可复用其路径。Windows 使用虚拟环境内的 Scripts/python.exe。大范围下载会自动分块；脚本持续输出进度或心跳时可以继续运行，连续无输出达到上面的时间才中止。环境检查仍使用该时间作为单次检查上限。</p>
          <ActionButton onClick={() => void check("gee")}>{checking === "gee" ? (saving ? "正在保存配置…" : "正在检查 GEE 运行环境…") : isDirty ? "保存并检查 GEE 运行环境" : "检查 GEE 运行环境"}</ActionButton>
          <CheckResult service="gee" result={results.gee} />
        </div>
      </FormSection>
      <FormSection title="浏览器在线取数" description="通过 Playwright 访问网页、查看页面与下载数据。">
        <div className="space-y-3">
          <CheckboxField label="启用浏览器" checked={form.browser.enabled} onChange={(enabled) => update("browser", { enabled })} />
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="浏览器启动命令"><TextInput value={form.browser.command} onChange={(event) => update("browser", { command: event.target.value })} placeholder="npx 或命令绝对路径" /></Field>
            <Field label="浏览器"><SelectInput value={form.browser.browser} onChange={(event) => update("browser", { browser: event.target.value as IntegrationsConfig["browser"]["browser"] })}><option value="chrome">Google Chrome</option><option value="msedge">Microsoft Edge</option><option value="chromium">Chromium</option></SelectInput></Field>
            <Field label="启动参数（每行一个）"><TextAreaInput rows={3} value={form.browser.argsText} onChange={(event) => update("browser", { argsText: event.target.value })} /></Field>
            <Field label="浏览器调用超时（秒）"><NumberInput min={1} max={3600} step={1} value={form.browser.timeout_seconds} onChange={(event) => update("browser", { timeout_seconds: Number(event.target.value) })} /></Field>
          </div>
          <CheckboxField label="无窗口运行（Headless）" checked={form.browser.headless} onChange={(headless) => update("browser", { headless })} />
          <p className="text-xs text-zinc-500 dark:text-zinc-400">使用独立浏览器会话，不共享你日常浏览器的登录状态。网站登录和验证码需要你手动完成；需要交互登录时请关闭无窗口运行。</p>
          <p className="text-xs text-zinc-500 dark:text-zinc-400">默认命令需要 Node.js 18+ 和所选浏览器。环境检查只查本地命令；首次执行浏览器任务时，npx 可能下载下面固定版本的服务：</p>
          <pre className="overflow-x-auto rounded bg-zinc-100 p-3 text-xs dark:bg-zinc-950"><code>{"npx -y @playwright/mcp@0.0.83 --help"}</code></pre>
          <ActionButton onClick={() => void check("browser")}>{checking === "browser" ? (saving ? "正在保存配置…" : "正在检查浏览器运行环境…") : isDirty ? "保存并检查浏览器运行环境" : "检查浏览器运行环境"}</ActionButton>
          <CheckResult service="browser" result={results.browser} />
        </div>
      </FormSection>
      <FormSection title="网页读取与文件下载" description="读取公开网页，并将在线数据文件下载到工作区。">
        <div className="space-y-3">
          <CheckboxField label="启用网页读取与下载" checked={form.web.enabled} onChange={(enabled) => update("web", { enabled })} />
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="网页请求超时（秒）"><NumberInput min={1} max={3600} step={1} value={form.web.timeout_seconds} onChange={(event) => update("web", { timeout_seconds: Number(event.target.value) })} /></Field>
            <Field label="单文件下载上限（MB）"><NumberInput min={1} max={1024} step={1} value={form.web.max_download_mb} onChange={(event) => update("web", { max_download_mb: Number(event.target.value) })} /></Field>
          </div>
        </div>
      </FormSection>
    </fieldset>
  </div>;
}
