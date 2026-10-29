import type {
  AppConfig,
  LLMProviderConfig,
  QGISRecentProject,
} from "@/types/config";
import type { ExecutionMode, RunIntent } from "@/types/message";

const REQUEST_TIMEOUT_MS = 15000;
export const HTTP_CONTROL_CONTRACT_VERSION = "v1";
export const HTTP_CONTROL_CONTRACT_VERSION_HEADER =
  "X-AutoCarto-Contract-Version";

export function buildControlContractHeaders(headers?: HeadersInit) {
  const merged = new Headers(headers);
  if (!merged.has("Content-Type")) {
    merged.set("Content-Type", "application/json");
  }
  merged.set(
    HTTP_CONTROL_CONTRACT_VERSION_HEADER,
    HTTP_CONTROL_CONTRACT_VERSION,
  );
  return merged;
}

function withTimeoutSignal(signal?: AbortSignal | null, timeoutMs = REQUEST_TIMEOUT_MS) {
  const timeoutSignal = AbortSignal.timeout(timeoutMs);
  if (!signal) {
    return timeoutSignal;
  }

  const abortSignalAny = (
    AbortSignal as unknown as {
      any?: (signals: AbortSignal[]) => AbortSignal;
    }
  ).any;
  if (typeof abortSignalAny === "function") {
    return abortSignalAny([signal, timeoutSignal]);
  }

  return signal;
}

interface ApiResponse<T = unknown> {
  success: boolean;
  data?: T;
  details?: unknown;
  code?: string;
  error?: string | ApiErrorEnvelope;
}

export interface ApiErrorEnvelope {
  code?: string;
  message?: string;
  origin?: string;
  stage?: string;
  retryable?: boolean;
  degraded?: boolean;
  user_action?: string;
  debug_context?: Record<string, unknown>;
  evidence_id?: string;
  correlation_id?: string;
}

export class ApiError<T = unknown> extends Error {
  status: number;
  code?: string;
  data?: T;
  envelope?: ApiErrorEnvelope;

  constructor(
    message: string,
    status: number,
    code?: string,
    data?: T,
    envelope?: ApiErrorEnvelope,
  ) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = typeof code === "string" ? code.trim() || undefined : undefined;
    this.data = data;
    this.envelope = envelope;
  }
}

function normalizeApiErrorEnvelope(raw: unknown): ApiErrorEnvelope | undefined {
  if (!raw || typeof raw !== "object") {
    return undefined;
  }
  const record = raw as Record<string, unknown>;
  const envelope: ApiErrorEnvelope = {
    code: typeof record.code === "string" ? record.code.trim() || undefined : undefined,
    message:
      typeof record.message === "string"
        ? record.message.trim() || undefined
        : undefined,
    origin:
      typeof record.origin === "string" ? record.origin.trim() || undefined : undefined,
    stage: typeof record.stage === "string" ? record.stage.trim() || undefined : undefined,
    retryable:
      typeof record.retryable === "boolean" ? record.retryable : undefined,
    degraded: typeof record.degraded === "boolean" ? record.degraded : undefined,
    user_action:
      typeof record.user_action === "string"
        ? record.user_action.trim() || undefined
        : undefined,
    evidence_id:
      typeof record.evidence_id === "string"
        ? record.evidence_id.trim() || undefined
        : undefined,
    correlation_id:
      typeof record.correlation_id === "string"
        ? record.correlation_id.trim() || undefined
        : undefined,
    debug_context:
      record.debug_context && typeof record.debug_context === "object"
        ? (record.debug_context as Record<string, unknown>)
        : undefined,
  };
  if (Object.values(envelope).every((value) => value === undefined)) {
    return undefined;
  }
  return envelope;
}

function resolveApiErrorMessage(
  error: string | ApiErrorEnvelope | undefined,
  fallback: string,
): string {
  if (typeof error === "string" && error.trim()) {
    return error.trim();
  }
  const envelope = normalizeApiErrorEnvelope(error);
  if (envelope?.message) {
    return envelope.message;
  }
  if (envelope?.code) {
    return envelope.code;
  }
  return fallback;
}

export interface ErrorEvidence {
  evidence_id?: string;
  correlation_id?: string;
}

export type IntegrationService = "gee" | "browser" | "osm";

export interface IntegrationCheckResult {
  service?: IntegrationService;
  ok?: boolean;
  success?: boolean;
  message?: string;
  error?: string;
  result?: unknown;
  [key: string]: unknown;
}

export interface InstanceSnapshot {
  instances: import("@/types/instance").QGISInstance[];
  qgis_available: boolean;
  instance_count: number;
}

export interface Capabilities {
  qgis_available: boolean;
  instance_count: number;
}

export interface ServiceDiagnostics {
  service_ready?: boolean;
  qgis_ready?: boolean;
  startup_error?: string;
  startup_phase?: string;
  stderr_tail?: string;
  suggested_actions?: string[];
  evidence_id?: string;
  correlation_id?: string;
}

export interface RuntimeServiceStatus {
  id?: string;
  status: string;
  port?: number;
  host?: string;
  source?: string;
  last_error?: string;
  last_heartbeat?: string;
  last_started_at?: string;
  last_updated_at?: string;
  pid?: number;
  ready?: boolean;
  enabled?: boolean;
  provider_count?: number;
  instance_count?: number;
  running_count?: number;
  restart_attempt?: number;
  diagnostics?: ServiceDiagnostics;
}

export interface RuntimeInstancesSnapshot {
  items: import("@/types/instance").QGISInstance[];
  active_id?: string;
  instance_count: number;
  running_count: number;
}

export interface SettingsModeIssue {
  code?: string;
  message?: string;
}

export interface RuntimeModeReadiness {
  enabled?: boolean;
  ready: boolean;
  code?: string;
  message?: string;
}

export interface RuntimeToolRevisions {
  catalog?: string;
  schema?: string;
  availability?: string;
}

export interface RuntimeSnapshot {
  runtime_paths?: {
    runtime_home: string;
    data_dir: string;
    log_file: string;
    warnings: { code: string; message: string }[];
  };
  version?: number;
  timestamp?: string;
  config_revision?: string;
  discovery_revision?: string;
  active_config?: string;
  service_phase?: string;
  tool_catalog_revision?: string;
  tool_schema_revision?: string;
  tool_availability_revision?: string;
  tool_revisions?: RuntimeToolRevisions;
  last_rebuild_reason?: string;
  last_rebuild_at?: string;
  planner_ready_reason?: string;
  runtime_revision?: string;
  active_agent_config?: string;
  runtime_active_agent_config?: string;
  active_instance_id?: string;
  plan_enabled?: boolean;
  plan_ready?: boolean;
  mode_issues?: Record<string, SettingsModeIssue>;
  mode_readiness?: Record<string, RuntimeModeReadiness>;
  services: Record<string, RuntimeServiceStatus>;
  instances: RuntimeInstancesSnapshot;
  stale?: boolean;
}

export interface RuntimeServicesSnapshot {
  services: Record<string, RuntimeServiceStatus>;
  instances: {
    active_id?: string;
    instance_count?: number;
    running_count?: number;
  };
  config_revision?: string;
  discovery_revision?: string;
  active_config?: string;
  service_phase?: string;
  runtime_revision?: string;
  runtime_active_agent_config?: string;
  active_instance_id?: string;
  mode_issues?: Record<string, SettingsModeIssue>;
  mode_readiness?: Record<string, RuntimeModeReadiness>;
  tool_catalog_revision?: string;
  tool_schema_revision?: string;
  tool_availability_revision?: string;
  tool_revisions?: RuntimeToolRevisions;
  last_rebuild_reason?: string;
  last_rebuild_at?: string;
  planner_ready_reason?: string;
  stale?: boolean;
  version?: number;
  timestamp?: string;
}

type RuntimeSnapshotInput = Partial<Omit<RuntimeSnapshot, "instances">> & {
  instances?:
    | Partial<RuntimeInstancesSnapshot>
    | RuntimeServicesSnapshot["instances"]
    | null;
};

export interface SettingsMutationResult {
  scope?: string;
  persisted?: boolean;
  config_loaded?: boolean;
  runtime_applied?: boolean;
  affected_domain?: string;
  active_agent_config?: string;
  runtime_revision?: string;
  required_action?:
    | "none"
    | "runtime_rebuild"
    | "desktop_restart"
    | "stop_active_runs";
  next_step?: string;
  warnings?: string[];
  affected_paths?: string[];
  blocking_runs?: string[];
  blocking_instances?: string[];
  mode_issues?: Record<string, SettingsModeIssue>;
  evidence_id?: string;
  correlation_id?: string;
}

export type ToolCatalogScope = "global" | "active" | "effective";

export interface ToolCatalogStatus {
  active_config?: string;
  scope?: ToolCatalogScope;
  cli_ready?: boolean;
  compute_ready?: boolean;
  active_instance_id?: string;
  running_instance_count?: number;
  qgis_tool_count?: number;
  cli_tool_count?: number;
  interactive_tool_count?: number;
  compute_tool_count?: number;
  runtime_tool_catalog_revision?: string;
  tool_schema_revision?: string;
  tool_availability_revision?: string;
  last_rebuild_reason?: string;
  last_rebuild_at?: string;
  planner_ready_reason?: string;
}

export interface ToolCatalogPolicy {
  scope?: ToolCatalogScope;
  config_name?: string;
  enabled?: boolean;
}

export interface ToolCatalogCapability {
  display_name?: string;
  description?: string;
  keywords?: string[];
  package_id?: string;
  id: string;
  source?: string;
  capability_type?: string;
  runtime_target?: string;
  availability?: string;
  missing_dependency_reason?: string;
  policy?: ToolCatalogPolicy;
  degradation_status?: string;
  instance_ids?: string[];
  evidence_id?: string;
  correlation_id?: string;
}

export interface ToolCatalogContract {
  id: string;
  version: string;
  source?: string;
  capability_type?: string;
  runtime_target?: string;
  availability: string;
  missing_dependency_reason?: string;
  policy?: ToolCatalogPolicy;
  degradation_status: string;
  items: ToolCatalogCapability[];
}

export interface ToolCatalogResponse {
  items?: ToolCatalogCapability[];
  scope: ToolCatalogScope;
  config_name?: string;
  runtime_revision?: string;
  available_count?: number;
  configured_count?: number;
  tool_catalog_status?: ToolCatalogStatus;
  catalog: ToolCatalogContract;
  tool_infos: ToolCatalogItem[];
  tools: string[];
}

export const EMPTY_RUNTIME_SNAPSHOT: RuntimeSnapshot = {
  services: {},
  instances: {
    items: [],
    instance_count: 0,
    running_count: 0,
  },
  stale: false,
};

function normalizeModeIssues(
  raw: unknown,
): Record<string, SettingsModeIssue> | undefined {
  if (!raw || typeof raw !== "object") {
    return undefined;
  }
  const result: Record<string, SettingsModeIssue> = {};
  Object.entries(raw as Record<string, unknown>).forEach(([mode, value]) => {
    const normalizedMode = String(mode || "").trim();
    if (!normalizedMode || !value || typeof value !== "object") {
      return;
    }
    const issue = value as Record<string, unknown>;
    result[normalizedMode] = {
      code: String(issue.code || "").trim() || undefined,
      message: String(issue.message || "").trim() || undefined,
    };
  });
  if (Object.keys(result).length === 0) {
    return undefined;
  }
  return result;
}

function normalizeModeReadiness(
  raw: unknown,
): Record<string, RuntimeModeReadiness> | undefined {
  if (!raw || typeof raw !== "object") {
    return undefined;
  }
  const result: Record<string, RuntimeModeReadiness> = {};
  Object.entries(raw as Record<string, unknown>).forEach(([mode, value]) => {
    const normalizedMode = String(mode || "").trim();
    if (!normalizedMode || !value || typeof value !== "object") {
      return;
    }
    const readiness = value as Record<string, unknown>;
    result[normalizedMode] = {
      enabled:
        typeof readiness.enabled === "boolean" ? readiness.enabled : undefined,
      ready: Boolean(readiness.ready),
      code: String(readiness.code || "").trim() || undefined,
      message: String(readiness.message || "").trim() || undefined,
    };
  });
  if (Object.keys(result).length === 0) {
    return undefined;
  }
  return result;
}

function normalizeServiceDiagnostics(raw: unknown): ServiceDiagnostics | undefined {
  if (!raw || typeof raw !== 'object') {
    return undefined;
  }
  const record = raw as Record<string, unknown>;
  return {
    service_ready: typeof record.service_ready === 'boolean' ? record.service_ready : undefined,
    qgis_ready: typeof record.qgis_ready === 'boolean' ? record.qgis_ready : undefined,
    startup_error: typeof record.startup_error === 'string' ? record.startup_error : undefined,
    startup_phase: typeof record.startup_phase === 'string' ? record.startup_phase : undefined,
    stderr_tail: typeof record.stderr_tail === 'string' ? record.stderr_tail : undefined,
    suggested_actions: Array.isArray(record.suggested_actions) ? record.suggested_actions.map(s => String(s)) : undefined,
    evidence_id: typeof record.evidence_id === 'string' ? record.evidence_id : undefined,
    correlation_id: typeof record.correlation_id === 'string' ? record.correlation_id : undefined,
  };
}

function normalizeToolRevisions(
  raw: unknown,
  fallback?: Partial<RuntimeToolRevisions>,
): RuntimeToolRevisions | undefined {
  const record =
    raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  const normalized = {
    catalog:
      typeof record.catalog === "string" ? record.catalog : fallback?.catalog,
    schema:
      typeof record.schema === "string" ? record.schema : fallback?.schema,
    availability:
      typeof record.availability === "string"
        ? record.availability
        : fallback?.availability,
  };
  if (!normalized.catalog && !normalized.schema && !normalized.availability) {
    return undefined;
  }
  return normalized;
}

export function normalizeSettingsMutationResult(
  payload?: Partial<SettingsMutationResult> | null,
): SettingsMutationResult {
  const warnings = Array.isArray(payload?.warnings)
    ? payload.warnings.map((item) => String(item || "").trim()).filter(Boolean)
    : [];
  const affectedPaths = Array.isArray(payload?.affected_paths)
    ? payload.affected_paths
        .map((item) => String(item || "").trim())
        .filter(Boolean)
    : [];
  const blockingRuns = Array.isArray(payload?.blocking_runs)
    ? payload.blocking_runs
        .map((item) => String(item || "").trim())
        .filter(Boolean)
    : [];
  const blockingInstances = Array.isArray(payload?.blocking_instances)
    ? payload.blocking_instances
        .map((item) => String(item || "").trim())
        .filter(Boolean)
    : [];
  return {
    scope: String(payload?.scope || "").trim() || undefined,
    persisted:
      typeof payload?.persisted === "boolean" ? payload.persisted : undefined,
    config_loaded:
      typeof payload?.config_loaded === "boolean"
        ? payload.config_loaded
        : undefined,
    runtime_applied:
      typeof payload?.runtime_applied === "boolean"
        ? payload.runtime_applied
        : undefined,
    active_agent_config:
      typeof payload?.active_agent_config === "string"
        ? payload.active_agent_config
        : undefined,
    affected_domain:
      typeof payload?.affected_domain === "string"
        ? payload.affected_domain
        : undefined,
    runtime_revision:
      typeof payload?.runtime_revision === "string"
        ? payload.runtime_revision
        : undefined,
    required_action:
      payload?.required_action === "runtime_rebuild" ||
      payload?.required_action === "desktop_restart" ||
      payload?.required_action === "stop_active_runs" ||
      payload?.required_action === "none"
        ? payload.required_action
        : undefined,
    next_step:
      typeof payload?.next_step === "string" ? payload.next_step : undefined,
    warnings,
    affected_paths: affectedPaths,
    blocking_runs: blockingRuns,
    blocking_instances: blockingInstances,
    mode_issues: normalizeModeIssues(payload?.mode_issues),
    evidence_id: typeof payload?.evidence_id === "string" ? payload.evidence_id.trim() || undefined : undefined,
    correlation_id: typeof payload?.correlation_id === "string" ? payload.correlation_id.trim() || undefined : undefined,
  };
}

function normalizeSkillImportEntries(raw: unknown): SkillImportEntry[] {
  if (!Array.isArray(raw)) {
    return [];
  }
  return raw.reduce<SkillImportEntry[]>((result, item) => {
    if (!item || typeof item !== "object") {
      return result;
    }
    const record = item as Record<string, unknown>;
    result.push({
      ...record,
      name: String(record.name || "").trim() || undefined,
      source: String(record.source || "").trim() || undefined,
      reason: String(record.reason || "").trim() || undefined,
    });
    return result;
  }, []);
}

export function normalizeSkillImportResult(
  payload?: Partial<SkillImportResult> | null,
): SkillImportResult {
  return {
    action:
      typeof payload?.action === "string"
        ? payload.action.trim() || undefined
        : undefined,
    mode:
      typeof payload?.mode === "string"
        ? payload.mode.trim() || undefined
        : undefined,
    imported: normalizeSkillImportEntries(payload?.imported),
    skipped: normalizeSkillImportEntries(payload?.skipped),
    mutation: normalizeSettingsMutationResult(payload?.mutation),
  };
}

export function describeSettingsMutation(
  result: SettingsMutationResult | null | undefined,
  fallbackMessage: string,
) {
  const normalized = normalizeSettingsMutationResult(result);
  const warnings = Array.isArray(normalized.warnings)
    ? [...normalized.warnings]
    : [];
  const planIssue = String(normalized.mode_issues?.plan?.message || "").trim();
  if (planIssue && !warnings.includes(planIssue)) {
    warnings.push(planIssue);
  }
  if (normalized.runtime_applied) {
    return {
      message: `${fallbackMessage}，运行态已同步`,
      warning: warnings.length > 0 ? warnings.join("；") : "",
    };
  }
  if (normalized.required_action && normalized.required_action !== "none") {
    const actionLabel =
      normalized.required_action === "desktop_restart"
        ? "需要重启桌面端"
        : normalized.required_action === "stop_active_runs"
          ? "需要先停止相关运行"
          : "需要重建运行时";
    const blocking = [
      (normalized.blocking_runs || []).length > 0
        ? `阻塞会话：${normalized.blocking_runs?.join("、")}`
        : "",
      (normalized.blocking_instances || []).length > 0
        ? `关联实例：${normalized.blocking_instances?.join("、")}`
        : "",
    ]
      .filter(Boolean)
      .join("；");
    const nextStep = String(normalized.next_step || "").trim();
    return {
      message: `${fallbackMessage}，${actionLabel}`,
      warning: [warnings.join("；"), blocking, nextStep]
        .filter(Boolean)
        .join("；"),
    };
  }
  if (warnings.length > 0) {
    return {
      message: fallbackMessage,
      warning: warnings.join("；"),
    };
  }
  if (normalized.config_loaded || normalized.persisted) {
    return {
      message: `${fallbackMessage}，配置已写入，运行态未完全同步`,
      warning: "",
    };
  }
  return { message: fallbackMessage, warning: "" };
}

export function extractMutationFromError(
  error: unknown,
): SettingsMutationResult | undefined {
  if (!(error instanceof ApiError)) {
    return undefined;
  }
  return normalizeSettingsMutationResult(
    error.data as Partial<SettingsMutationResult> | null | undefined,
  );
}

export function getApiErrorMessage(error: unknown, fallback: string) {
  if (error instanceof ApiError && error.message.trim()) {
    return error.message;
  }
  if (error instanceof Error && error.message.trim()) {
    return error.message;
  }
  return fallback;
}

function normalizeToolCatalogPolicy(raw: unknown): ToolCatalogPolicy | undefined {
  if (!raw || typeof raw !== "object") {
    return undefined;
  }
  const record = raw as Record<string, unknown>;
  const scope = String(record.scope || "").trim().toLowerCase();
  const normalizedScope: ToolCatalogScope | undefined =
    scope === "active" || scope === "effective" || scope === "global"
      ? scope
      : undefined;
  const policy: ToolCatalogPolicy = {
    scope: normalizedScope,
    config_name:
      typeof record.config_name === "string"
        ? record.config_name.trim() || undefined
        : undefined,
    enabled:
      typeof record.enabled === "boolean" ? record.enabled : undefined,
  };
  if (Object.values(policy).every((value) => value === undefined)) {
    return undefined;
  }
  return policy;
}

function normalizeToolCatalogCapabilities(raw: unknown): ToolCatalogCapability[] {
  if (!Array.isArray(raw)) {
    return [];
  }
  const capabilities: ToolCatalogCapability[] = [];
  for (const item of raw) {
    const record = item as Record<string, unknown>;
    const id = String(record?.id || "").trim();
    if (!id) {
      continue;
    }
    capabilities.push({
      id,
      display_name: String(record.display_name || "").trim(), description: String(record.description || "").trim(),
      keywords: Array.isArray(record.keywords) ? record.keywords.map(String) : [],
      package_id: String(record.package_id || "").trim(),
      source: String(record?.source || "").trim() || undefined,
      capability_type:
        String(record?.capability_type || "").trim() || undefined,
      runtime_target: String(record?.runtime_target || "").trim() || undefined,
      availability: String(record?.availability || "").trim() || undefined,
      missing_dependency_reason:
        String(record?.missing_dependency_reason || "").trim() || undefined,
      policy: normalizeToolCatalogPolicy(record?.policy),
      degradation_status:
        String(record?.degradation_status || "").trim() || undefined,
      instance_ids: Array.isArray(record?.instance_ids)
        ? record.instance_ids
            .map((value) => String(value || "").trim())
            .filter(Boolean)
        : [],
      evidence_id: String(record?.evidence_id || "").trim() || undefined,
      correlation_id:
        String(record?.correlation_id || "").trim() || undefined,
    });
  }
  return capabilities;
}

function normalizeToolCatalogContract(
  raw: unknown,
  scope: ToolCatalogScope,
  configName?: string,
): ToolCatalogContract {
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  const items = normalizeToolCatalogCapabilities(record.items);
  if (items.length > 0 || Object.keys(record).length > 0) {
    return {
      id: String(record.id || `tool_catalog/${scope}`).trim() || `tool_catalog/${scope}`,
      version: String(record.version || "1").trim() || "1",
      source: String(record.source || "").trim() || undefined,
      capability_type:
        String(record.capability_type || "tool_catalog").trim() || "tool_catalog",
      runtime_target:
        String(record.runtime_target || "mixed").trim() || "mixed",
      availability:
        String(record.availability || "available").trim() || "available",
      missing_dependency_reason:
        String(record.missing_dependency_reason || "").trim() || undefined,
      policy:
        normalizeToolCatalogPolicy(record.policy) ??
        normalizeToolCatalogPolicy({ scope, config_name: configName }),
      degradation_status:
        String(record.degradation_status || "none").trim() || "none",
      items,
    };
  }

  return {
    id: `tool_catalog/${scope}`,
    version: "1",
    source: "go_control_plane",
    capability_type: "tool_catalog",
    runtime_target: "mixed",
    availability: "valid_empty",
    policy: normalizeToolCatalogPolicy({ scope, config_name: configName }),
    degradation_status: "none",
    items: [],
  };
}

function normalizeToolCatalogItemsFromContract(
  items: ToolCatalogCapability[],
): ToolCatalogItem[] {
  return items
    .map((item) => ({
      name: String(item.id || "").trim(),
      display_name: item.display_name, description: item.description, keywords: item.keywords, package_id:item.package_id,
      source: String(item.source || "").trim(),
      backend: String(item.runtime_target || "").trim(),
      available: String(item.availability || "").trim() === "available",
      missing_reason: String(item.missing_dependency_reason || "").trim(),
      enabled:
        typeof item.policy?.enabled === "boolean"
          ? item.policy.enabled
          : undefined,
      instance_ids: Array.isArray(item.instance_ids) ? item.instance_ids : [],
      evidence_id: String(item.evidence_id || "").trim() || undefined,
      correlation_id:
        String(item.correlation_id || "").trim() || undefined,
    }))
    .filter((item) => item.name);
}

export function normalizeToolCatalogResponse(
  payload?: Partial<ToolCatalogResponse> | null,
): ToolCatalogResponse {
  const scope = String(payload?.scope || "global")
    .trim()
    .toLowerCase();
  const normalizedScope: ToolCatalogScope =
    scope === "active" || scope === "effective" ? scope : "global";
  const catalog = normalizeToolCatalogContract(
    payload?.catalog || (Array.isArray(payload?.items) ? payload : undefined),
    normalizedScope,
    typeof payload?.config_name === "string" ? payload.config_name : undefined,
  );
  const toolInfos = normalizeToolCatalogItemsFromContract(catalog.items);
  const configuredTools = toolInfos.map((item) => item.name);
  return {
    scope: normalizedScope,
    config_name:
      typeof payload?.config_name === "string"
        ? payload.config_name
        : undefined,
    runtime_revision:
      typeof payload?.runtime_revision === "string"
        ? payload.runtime_revision
        : undefined,
    catalog,
    available_count: toolInfos.filter((item) => item.available !== false).length,
    configured_count: configuredTools.length,
    tool_catalog_status: undefined,
    tool_infos: toolInfos,
    tools: configuredTools,
  };
}

export function normalizeRuntimeSnapshot(
  snapshot?: RuntimeSnapshotInput | null,
): RuntimeSnapshot {
  const instances: Partial<RuntimeInstancesSnapshot> =
    snapshot?.instances ?? {};
  const modeReadiness = normalizeModeReadiness(snapshot?.mode_readiness);
  const modeIssues = normalizeModeIssues(snapshot?.mode_issues);
  const fallbackPlanReadiness = modeReadiness?.plan;
  const fallbackPlanReady =
    typeof snapshot?.plan_ready === "boolean"
      ? snapshot.plan_ready
      : fallbackPlanReadiness
        ? fallbackPlanReadiness.ready
        : undefined;
  const fallbackPlanEnabled =
    typeof snapshot?.plan_enabled === "boolean"
      ? snapshot.plan_enabled
      : fallbackPlanReadiness?.enabled;

  const services: Record<string, RuntimeServiceStatus> = {};
  if (snapshot?.services && typeof snapshot.services === "object") {
    Object.entries(snapshot.services).forEach(([k, v]) => {
      const record = v ?? {};
      services[k] = {
        ...record,
        status: String(record.status || "unknown"),
        diagnostics: normalizeServiceDiagnostics(record.diagnostics),
      };
    });
  }

  return {
    version:
      typeof snapshot?.version === "number" ? snapshot.version : undefined,
    timestamp:
      typeof snapshot?.timestamp === "string" ? snapshot.timestamp : undefined,
    config_revision:
      typeof snapshot?.config_revision === "string"
        ? snapshot.config_revision
        : typeof snapshot?.runtime_revision === "string"
          ? snapshot.runtime_revision
          : undefined,
    discovery_revision:
      typeof snapshot?.discovery_revision === "string"
        ? snapshot.discovery_revision
        : typeof snapshot?.runtime_revision === "string"
          ? snapshot.runtime_revision
          : undefined,
    active_config:
      typeof snapshot?.active_config === "string"
        ? snapshot.active_config
        : undefined,
    service_phase:
      typeof snapshot?.service_phase === "string"
        ? snapshot.service_phase
        : undefined,
    tool_catalog_revision:
      typeof snapshot?.tool_catalog_revision === "string"
        ? snapshot.tool_catalog_revision
        : undefined,
    tool_schema_revision:
      typeof snapshot?.tool_schema_revision === "string"
        ? snapshot.tool_schema_revision
        : undefined,
    tool_availability_revision:
      typeof snapshot?.tool_availability_revision === "string"
        ? snapshot.tool_availability_revision
        : undefined,
    tool_revisions: normalizeToolRevisions(snapshot?.tool_revisions, {
      catalog:
        typeof snapshot?.tool_catalog_revision === "string"
          ? snapshot.tool_catalog_revision
          : undefined,
      schema:
        typeof snapshot?.tool_schema_revision === "string"
          ? snapshot.tool_schema_revision
          : undefined,
      availability:
        typeof snapshot?.tool_availability_revision === "string"
          ? snapshot.tool_availability_revision
          : undefined,
    }),
    last_rebuild_reason:
      typeof snapshot?.last_rebuild_reason === "string"
        ? snapshot.last_rebuild_reason
        : undefined,
    last_rebuild_at:
      typeof snapshot?.last_rebuild_at === "string"
        ? snapshot.last_rebuild_at
        : undefined,
    planner_ready_reason:
      typeof snapshot?.planner_ready_reason === "string"
        ? snapshot.planner_ready_reason
        : undefined,
    runtime_revision:
      typeof snapshot?.runtime_revision === "string"
        ? snapshot.runtime_revision
        : undefined,
    active_agent_config:
      typeof snapshot?.active_agent_config === "string"
        ? snapshot.active_agent_config
        : undefined,
    runtime_active_agent_config:
      typeof snapshot?.runtime_active_agent_config === "string"
        ? snapshot.runtime_active_agent_config
        : undefined,
    active_instance_id:
      typeof snapshot?.active_instance_id === "string"
        ? snapshot.active_instance_id
        : undefined,
    plan_enabled: fallbackPlanEnabled,
    plan_ready: fallbackPlanReady,
    mode_issues: modeIssues,
    mode_readiness: modeReadiness,
    runtime_paths: snapshot?.runtime_paths,
    services,
    instances: {
      items: Array.isArray(instances.items) ? instances.items : [],
      active_id:
        typeof instances.active_id === "string"
          ? instances.active_id
          : undefined,
      instance_count:
        typeof instances.instance_count === "number"
          ? instances.instance_count
          : Array.isArray(instances.items)
            ? instances.items.length
            : 0,
      running_count:
        typeof instances.running_count === "number"
          ? instances.running_count
          : Array.isArray(instances.items)
            ? instances.items.filter((item) => item?.status === "running")
                .length
            : 0,
    },
    stale: Boolean(snapshot?.stale),
  };
}

export function toRuntimeServicesSnapshot(
  snapshot?: RuntimeSnapshotInput | Partial<RuntimeServicesSnapshot> | null,
): RuntimeServicesSnapshot {
  const rawInstances =
    snapshot?.instances && typeof snapshot.instances === "object"
      ? (snapshot.instances as Record<string, unknown>)
      : {};
  const normalized = normalizeRuntimeSnapshot({
    ...(snapshot ?? {}),
    instances: {
      items: Array.isArray(rawInstances.items) ? rawInstances.items : [],
      active_id:
        typeof rawInstances.active_id === "string"
          ? rawInstances.active_id
          : undefined,
      instance_count:
        typeof rawInstances.instance_count === "number"
          ? rawInstances.instance_count
          : undefined,
      running_count:
        typeof rawInstances.running_count === "number"
          ? rawInstances.running_count
          : undefined,
    },
  });
  return {
    services: normalized.services,
    instances: {
      active_id: normalized.instances.active_id,
      instance_count: normalized.instances.instance_count,
      running_count: normalized.instances.running_count,
    },
    config_revision: normalized.config_revision,
    discovery_revision: normalized.discovery_revision,
    active_config: normalized.active_config,
    service_phase: normalized.service_phase,
    runtime_revision: normalized.runtime_revision,
    runtime_active_agent_config: normalized.runtime_active_agent_config,
    active_instance_id: normalized.active_instance_id,
    mode_issues: normalized.mode_issues,
    mode_readiness: normalized.mode_readiness,
    tool_catalog_revision: normalized.tool_catalog_revision,
    tool_schema_revision: normalized.tool_schema_revision,
    tool_availability_revision: normalized.tool_availability_revision,
    tool_revisions: normalized.tool_revisions,
    last_rebuild_reason: normalized.last_rebuild_reason,
    last_rebuild_at: normalized.last_rebuild_at,
    planner_ready_reason: normalized.planner_ready_reason,
    stale: normalized.stale,
    version: normalized.version,
    timestamp: normalized.timestamp,
  };
}

export type { LLMProviderConfig };

export interface LLMProviderRegistryItem {
  id: string;
  display_name: string;
  category: "native";
  default_base_url?: string;
  requires_api_key: boolean;
  requires_base_url: boolean;
  suggested_models: string[];
  supports_thinking: boolean;
  supports_forced_tool_choice: boolean;
  supports_plan_execute: boolean;
  backend_type: string;
}

export interface LLMConfigPayload {
  active: string;
  quick_model?: string;
  providers: LLMProviderConfig[];
}

export interface Image2ConfigPayload {
  enabled: boolean;
  model: "gpt-image-2";
  base_url: string;
  api_key: string;
  has_api_key: boolean;
}

export interface Image2ConfigSavePayload {
  enabled: boolean;
  base_url: string;
  api_key: string;
}

export interface Image2ConnectionTestPayload {
  base_url: string;
  api_key: string;
}

export interface AgentConfigSummary {
  config_name: string;
  is_active: boolean;
  is_default: boolean;
  updated_at?: string;
}

export interface AgentLLMConfig {
  [key: string]: unknown;
  provider: string;
}

export type AgentRoleKind = "agent" | "executor";

export interface MigrationWarning { code: string; path: string; package?: string; message: string; }
export interface AgentFlowsConfig { enabled: string[]; [key: string]: unknown; }
// Historical input only; v5 normalization discards this field.
export type FlowPolicyConfig = Record<string, unknown>;

export interface PlanAgentDefData {
  [key: string]: unknown;
  kind?: AgentRoleKind;
  tool_packages?: string[];
  legacy_unresolved_packages?: string[];
  display_name: string;
  is_main?: boolean;
  mode?: string;
  llm?: AgentLLMConfig;
  skill_packages?: string[];
  prompts: Record<string, string>;
}

export interface AgentDefData {
  [key: string]: unknown;
  kind?: AgentRoleKind;
  executor?: string;
  flow_only?: boolean;
  tool_packages?: string[];
  legacy_unresolved_packages?: string[];
  display_name: string;
  description: string;
  is_main?: boolean;
  deletable?: boolean;
  enabled: boolean;
  llm?: AgentLLMConfig;
  skill_packages?: string[];
  prompts: Record<string, string>;
}

export interface AgentGlobalConfig {
  [key: string]: unknown;
  tool_packages?: string[];
  legacy_unresolved_packages?: string[];
  skill_packages?: string[];
}

export interface RuntimeConfig {
  shared: AgentGlobalConfig;
  agents: Record<string, AgentDefData>;
}

export interface AgentConfigMeta {
  [key: string]: unknown;
  default_execution_mode: ExecutionMode;
  description: string;
  plan_enabled: boolean;
}

export interface PlanConfig {
  [key: string]: unknown;
  enabled: boolean;
  max_iterations: number;
  shared: AgentGlobalConfig;
  agents: Record<string, PlanAgentDefData>;
}

export interface AgentProfileDiagnostics {
  selected_config?: string;
  runtime_active_config?: string;
  runtime_revision?: string;
  plan_ready?: boolean;
  plan_issue?: SettingsModeIssue;
  tool_schema_revision?: string;
  tool_availability_revision?: string;
  active_instance_id?: string;
}

export interface AgentConfigData {
  flow_policy?: FlowPolicyConfig;
  [key: string]: unknown;
  version?: string;
  flows?: AgentFlowsConfig;
  migration_warnings?: MigrationWarning[];
  meta: AgentConfigMeta;
  runtime_shared: AgentGlobalConfig;
  runtime_agents: Record<string, AgentDefData>;
  has_plan: boolean;
  plan?: PlanConfig;
  diagnostics?: AgentProfileDiagnostics;
}

interface AgentRuntimeAgentEntryPayload {
  name?: string;
  agent?: AgentDefData;
}

interface AgentPlanAgentEntryPayload {
  name?: string;
  agent?: PlanAgentDefData;
}

interface AgentPlanPayload {
  [key: string]: unknown;
  enabled?: boolean;
  max_iterations?: number;
  shared?: AgentGlobalConfig;
  agents?: AgentPlanAgentEntryPayload[];
}

interface AgentProfilePayload {
  flow_policy?: FlowPolicyConfig;
  [key: string]: unknown;
  version?: string;
  flows?: AgentFlowsConfig;
  migration_warnings?: MigrationWarning[];
  meta?: AgentConfigMeta;
  runtime_shared?: AgentGlobalConfig;
  runtime_agents?: AgentRuntimeAgentEntryPayload[];
  has_plan?: boolean;
  plan?: AgentPlanPayload;
  diagnostics?: AgentProfileDiagnostics;
}

export function toolPackageSelections(raw?: { tool_packages?: string[]; skill_packages?: string[] } | null): string[] {
  const values = Array.isArray(raw?.tool_packages) ? raw.tool_packages : raw?.skill_packages;
  return Array.from(new Set((values || []).map((value) => String(value || "").trim()).filter(Boolean)));
}

export function normalizeAgentGlobalConfig(raw?: Partial<AgentGlobalConfig> | null): AgentGlobalConfig {
  return { ...raw, tool_packages: toolPackageSelections(raw),
    ...(Array.isArray(raw?.legacy_unresolved_packages) ? { legacy_unresolved_packages: [...raw.legacy_unresolved_packages] } : {}),
  };
}

export function normalizeRuntimeAgentDef(raw?: Partial<AgentDefData> | null): AgentDefData {
  return {
    ...raw,
    display_name: String(raw?.display_name || ""), description: String(raw?.description || ""),
    is_main: Boolean(raw?.is_main), deletable: Boolean(raw?.deletable), enabled: raw?.enabled !== false,
    ...(raw?.kind ? {kind:raw.kind} : {}),
    llm: { ...raw?.llm, provider: String(raw?.llm?.provider || "") },
    tool_packages: toolPackageSelections(raw),
    prompts: Object.fromEntries(Object.entries(raw?.prompts || {}).map(([key,value]) => [key,String(value ?? "")])),
  };
}

export function normalizePlanAgentDef(raw?: Partial<PlanAgentDefData> | null): PlanAgentDefData {
  return {
    ...raw,
    display_name: String(raw?.display_name || ""), is_main: Boolean(raw?.is_main),
    mode: String(raw?.mode || "readonly_plus").trim() || "readonly_plus",
    llm: { ...raw?.llm, provider: String(raw?.llm?.provider || "") },
    tool_packages: toolPackageSelections(raw),
    prompts: Object.fromEntries(Object.entries(raw?.prompts || {}).map(([key,value]) => [key,String(value ?? "")])),
  };
}

function normalizeAgentDiagnostics(
  raw?: AgentProfileDiagnostics | null,
): AgentProfileDiagnostics | undefined {
  if (!raw) {
    return undefined;
  }
  return {
    selected_config:
      typeof raw.selected_config === "string"
        ? raw.selected_config.trim()
        : undefined,
    runtime_active_config:
      typeof raw.runtime_active_config === "string"
        ? raw.runtime_active_config.trim()
        : undefined,
    runtime_revision:
      typeof raw.runtime_revision === "string"
        ? raw.runtime_revision.trim()
        : undefined,
    plan_ready:
      typeof raw.plan_ready === "boolean" ? raw.plan_ready : undefined,
    plan_issue: raw.plan_issue,
    tool_schema_revision:
      typeof raw.tool_schema_revision === "string"
        ? raw.tool_schema_revision.trim()
        : undefined,
    tool_availability_revision:
      typeof raw.tool_availability_revision === "string"
        ? raw.tool_availability_revision.trim()
        : undefined,
    active_instance_id:
      typeof raw.active_instance_id === "string"
        ? raw.active_instance_id.trim()
        : undefined,
  };
}

export function normalizeAgentProfilePayload(
  payload?: AgentProfilePayload | AgentConfigData | null,
): AgentConfigData {
  const runtimeShared = normalizeAgentGlobalConfig(payload?.runtime_shared);
  const runtimeAgentsSource = Array.isArray(payload?.runtime_agents)
    ? payload.runtime_agents
    : null;
  if (!runtimeAgentsSource) {
    throw new Error("missing runtime_agents in agent profile payload");
  }
  const runtimeAgents = Object.fromEntries(
    runtimeAgentsSource
      .map((item) => {
        const name = String(item?.name || "").trim();
        if (!name || !item?.agent) {
          return null;
        }
        return [name, item.agent] as const;
      })
      .filter((item): item is readonly [string, AgentDefData] => Boolean(item))
      .map(([name, agent]) => [name, normalizeRuntimeAgentDef(agent)] as const),
  );
  return {
    ...payload,
    version: String(payload?.version || "1.0"),
    flows: undefined,
    flow_policy: undefined,
    migration_warnings: Array.isArray(payload?.migration_warnings) ? payload.migration_warnings.map((warning) => ({ ...warning })) : [],
    meta: {
      ...payload?.meta,
      default_execution_mode: payload?.meta?.default_execution_mode ?? "build",
      description: String(payload?.meta?.description || ""),
      plan_enabled: false,
    },
    runtime_shared: runtimeShared,
    runtime_agents: runtimeAgents,
    has_plan: false,
    plan: undefined,
    diagnostics: normalizeAgentDiagnostics(payload?.diagnostics),
  };
}

export function serializeAgentProfilePayload(data: AgentConfigData) {
  const legacy = !String(data.version || "1.0").startsWith("2.");
  const packages = <T extends AgentGlobalConfig | AgentDefData | PlanAgentDefData>(raw: T): T => {
    const next = { ...raw };
    if (legacy) {
      next.skill_packages = toolPackageSelections(raw);
      delete next.tool_packages;
    } else {
      next.tool_packages = toolPackageSelections(raw);
      delete next.skill_packages;
    }
    return next;
  };
  return {
    ...data,
    version: legacy ? "1.0" : "2.0",
    has_plan: false,
    meta: { ...data.meta, plan_enabled: undefined },
    runtime_shared: packages(data.runtime_shared),
    runtime_agents: Object.entries(data.runtime_agents || {}).map(([name,agent]) => ({ name, agent: packages(agent) })),
    plan: undefined,
    flows: undefined,
    flow_policy: undefined,
  };
}

export interface ConfigBatchItem {
  path: string;
  value: unknown;
}

export interface SessionMessageRecord {
  role: string;
  content: string;
  timestamp?: string;
  msg_type?: string;
  agent?: string;
  agent_display_name?: string;
  phase?: string;
  workflow_block_id?: string;
  data?: Record<string, unknown>;
  [key: string]: unknown;
}

export interface SessionRecord {
  carto_plans?: import('@/features/plan/types').PlanSnapshot[];
  flow_run?: import('@/features/plan/types').FlowRunState | null;
  flow_run_event_seq?: number;
  session_id: string;
  name: string;
  name_auto_generated?: boolean;
  title_source?: string;
  created_at: string;
  updated_at: string;
  project_path?: string;
  llm_config?: Record<string, unknown>;
  execution_mode?: string;
  run_intent?: string;
  messages: SessionMessageRecord[];
  tool_calls?: unknown[];
  workflow_blocks?: unknown[];
  snapshots?: unknown[];
  context_stats?: Record<string, unknown>;
  tags?: string[];
  archived?: boolean;
  description?: string;
  last_message_preview?: string;
  total_tokens_used?: number;
  compression_history?: unknown[];
  [key: string]: unknown;
}

export interface SessionCreatePayload {
  session_id?: string;
  name?: string;
  project_path?: string;
}

export interface SessionPatchPayload {
  name?: string;
  archived?: boolean;
  description?: string;
  tags?: string[];
  execution_mode?: ExecutionMode;
  run_intent?: RunIntent;
  last_message_preview?: string;
}

export interface SessionOperationPayload {
  type: "clear_history" | "restore_snapshot" | "create_snapshot";
  snapshot_id?: string;
  mode?: "strict";
  reason?: string;
}

export interface ProjectRecord {
  path: string;
  last_accessed?: string;
}

export interface SkillItem {
  skill_id?: string;
  name?: string;
  description?: string;
  path?: string;
  updated_at?: string;
  size_bytes?: number;
  [key: string]: unknown;
}

export interface SkillDetail {
  skill_id?: string;
  skill_name: string;
  description: string;
  content: string;
  path?: string;
  updated_at?: string;
  size_bytes?: number;
}

export interface SkillReference {
  config_name: string;
  agent_name: string;
  layer: string;
  source: string;
}

// Tool packages use category. Unknown strings are retained only while reading
// earlier catalogs so retired entries can be filtered instead of misclassified.
export type SkillPackageKind = string;

export interface SkillPackageFrontMatter {
  display_name?: string;
  domain?: string;
  system?: boolean;
  hidden?: boolean;
  auto_mount?: string;
  deprecated?: boolean;
  tools?: string[];
  tool_refs?: string[];
  name?: string;
  description?: string;
  kind?: SkillPackageKind;
  backend?: string;
  enabled?: boolean;
  tags?: string[];
  requires_tools?: string[];
}

export interface SkillPackageToolDefinition {
  metadata?: {
    name?: string;
    description?: string;
    backend_type?: string;
    category?: string;
  };
  [key: string]: unknown;
}

export interface SkillPackageSummary {
  display_name?: string;
  domain?: string;
  system?: boolean;
  hidden?: boolean;
  auto_mount?: string;
  deprecated?: boolean;
  package_id: string;
  name: string;
  description?: string;
  kind?: SkillPackageKind;
  backend?: string;
  source?: string;
  enabled: boolean;
  tags: string[];
  requires_tools: string[];
  tool_count: number;
  tool_ref_count?: number;
  has_tools: boolean;
  has_config: boolean;
  path?: string;
  skill_path?: string;
  updated_at?: string;
  size_bytes?: number;
  scope?: SkillPackageScope;
  owner_config?: string;
}

export type SkillPackageScope =
  | "builtin"
  | "user"
  | "exclusive"
  | "all"
  | "global";

function normalizeSkillPackageScope(
  value: unknown,
): SkillPackageScope | undefined {
  const normalized = String(value || "").trim();
  if (
    normalized === "builtin" ||
    normalized === "user" ||
    normalized === "global" ||
    normalized === "exclusive" ||
    normalized === "all"
  ) {
    return normalized;
  }
  return undefined;
}

export interface SkillPackageDetail extends SkillPackageSummary {
  tool_refs?: string[];
  front_matter?: SkillPackageFrontMatter;
  markdown_body?: string;
  tools: SkillPackageToolDefinition[];
  config?: Record<string, unknown> | null;
  references: SkillReference[];
}

export interface SkillImportEntry {
  name?: string;
  source?: string;
  reason?: string;
  [key: string]: unknown;
}

export interface SkillImportResult {
  action?: string;
  mode?: string;
  imported: SkillImportEntry[];
  skipped: SkillImportEntry[];
  mutation?: SettingsMutationResult;
}

export interface ToolCatalogItem {
  display_name?: string;
  keywords?: string[];
  name: string;
  description?: string;
  category?: string;
  domain?: string;
  source?: string;
  backend?: string;
  package_id?: string;
  kind?: string;
  enabled?: boolean;
  available?: boolean;
  missing_reason?: string;
  instance_ids?: string[];
  evidence_id?: string;
  correlation_id?: string;
}

function normalizeSkillPackageSummary(
  raw?: Partial<SkillPackageSummary> | null,
): SkillPackageSummary {
  return {
    package_id: String(raw?.package_id || "").trim(),
    name: String(raw?.name || raw?.package_id || "").trim(),
    display_name: String(raw?.display_name || "").trim(), domain: String(raw?.domain || "").trim(),
    system: raw?.system === true, hidden: raw?.hidden === true, deprecated: raw?.deprecated === true,
    auto_mount: String(raw?.auto_mount || "").trim(),
    description: String(raw?.description || "").trim(),
    kind: String(raw?.kind || "").trim(),
    backend: String(raw?.backend || "").trim(),
    source: String(raw?.source || "").trim(),
    enabled: raw?.enabled !== false,
    tags: Array.isArray(raw?.tags)
      ? raw.tags.map((item) => String(item || "").trim()).filter(Boolean)
      : [],
    requires_tools: Array.isArray(raw?.requires_tools)
      ? raw.requires_tools
          .map((item) => String(item || "").trim())
          .filter(Boolean)
      : [],
    tool_count: typeof raw?.tool_count === "number" ? raw.tool_count : 0,
    tool_ref_count:
      typeof raw?.tool_ref_count === "number" ? raw.tool_ref_count : undefined,
    has_tools: Boolean(raw?.has_tools),
    has_config: Boolean(raw?.has_config),
    path: String(raw?.path || "").trim(),
    skill_path: String(raw?.skill_path || "").trim(),
    updated_at: String(raw?.updated_at || "").trim(),
    size_bytes: typeof raw?.size_bytes === "number" ? raw.size_bytes : 0,
    scope: normalizeSkillPackageScope(raw?.scope),
    owner_config: String(raw?.owner_config || "").trim() || undefined,
  };
}

function normalizeSkillPackageDetail(
  raw?: Partial<SkillPackageDetail> | null,
): SkillPackageDetail {
  return {
    ...normalizeSkillPackageSummary(raw),
    tool_refs: Array.isArray(raw?.tool_refs) ? [...raw.tool_refs] : [...(raw?.front_matter?.tools || raw?.front_matter?.tool_refs || [])],
    front_matter: raw?.front_matter
      ? {
          ...raw.front_matter,
          name: String(raw.front_matter.name || "").trim(),
          description: String(raw.front_matter.description || "").trim(),
          kind: String(raw.front_matter.kind || "").trim(),
          backend: String(raw.front_matter.backend || "").trim(),
          enabled:
            typeof raw.front_matter.enabled === "boolean"
              ? raw.front_matter.enabled
              : undefined,
          tags: Array.isArray(raw.front_matter.tags)
            ? raw.front_matter.tags
                .map((item) => String(item || "").trim())
                .filter(Boolean)
            : [],
          requires_tools: Array.isArray(raw.front_matter.requires_tools)
            ? raw.front_matter.requires_tools
                .map((item) => String(item || "").trim())
                .filter(Boolean)
            : [],
        }
      : undefined,
    markdown_body: String(raw?.markdown_body || ""),
    tools: Array.isArray(raw?.tools) ? raw.tools : [],
    config:
      raw?.config && typeof raw.config === "object"
        ? (raw.config as Record<string, unknown>)
        : null,
    references: Array.isArray(raw?.references) ? raw.references : [],
  };
}

export type MutationEnvelope<T> = T & {
  mutation?: SettingsMutationResult;
};

function buildQuery(params?: Record<string, string | undefined | null>) {
  if (!params) {
    return "";
  }
  const search = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (typeof value === "string" && value.trim() !== "") {
      search.set(key, value);
    }
  });
  const text = search.toString();
  return text ? `?${text}` : "";
}

function normalizeQGISRecentProject(item: unknown): QGISRecentProject | null {
  if (!item || typeof item !== "object") {
    return null;
  }
  const record = item as Record<string, unknown>;
  const path = String(record.path || "").trim();
  if (!path) {
    return null;
  }
  const name = String(record.name || "").trim();
  const lastOpened = String(record.last_opened || "").trim();
  return {
    path,
    name:
      name ||
      path
        .split(/[\\/]/)
        .pop()
        ?.replace(/\.(qgs|qgz)$/i, "") ||
      path,
    last_opened: lastOpened || new Date(0).toISOString(),
  };
}

export class ApiClient {
  private baseURL: string;

  constructor(baseURL?: string) {
    this.baseURL = typeof baseURL === "string" ? baseURL : "";
  }

  private async request<T>(path: string, options?: RequestInit, timeoutMs = REQUEST_TIMEOUT_MS, envelope = false): Promise<T> {
    const signal = withTimeoutSignal(options?.signal, timeoutMs);
    try {
      return await this.fetchResponse<T>(path, { ...options, signal }, envelope);
    } catch (error) {
      if (!(error instanceof ApiError) && signal.aborted) {
        // WebKit may throw AbortError ("Fetch is aborted") even when the
        // actual cause, recorded on the signal, is a timeout.
        if (signal.reason?.name === "TimeoutError") {
          const method = options?.method?.toUpperCase() || "GET";
          const advice = method === "GET" || method === "HEAD"
            ? "请稍后重试。"
            : "更改可能已保存，请刷新查看结果后再重试。";
          throw new ApiError(`请求等待超过 ${Math.ceil(timeoutMs / 1000)} 秒，${advice}`, 0, "REQUEST_TIMEOUT");
        }
        throw new ApiError("请求已取消。", 0, "REQUEST_ABORTED");
      }
      throw error;
    }
  }

  private async fetchResponse<T>(path: string, options: RequestInit, envelope = false): Promise<T> {
    const url = `${this.baseURL}${path}`;
    const res = await fetch(url, {
      ...options,
      headers: buildControlContractHeaders(options?.headers),
    });

    if (!res.ok) {
      const contentType = res.headers.get("content-type") || "";
      const text = await res.text();
      let message = `API Error ${res.status}: ${text}`;
      let code: string | undefined;
      let data: unknown;
      let envelope: ApiErrorEnvelope | undefined;
      const returnsHtml = contentType.toLowerCase().includes("text/html");
      if (returnsHtml && path.startsWith("/api/")) {
        message = `API Error ${res.status}: ${path} returned HTML instead of JSON; backend runtime may be unavailable.`;
      }
      try {
        const parsed = JSON.parse(text) as ApiResponse<unknown>;
        envelope = normalizeApiErrorEnvelope(parsed.error);
        if (parsed.error || parsed.code || envelope?.message || envelope?.code) {
          message = resolveApiErrorMessage(parsed.error, parsed.code || message);
        }
        code =
          typeof parsed.code === "string"
            ? parsed.code.trim() || undefined
            : envelope?.code;
        data = parsed.data ?? parsed.details;
      } catch (error) {
        void error;
      }
      throw new ApiError(message, res.status, code, data, envelope);
    }

    const json: ApiResponse<T> = await res.json();
    if (!json.success) {
      const envelope = normalizeApiErrorEnvelope(json.error);
      throw new ApiError(
        resolveApiErrorMessage(json.error, "Unknown API error"),
        res.status,
        typeof json.code === "string" ? json.code.trim() || undefined : envelope?.code,
        json.data ?? json.details,
        envelope,
      );
    }
    return (envelope ? json : json.data) as T;
  }

  async get<T>(path: string): Promise<T> {
    return this.request<T>(path);
  }

  async post<T>(path: string, body?: unknown, options?: { timeoutMs?: number }): Promise<T> {
    return this.request<T>(path, {
      method: "POST",
      body: body ? JSON.stringify(body) : undefined,
    }, options?.timeoutMs);
  }

  async postEnvelope<T>(path: string, body?: unknown): Promise<ApiResponse<T> & Record<string, unknown>> {
    return this.request<ApiResponse<T> & Record<string, unknown>>(path, {
      method: "POST",
      body: body ? JSON.stringify(body) : undefined,
    }, REQUEST_TIMEOUT_MS, true);
  }

  async put<T>(path: string, body?: unknown, options?: { timeoutMs?: number }): Promise<T> {
    return this.request<T>(path, {
      method: "PUT",
      body: body ? JSON.stringify(body) : undefined,
    }, options?.timeoutMs);
  }

  async patch<T>(path: string, body?: unknown): Promise<T> {
    return this.request<T>(path, {
      method: "PATCH",
      body: body ? JSON.stringify(body) : undefined,
    });
  }

  async delete<T>(path: string): Promise<T> {
    return this.request<T>(path, { method: "DELETE" });
  }

  // 配置
  async getConfig() {
    return this.get<AppConfig>("/api/config");
  }

  async getConfigValue<T = unknown>(path: string) {
    return this.get<T>(`/api/config/${encodeURIComponent(path)}`);
  }

  async setConfigValue(path: string, value: unknown) {
    return normalizeSettingsMutationResult(
      await this.put<SettingsMutationResult>(
        `/api/config/${encodeURIComponent(path)}`,
        { value },
      ),
    );
  }

  async batchSetConfig(items: ConfigBatchItem[]) {
    return normalizeSettingsMutationResult(
      await this.put<SettingsMutationResult>("/api/config/batch", { items }),
    );
  }

  async checkIntegration(service: IntegrationService, timeoutSeconds = 60) {
    return this.post<IntegrationCheckResult>(
      "/api/integrations/check",
      { service },
      { timeoutMs: Math.max(15_000, timeoutSeconds * 1000 + 10_000) },
    );
  }

  async getQGISExecutablePath() {
    const raw = await this.getConfigValue<unknown>("qgis.executable_path");
    return String(raw || "").trim();
  }

  async getQGISRecentProjects() {
    const raw = await this.getConfigValue<unknown>("qgis.recent_projects");
    if (!Array.isArray(raw)) {
      return [];
    }
    return raw
      .map((item) => normalizeQGISRecentProject(item))
      .filter((item): item is QGISRecentProject => item !== null)
      .sort((a, b) => b.last_opened.localeCompare(a.last_opened));
  }

  async setQGISRecentProjects(projects: QGISRecentProject[]) {
    const normalized = projects
      .map((item) => normalizeQGISRecentProject(item))
      .filter((item): item is QGISRecentProject => item !== null)
      .slice(0, 10);
    return this.setConfigValue("qgis.recent_projects", normalized);
  }

  async upsertQGISRecentProject(path: string, name?: string) {
    const normalizedPath = String(path || "").trim();
    if (!normalizedPath) {
      return [] as QGISRecentProject[];
    }

    const current = await this.getQGISRecentProjects().catch(
      () => [] as QGISRecentProject[],
    );
    const normalizedName =
      String(name || "").trim() ||
      normalizedPath
        .split(/[\\/]/)
        .pop()
        ?.replace(/\.(qgs|qgz)$/i, "") ||
      normalizedPath;
    const now = new Date().toISOString();
    const next = [
      {
        path: normalizedPath,
        name: normalizedName,
        last_opened: now,
      },
      ...current.filter((item) => item.path !== normalizedPath),
    ].slice(0, 10);

    await this.setQGISRecentProjects(next);
    return next;
  }

  async getLLMProviders() {
    return this.get<LLMConfigPayload>("/api/llm/providers");
  }

  async getLLMRegistry() {
    const data = await this.get<{
      providers?: LLMProviderRegistryItem[] | null;
    }>("/api/llm/registry");
    return data.providers ?? [];
  }

  async saveLLMProviders(config: LLMConfigPayload | Record<string, unknown>) {
    return normalizeSettingsMutationResult(
      await this.put<SettingsMutationResult>("/api/llm/providers", config),
    );
  }

  async testLLMConnection(provider: LLMProviderConfig) {
    return this.post("/api/llm/test", provider);
  }

  async getImage2Config() {
    return this.get<Image2ConfigPayload>("/api/image2/config");
  }

  async saveImage2Config(config: Image2ConfigSavePayload) {
    return normalizeSettingsMutationResult(
      await this.put<SettingsMutationResult>("/api/image2/config", config),
    );
  }

  async testImage2Connection(config: Image2ConnectionTestPayload) {
    return this.post<{ status: string; model: string }>(
      "/api/image2/test",
      config,
    );
  }

  // 智能体配置
  async getAgentConfigs() {
    const data = await this.get<{ configs?: AgentConfigSummary[] | null }>(
      "/api/agent-configs",
    );
    return data.configs ?? [];
  }

  async getAgentConfigData(name: string) {
    return normalizeAgentProfilePayload(
      await this.get<AgentProfilePayload>(
        `/api/agent-configs/${encodeURIComponent(name)}/data`,
      ),
    );
  }

  async getAgent(configName: string, agentName: string) {
    throw new Error(
      `Granular agent endpoint has been removed: ${configName}/${agentName}`,
    );
  }

  async saveAgentConfig(name: string, data: AgentConfigData) {
    return normalizeSettingsMutationResult(
      await this.put<SettingsMutationResult>(
        `/api/agent-configs/${encodeURIComponent(name)}`,
        serializeAgentProfilePayload(data),
      ),
    );
  }

  async createAgentConfig(name: string, cloneFrom?: string) {
    const data = await this.post<
      MutationEnvelope<{
        config_name: string;
        cloned_from?: string;
      }>
    >(`/api/agent-configs`, {
      name,
      clone_from: cloneFrom || "",
    });
    return {
      ...data,
      mutation: normalizeSettingsMutationResult(data?.mutation),
    };
  }

  async cloneAgentConfig(sourceName: string, targetName: string) {
    const data = await this.post<
      MutationEnvelope<{
        config_name: string;
        cloned_from?: string;
      }>
    >(`/api/agent-configs/${encodeURIComponent(sourceName)}/clone`, {
      target_name: targetName,
    });
    return {
      ...data,
      mutation: normalizeSettingsMutationResult(data?.mutation),
    };
  }

  async activateAgentConfig(name: string) {
    return normalizeSettingsMutationResult(
      await this.post<SettingsMutationResult>(
        `/api/agent-configs/${encodeURIComponent(name)}/activate`,
        {},
      ),
    );
  }

  async deleteAgentConfig(name: string) {
    const data = await this.delete<
      MutationEnvelope<{
        config_name: string;
      }>
    >(`/api/agent-configs/${encodeURIComponent(name)}`);
    return normalizeSettingsMutationResult(data?.mutation);
  }

  // 会话
  async getSessions(projectPath?: string) {
    const query = buildQuery({ project_path: projectPath });
    const data = await this.get<{ sessions?: SessionRecord[] | null }>(
      `/api/sessions${query}`,
    );
    return data.sessions ?? [];
  }

  async getProjects() {
    const data = await this.get<{ projects?: ProjectRecord[] | null }>(
      "/api/projects",
    );
    return data.projects ?? [];
  }

  async getSession(id: string, projectPath?: string) {
    if (!String(projectPath || "").trim()) {
      throw new Error("project_path is required for getSession");
    }
    const query = buildQuery({ project_path: projectPath });
    return this.get<SessionRecord>(
      `/api/sessions/${encodeURIComponent(id)}${query}`,
    );
  }

  async exportSession(id: string, projectPath?: string) {
    if (!String(projectPath || "").trim()) {
      throw new Error("project_path is required for exportSession");
    }
    const query = buildQuery({ project_path: projectPath });
    return this.get<SessionRecord>(
      `/api/sessions/${encodeURIComponent(id)}/export${query}`,
    );
  }

  async createSession(payload: SessionCreatePayload, projectPath?: string) {
    if (!String(projectPath || "").trim()) {
      throw new Error("project_path is required for createSession");
    }
    const query = buildQuery({
      project_path: projectPath,
    });
    return this.post<SessionRecord>(`/api/sessions${query}`, payload);
  }

  async importSession(session: Record<string, unknown>, projectPath?: string) {
    if (!String(projectPath || "").trim()) {
      throw new Error("project_path is required for importSession");
    }
    const query = buildQuery({ project_path: projectPath });
    return this.post<SessionRecord>(`/api/sessions/import${query}`, {
      session,
      project_path: projectPath,
    });
  }

  async saveSession(id: string, session: SessionRecord, projectPath?: string) {
    if (!String(projectPath || "").trim()) {
      throw new Error("project_path is required for saveSession");
    }
    const query = buildQuery({
      project_path: projectPath,
    });
    return this.put(`/api/sessions/${encodeURIComponent(id)}${query}`, session);
  }

  async patchSession(
    id: string,
    patch: SessionPatchPayload,
    projectPath?: string,
  ) {
    if (!String(projectPath || "").trim()) {
      throw new Error("project_path is required for patchSession");
    }
    const query = buildQuery({ project_path: projectPath });
    return this.patch<SessionRecord>(
      `/api/sessions/${encodeURIComponent(id)}${query}`,
      patch,
    );
  }

  async deleteSession(id: string, projectPath?: string) {
    if (!String(projectPath || "").trim()) {
      throw new Error("project_path is required for deleteSession");
    }
    const query = buildQuery({ project_path: projectPath });
    return this.delete(`/api/sessions/${encodeURIComponent(id)}${query}`);
  }

  async initSessionDir(id: string, projectPath?: string) {
    if (!String(projectPath || "").trim()) {
      throw new Error("project_path is required for initSessionDir");
    }
    const query = buildQuery({ project_path: projectPath });
    return this.post<{ dirs: Record<string, string> }>(
      `/api/sessions/${encodeURIComponent(id)}/dir${query}`,
      {},
    );
  }

  async runSessionOperation(
    id: string,
    operation: SessionOperationPayload,
    projectPath?: string,
    session?: SessionRecord,
  ) {
    if (!String(projectPath || "").trim()) {
      throw new Error("project_path is required for runSessionOperation");
    }
    const query = buildQuery({
      project_path: projectPath,
    });
    return this.post<SessionRecord>(
      `/api/sessions/${encodeURIComponent(id)}/operations${query}`,
      {
        operation,
        session,
        project_path: projectPath,
      },
    );
  }

  // 实例
  async getInstanceSnapshot() {
    const data = await this.get<{
      instances?: import("@/types/instance").QGISInstance[];
      qgis_available?: boolean;
      instance_count?: number;
    }>("/api/instances");
    return {
      instances: data.instances ?? [],
      qgis_available: Boolean(data.qgis_available),
      instance_count:
        typeof data.instance_count === "number"
          ? data.instance_count
          : (data.instances ?? []).length,
    } as InstanceSnapshot;
  }

  async getInstances() {
    const snapshot = await this.getInstanceSnapshot();
    return snapshot.instances;
  }

  async closeInstance(instanceId: string) {
    return this.post<{
      status: string;
      instance_id: string;
      requested_at?: string;
      active_id?: string;
    }>(`/api/instances/${encodeURIComponent(instanceId)}/close`, {});
  }

  async openInstanceProject(instanceId: string, projectPath: string) {
    return this.post<{
      status: string;
      instance_id: string;
      result: unknown;
    }>(`/api/instances/${encodeURIComponent(instanceId)}/open-project`, {
      path: projectPath,
    });
  }

  async createInstanceProject(instanceId: string, projectPath: string) {
    return this.post<{
      status: string;
      instance_id: string;
      result: unknown;
    }>(`/api/instances/${encodeURIComponent(instanceId)}/create-project`, {
      path: projectPath,
    });
  }

  async getCapabilities() {
    const data = await this.get<{
      qgis_available?: boolean;
      instance_count?: number;
    }>("/api/capabilities");
    return {
      qgis_available: Boolean(data.qgis_available),
      instance_count:
        typeof data.instance_count === "number" ? data.instance_count : 0,
    } as Capabilities;
  }

  async getRuntimeSnapshot() {
    const data = await this.get<RuntimeSnapshot>("/api/runtime/snapshot");
    return normalizeRuntimeSnapshot(data);
  }

  async getRuntimeServices() {
    const data = await this.get<RuntimeServicesSnapshot>(
      "/api/runtime/services",
    );
    return toRuntimeServicesSnapshot(data);
  }

  async getSettingsWorkspace() {
    return this.get<Record<string, unknown>>("/api/settings/workspace");
  }

  // 工具包；路径名称保留服务端兼容接口。
  async getSkills() {
    const data = await this.get<{ skills: SkillItem[] }>("/api/skills");
    return data.skills ?? [];
  }

  async getSkill(name: string) {
    return this.get<SkillDetail>(`/api/skills/${encodeURIComponent(name)}`);
  }

  async saveSkill(
    name: string,
    payload: { description: string; content: string },
    scope?: SkillPackageScope,
    configName?: string,
  ) {
    return normalizeSettingsMutationResult(
      await this.put<SettingsMutationResult>(
        `/api/skills/${encodeURIComponent(name)}${buildQuery({
          scope,
          config_name: configName,
        })}`,
        payload,
      ),
    );
  }

  async deleteSkill(
    name: string,
    scope?: SkillPackageScope,
    configName?: string,
  ) {
    return normalizeSettingsMutationResult(
      await this.delete<SettingsMutationResult>(
        `/api/skills/${encodeURIComponent(name)}${buildQuery({
          scope,
          config_name: configName,
        })}`,
      ),
    );
  }

  async getSkillReferences(name: string) {
    const data = await this.get<{ references?: SkillReference[] | null }>(
      `/api/skills/${encodeURIComponent(name)}/references`,
    );
    return data.references ?? [];
  }

  async importSkill(
    sourcePath: string,
    overwrite: boolean,
    scope?: SkillPackageScope,
    configName?: string,
  ) {
    return normalizeSkillImportResult(
      await this.post<SkillImportResult>(
        `/api/skills/import${buildQuery({
          scope,
          config_name: configName,
        })}`,
        {
          source_path: sourcePath,
          overwrite,
        },
      ),
    );
  }

  async getSkillPackages(
    scope: SkillPackageScope = "all",
    configName?: string,
  ) {
    const data = await this.get<{ packages?: SkillPackageSummary[] | null }>(
      `/api/skill-packages${buildQuery({
        scope,
        config_name: configName,
      })}`,
    );
    return Array.isArray(data.packages)
      ? data.packages.map((item) => normalizeSkillPackageSummary(item))
      : [];
  }

  async getSkillPackage(
    packageId: string,
    scope?: SkillPackageScope,
    configName?: string,
  ) {
    return normalizeSkillPackageDetail(
      await this.get<SkillPackageDetail>(
        `/api/skill-packages/${encodeURIComponent(packageId)}${buildQuery({
          scope,
          config_name: configName,
        })}`,
      ),
    );
  }

  async getSkillPackageReferences(packageId: string) {
    const data = await this.get<{ references?: SkillReference[] | null }>(
      `/api/skill-packages/${encodeURIComponent(packageId)}/references`,
    );
    return data.references ?? [];
  }

  async toggleSkillPackage(
    packageId: string,
    enabled: boolean,
    scope?: SkillPackageScope,
    configName?: string,
  ) {
    const data = await this.post<
      MutationEnvelope<{
        package?: SkillPackageSummary | null;
        enabled?: boolean;
        package_id?: string;
      }>
    >(
      `/api/skill-packages/${encodeURIComponent(packageId)}/toggle${buildQuery({
        scope,
        config_name: configName,
      })}`,
      {
        enabled,
      },
    );
    return {
      ...data,
      package: data.package
        ? normalizeSkillPackageSummary(data.package)
        : undefined,
      mutation: normalizeSettingsMutationResult(data.mutation),
    };
  }

  async migrateSkillPackages() {
    return this.post<
      MutationEnvelope<{
        migrated?: Array<Record<string, unknown>>;
        skipped?: Array<Record<string, unknown>>;
      }>
    >("/api/skill-packages/migrate");
  }

  // 工具
  async getToolCatalog(scope: ToolCatalogScope = "global") {
    const data = await this.get<ToolCatalogResponse>(
      `/api/tools/catalog${buildQuery({ scope })}`,
    );
    return normalizeToolCatalogResponse(data);
  }

  async getTools(scope: ToolCatalogScope = "global") {
    const data = await this.getToolCatalog(scope);
    if (data.tool_infos.length > 0) {
      return data.tool_infos;
    }
    return data.tools.map((name) => ({
      name,
      available: true,
      missing_reason: "",
      instance_ids: [],
    }));
  }

  setBaseURL(url: string) {
    this.baseURL = url;
  }
}

function resolveInitialBackendBaseURL() {
  const candidates = [
    process.env.NEXT_PUBLIC_AUTOCARTO_BACKEND_URL,
    process.env.NEXT_PUBLIC_API_URL,
  ];
  for (const candidate of candidates) {
    if (typeof candidate !== "string" || !candidate.trim()) {
      continue;
    }
    try {
      const parsed = new URL(candidate.trim());
      if (parsed.protocol === "http:" || parsed.protocol === "https:") {
        return parsed.origin;
      }
    } catch {
      void 0;
    }
  }
  return "";
}

export const api = new ApiClient(resolveInitialBackendBaseURL());
export default api;
