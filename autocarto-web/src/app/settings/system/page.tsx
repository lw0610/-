"use client";

import { toast } from "sonner";
import AppShell from "@/components/layout/AppShell";
import ActionBar, { ActionButton } from "@/components/settings/ActionBar";
import FormSection from "@/components/settings/FormSection";
import IntegrationsConfigWorkspace from "@/components/settings/IntegrationsConfigWorkspace";
import {
  CheckboxField,
  Field,
  NumberInput,
  PathInput,
  SelectInput,
  TextInput,
} from "@/components/settings/FormField";
import SectionNav from "@/components/settings/SectionNav";
import WorkspaceShell from "@/components/layout/WorkspaceShell";
import { useSettingsActionStatus } from "@/hooks/useSettingsActionStatus";
import { useSettingsForm } from "@/hooks/useSettingsForm";
import api, { ConfigBatchItem } from "@/lib/api";
import {
  DEFAULT_CUSTOM_NAME_PATTERN,
  NamePatternMode,
  TIMESTAMP_PATTERN,
  inferPatternMode,
  pickQGISExecutable,
  resolvePattern,
} from "@/lib/config-form-helpers";
import {
  asBool,
  asNumber,
  asRecord,
  asString,
  clampInt,
} from "@/lib/config-utils";
import { parseExecutionMode } from "@/lib/runIntent";
import { useSettingsStore } from "@/stores/settingsStore";
import type { ExecutionMode } from "@/types/message";

type SystemFormState = {
  logLevel: string;
  logDir: string;
  dataDir: string;
  qgisExecutablePath: string;
  logMaxSizeMB: number;
  logMaxBackups: number;
  logMaxAgeDays: number;
  logMaxTotalSizeMB: number;
  logCompress: boolean;
  toolTimeoutSec: number;
  agentTimeoutSec: number;
  maxConcurrentTasks: number;
  loopMaxIterations: number;
  namePatternMode: NamePatternMode;
  customNamePattern: string;
  defaultExecutionMode: ExecutionMode;
  autoTitleEnabled: boolean;
  autoSnapshotBeforeCompress: boolean;
  maxSnapshotsPerSession: number;
  autoArchiveEnabled: boolean;
  autoArchiveDays: number;
  autoCleanupEnabled: boolean;
  autoCleanupArchiveRetentionDays: number;
};

const defaultForm: SystemFormState = {
  logLevel: "INFO",
  logDir: "data/logs",
  dataDir: "data",
  qgisExecutablePath: "",
  logMaxSizeMB: 10,
  logMaxBackups: 5,
  logMaxAgeDays: 30,
  logMaxTotalSizeMB: 100,
  logCompress: false,
  toolTimeoutSec: 30,
  agentTimeoutSec: 120,
  maxConcurrentTasks: 5,
  loopMaxIterations: 100,
  namePatternMode: "timestamp",
  customNamePattern: DEFAULT_CUSTOM_NAME_PATTERN,
  defaultExecutionMode: "build",
  autoTitleEnabled: true,
  autoSnapshotBeforeCompress: true,
  maxSnapshotsPerSession: 10,
  autoArchiveEnabled: false,
  autoArchiveDays: 30,
  autoCleanupEnabled: false,
  autoCleanupArchiveRetentionDays: 90,
};

function formatExecutionModeDisplay(mode: ExecutionMode) {
  return mode.toUpperCase();
}

export default function SystemSettingsPage() {
  const refreshConfig = useSettingsStore((s) => s.refreshConfig);
  const { actionStatus, publishActionStatus } = useSettingsActionStatus();

  const {
    form,
    setForm,
    isDirty,
    loading,
    saving,
    reload,
    handleSave,
    resetToDefault,
  } = useSettingsForm<SystemFormState>({
    defaultValues: defaultForm,
    load: async () => {
      await refreshConfig();
      const cfg = useSettingsStore.getState().config;
      if (!cfg) {
        throw new Error("配置读取失败");
      }

      const log = asRecord(cfg.log);
      const performance = asRecord(cfg.performance);
      const agent = asRecord(cfg.agent);
      const session = asRecord(cfg.session);
      const autoArchive = asRecord(session.auto_archive);
      const autoCleanup = asRecord(session.auto_cleanup);
      const qgis = asRecord(cfg.qgis);
      const inferredPattern = inferPatternMode(
        asString(session.default_name_pattern, TIMESTAMP_PATTERN),
      );

      return {
        logLevel: asString(log.level, defaultForm.logLevel),
        logDir: defaultForm.logDir,
        dataDir: defaultForm.dataDir,
        qgisExecutablePath: asString(
          qgis.executable_path,
          defaultForm.qgisExecutablePath,
        ),
        logMaxSizeMB: clampInt(
          asNumber(log.max_size, defaultForm.logMaxSizeMB * 1024 * 1024) /
            1024 /
            1024,
        ),
        logMaxBackups: clampInt(
          asNumber(log.max_backups, defaultForm.logMaxBackups),
        ),
        logMaxAgeDays: clampInt(
          asNumber(log.max_age, defaultForm.logMaxAgeDays),
        ),
        logMaxTotalSizeMB: clampInt(
          asNumber(
            log.max_total_size,
            defaultForm.logMaxTotalSizeMB * 1024 * 1024,
          ) /
            1024 /
            1024,
        ),
        logCompress: asBool(log.compress, defaultForm.logCompress),
        toolTimeoutSec: clampInt(
          asNumber(
            performance.tool_call_timeout,
            defaultForm.toolTimeoutSec * 1000,
          ) / 1000,
        ),
        agentTimeoutSec: clampInt(
          asNumber(
            performance.agent_response_timeout,
            defaultForm.agentTimeoutSec * 1000,
          ) / 1000,
        ),
        maxConcurrentTasks: clampInt(
          asNumber(
            performance.max_concurrent_tasks,
            defaultForm.maxConcurrentTasks,
          ),
        ),
        loopMaxIterations: clampInt(
          asNumber(agent.loop_max_iterations, defaultForm.loopMaxIterations),
        ),
        namePatternMode: inferredPattern.mode,
        customNamePattern: inferredPattern.custom,
        defaultExecutionMode:
          parseExecutionMode(session.default_execution_mode) || "build",
        autoTitleEnabled: asBool(
          session.auto_title_enabled,
          defaultForm.autoTitleEnabled,
        ),
        autoSnapshotBeforeCompress: asBool(
          session.auto_snapshot_before_compress,
          defaultForm.autoSnapshotBeforeCompress,
        ),
        maxSnapshotsPerSession: clampInt(
          asNumber(
            session.max_snapshots_per_session,
            defaultForm.maxSnapshotsPerSession,
          ),
        ),
        autoArchiveEnabled: asBool(
          autoArchive.enabled,
          defaultForm.autoArchiveEnabled,
        ),
        autoArchiveDays: clampInt(
          asNumber(autoArchive.days, defaultForm.autoArchiveDays),
        ),
        autoCleanupEnabled: asBool(
          autoCleanup.enabled,
          defaultForm.autoCleanupEnabled,
        ),
        autoCleanupArchiveRetentionDays: clampInt(
          asNumber(
            autoCleanup.archive_retention_days,
            defaultForm.autoCleanupArchiveRetentionDays,
          ),
        ),
      };
    },
    validate: (values) => {
      if (
        values.namePatternMode === "custom" &&
        !values.customNamePattern.trim()
      ) {
        return "自定义命名模板不能为空";
      }

      const numbers = [
        values.logMaxSizeMB,
        values.logMaxBackups,
        values.logMaxAgeDays,
        values.logMaxTotalSizeMB,
        values.toolTimeoutSec,
        values.agentTimeoutSec,
        values.maxConcurrentTasks,
        values.loopMaxIterations,
        values.maxSnapshotsPerSession,
        values.autoArchiveDays,
        values.autoCleanupArchiveRetentionDays,
      ];
      if (numbers.some((item) => !Number.isFinite(item) || item < 1)) {
        return "数值配置必须大于等于 1";
      }

      return null;
    },
    save: async (values) => {
      const items: ConfigBatchItem[] = [
        { path: "log.level", value: values.logLevel },
        { path: "log.dir", value: defaultForm.logDir },
        { path: "log.max_size", value: clampInt(values.logMaxSizeMB) * 1024 * 1024 },
        { path: "log.max_backups", value: clampInt(values.logMaxBackups) },
        { path: "log.max_age", value: clampInt(values.logMaxAgeDays) },
        {
          path: "log.max_total_size",
          value: clampInt(values.logMaxTotalSizeMB) * 1024 * 1024,
        },
        { path: "log.compress", value: values.logCompress },
        {
          path: "performance.tool_call_timeout",
          value: clampInt(values.toolTimeoutSec) * 1000,
        },
        {
          path: "performance.agent_response_timeout",
          value: clampInt(values.agentTimeoutSec) * 1000,
        },
        {
          path: "performance.max_concurrent_tasks",
          value: clampInt(values.maxConcurrentTasks),
        },
        {
          path: "agent.loop_max_iterations",
          value: clampInt(values.loopMaxIterations),
        },
        { path: "data_dir", value: defaultForm.dataDir },
        {
          path: "qgis.executable_path",
          value: values.qgisExecutablePath.trim(),
        },
        { path: "session.default_name_pattern", value: resolvePattern(values) },
        {
          path: "session.default_execution_mode",
          value: values.defaultExecutionMode,
        },
        { path: "session.auto_title_enabled", value: values.autoTitleEnabled },
        {
          path: "session.auto_snapshot_before_compress",
          value: values.autoSnapshotBeforeCompress,
        },
        {
          path: "session.max_snapshots_per_session",
          value: clampInt(values.maxSnapshotsPerSession),
        },
        {
          path: "session.auto_archive.enabled",
          value: values.autoArchiveEnabled,
        },
        {
          path: "session.auto_archive.days",
          value: clampInt(values.autoArchiveDays),
        },
        {
          path: "session.auto_cleanup.enabled",
          value: values.autoCleanupEnabled,
        },
        {
          path: "session.auto_cleanup.archive_retention_days",
          value: clampInt(values.autoCleanupArchiveRetentionDays),
        },
      ];

      const result = await api.batchSetConfig(items);
      await refreshConfig();
      return result;
    },
    saveSuccessMessage: "系统设置已保存",
    onSaveFeedback: ({ message, warning, evidence_id, correlation_id }) => {
      publishActionStatus(message, warning, false, evidence_id, correlation_id);
    },
  });

  const handlePickQGISPath = async () => {
    try {
      const path = await pickQGISExecutable();
      if (!path) {
        return;
      }
      setForm((prev) => ({ ...prev, qgisExecutablePath: path }));
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "选择 QGIS 路径失败",
      );
    }
  };

  return (
    <AppShell>
      <WorkspaceShell
        actionBar={
          <ActionBar
            dirty={isDirty}
            status={actionStatus}
            left={
              <>
                <ActionButton onClick={() => void reload()} disabled={loading || saving}>
                  {loading ? "加载中..." : "刷新"}
                </ActionButton>
                <ActionButton onClick={() => resetToDefault()} disabled={saving}>
                  恢复默认
                </ActionButton>
              </>
            }
            right={
              <ActionButton
                variant="primary"
                onClick={() => void handleSave()}
                disabled={loading || saving || !isDirty}
              >
                {saving ? "保存中..." : "保存"}
              </ActionButton>
            }
          />
        }
      >
        <SectionNav items={[{id: "system-integrations", label: "在线数据与浏览器"}, {id: "system-runtime", label: "系统运行"}, {id: "system-session", label: "会话策略"}]} />
        <div className="space-y-3">
          <div id="system-integrations" className="settings-anchor-target">
            <IntegrationsConfigWorkspace />
          </div>
          <div id="system-runtime" className="settings-anchor-target">
              <FormSection
                title="系统运行"
                description="管理基础目录、日志策略、QGIS 路径与运行保护参数。"
              >
                <div className="flex flex-col space-y-4">
                    <Field label="日志级别">
                      <SelectInput
                        value={form.logLevel}
                        onChange={(event) =>
                          setForm((prev) => ({
                            ...prev,
                            logLevel: event.target.value,
                          }))
                        }
                      >
                        <option value="DEBUG">DEBUG</option>
                        <option value="INFO">INFO</option>
                        <option value="WARNING">WARNING</option>
                        <option value="ERROR">ERROR</option>
                      </SelectInput>
                    </Field>

                    <Field label="日志目录" description="固定写入插件目录下的 data/logs">
                      <PathInput
                        value={form.logDir}
                        readOnly
                        aria-readonly
                      />
                    </Field>

                    <Field label="数据目录" description="固定写入插件目录下的 data">
                      <PathInput
                        value={form.dataDir}
                        readOnly
                        aria-readonly
                      />
                    </Field>

                    <Field
                      label="QGIS 可执行路径"
                      description="留空时按系统环境自动探测；桌面端可直接检测或浏览。"
                    >
                      <PathInput
                        value={form.qgisExecutablePath}
                        onChange={(event) =>
                          setForm((prev) => ({
                            ...prev,
                            qgisExecutablePath: event.target.value,
                          }))
                        }
                        placeholder="例如：/Applications/QGIS.app/Contents/MacOS/QGIS"
                        onBrowse={() => void handlePickQGISPath()}
                      />
                    </Field>

                  <div className="border-t border-zinc-200 pt-3 dark:border-zinc-800">
                    <div className="mb-2.5 text-[11px] font-semibold uppercase tracking-[0.12em] text-zinc-500 dark:text-zinc-400">
                      日志与资源
                    </div>
                    <div className="flex flex-col space-y-4">
                      <Field label="单文件大小 (MB)">
                        <NumberInput
                          min={1}
                          value={form.logMaxSizeMB}
                          onChange={(event) =>
                            setForm((prev) => ({
                              ...prev,
                              logMaxSizeMB: clampInt(Number(event.target.value || 1)),
                            }))
                          }
                        />
                      </Field>
                      <Field label="备份数量">
                        <NumberInput
                          min={1}
                          value={form.logMaxBackups}
                          onChange={(event) =>
                            setForm((prev) => ({
                              ...prev,
                              logMaxBackups: clampInt(Number(event.target.value || 1)),
                            }))
                          }
                        />
                      </Field>
                      <Field label="保留天数">
                        <NumberInput
                          min={1}
                          value={form.logMaxAgeDays}
                          onChange={(event) =>
                            setForm((prev) => ({
                              ...prev,
                              logMaxAgeDays: clampInt(Number(event.target.value || 1)),
                            }))
                          }
                        />
                      </Field>
                      <Field label="日志总容量 (MB)">
                        <NumberInput
                          min={1}
                          value={form.logMaxTotalSizeMB}
                          onChange={(event) =>
                            setForm((prev) => ({
                              ...prev,
                              logMaxTotalSizeMB: clampInt(Number(event.target.value || 1)),
                            }))
                          }
                        />
                      </Field>
                      <CheckboxField
                        label="压缩历史日志"
                        description="减小长周期日志占用"
                        checked={form.logCompress}
                        onChange={(next) =>
                          setForm((prev) => ({ ...prev, logCompress: next }))
                        }
                      />
                    </div>
                  </div>

                  <div className="border-t border-zinc-200 pt-3 dark:border-zinc-800">
                    <div className="mb-2.5 text-[11px] font-semibold uppercase tracking-[0.12em] text-zinc-500 dark:text-zinc-400">
                      性能保护
                    </div>
                    <div className="flex flex-col space-y-4">
                      <Field label="工具超时 (秒)">
                        <NumberInput
                          min={1}
                          value={form.toolTimeoutSec}
                          onChange={(event) =>
                            setForm((prev) => ({
                              ...prev,
                              toolTimeoutSec: clampInt(Number(event.target.value || 1)),
                            }))
                          }
                        />
                      </Field>
                      <Field label="智能体超时 (秒)">
                        <NumberInput
                          min={1}
                          value={form.agentTimeoutSec}
                          onChange={(event) =>
                            setForm((prev) => ({
                              ...prev,
                              agentTimeoutSec: clampInt(Number(event.target.value || 1)),
                            }))
                          }
                        />
                      </Field>
                      <Field label="最大并发任务数">
                        <NumberInput
                          min={1}
                          value={form.maxConcurrentTasks}
                          onChange={(event) =>
                            setForm((prev) => ({
                              ...prev,
                              maxConcurrentTasks: clampInt(Number(event.target.value || 1)),
                            }))
                          }
                        />
                      </Field>
                      <Field label="最大循环迭代数">
                        <NumberInput
                          min={1}
                          value={form.loopMaxIterations}
                          onChange={(event) =>
                            setForm((prev) => ({
                              ...prev,
                              loopMaxIterations: clampInt(Number(event.target.value || 1)),
                            }))
                          }
                        />
                      </Field>
                    </div>
                  </div>
                </div>
              </FormSection>
            </div>

            <div id="system-session" className="settings-anchor-target">
              <FormSection
                title="会话策略"
                description="配置默认命名、执行模式、快照与归档清理策略。"
              >
                <div className="flex flex-col space-y-4">
                    <Field label="默认命名规则">
                      <SelectInput
                        value={form.namePatternMode}
                        onChange={(event) =>
                          setForm((prev) => ({
                            ...prev,
                            namePatternMode: event.target.value as NamePatternMode,
                          }))
                        }
                      >
                        <option value="timestamp">会话 MM-DD HH:mm</option>
                        <option value="project-seq">项目名-序号</option>
                        <option value="custom">自定义模板</option>
                      </SelectInput>
                    </Field>

                    <Field label="默认执行模式">
                      <SelectInput
                        value={form.defaultExecutionMode}
                        onChange={(event) =>
                          setForm((prev) => ({
                            ...prev,
                            defaultExecutionMode:
                              parseExecutionMode(event.target.value) || "build",
                          }))
                        }
                      >
                        <option value="plan">优先规划</option>
                        <option value="build">直接执行</option>
                      </SelectInput>
                    </Field>

                    <CheckboxField
                      label="自动生成会话标题"
                      description="用于提升会话列表可读性"
                      checked={form.autoTitleEnabled}
                      onChange={(next) =>
                        setForm((prev) => ({
                          ...prev,
                          autoTitleEnabled: next,
                        }))
                      }
                    />

                    <CheckboxField
                      label="压缩前自动创建快照"
                      description="压缩上下文前保留关键状态"
                      checked={form.autoSnapshotBeforeCompress}
                      onChange={(next) =>
                        setForm((prev) => ({
                          ...prev,
                          autoSnapshotBeforeCompress: next,
                        }))
                      }
                    />

                  {form.namePatternMode === "custom" ? (
                    <Field
                      label="自定义命名模板"
                      description="支持 {date}、{project}、{index} 占位符。"
                    >
                      <TextInput
                        value={form.customNamePattern}
                        onChange={(event) =>
                          setForm((prev) => ({
                            ...prev,
                            customNamePattern: event.target.value,
                          }))
                        }
                      />
                    </Field>
                  ) : null}

                    <Field label="每会话最大快照数">
                      <NumberInput
                        min={1}
                        value={form.maxSnapshotsPerSession}
                        onChange={(event) =>
                          setForm((prev) => ({
                            ...prev,
                            maxSnapshotsPerSession: clampInt(
                              Number(event.target.value || 1),
                            ),
                          }))
                        }
                      />
                    </Field>
                    <Field label="执行模式展示">
                      <TextInput
                        value={formatExecutionModeDisplay(form.defaultExecutionMode)}
                        readOnly
                      />
                    </Field>

                  <div className="border-t border-zinc-200 pt-3 dark:border-zinc-800">
                    <div className="mb-2.5 text-[11px] font-semibold uppercase tracking-[0.12em] text-zinc-500 dark:text-zinc-400">
                      归档与清理
                    </div>
                    <div className="flex flex-col space-y-4">
                        <CheckboxField
                          label="启用自动归档"
                          description="长期未活跃会话自动转入归档"
                          checked={form.autoArchiveEnabled}
                          onChange={(next) =>
                            setForm((prev) => ({
                              ...prev,
                              autoArchiveEnabled: next,
                            }))
                          }
                        />
                        <Field label="归档阈值（天）">
                          <NumberInput
                            min={1}
                            disabled={!form.autoArchiveEnabled}
                            value={form.autoArchiveDays}
                            onChange={(event) =>
                              setForm((prev) => ({
                                ...prev,
                                autoArchiveDays: clampInt(Number(event.target.value || 1)),
                              }))
                            }
                          />
                        </Field>
                        <CheckboxField
                          label="启用自动清理"
                          description="按归档保留期清理旧会话"
                          checked={form.autoCleanupEnabled}
                          onChange={(next) =>
                            setForm((prev) => ({
                              ...prev,
                              autoCleanupEnabled: next,
                            }))
                          }
                        />
                        <Field label="归档保留（天）">
                          <NumberInput
                            min={1}
                            disabled={!form.autoCleanupEnabled}
                            value={form.autoCleanupArchiveRetentionDays}
                            onChange={(event) =>
                              setForm((prev) => ({
                                ...prev,
                                autoCleanupArchiveRetentionDays: clampInt(
                                  Number(event.target.value || 1),
                                ),
                              }))
                            }
                          />
                        </Field>
                    </div>
                  </div>
                </div>
              </FormSection>
            </div>
          </div>
      </WorkspaceShell>
    </AppShell>
  );
}
