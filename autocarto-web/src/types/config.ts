export interface LLMProviderConfig {
  vision?: boolean
  name: string
  provider: string
  model: string
  base_url?: string
  max_tokens?: number
  temperature?: number
  thinking_enabled?: boolean
  thinking_budget?: number
  max_context_tokens?: number
  max_context_usage?: number
  auto_compress_threshold?: number
  compress_strategy?: string
  has_api_key?: boolean
  api_key?: string
  supports_thinking?: boolean
  supports_plan_execute?: boolean
  supports_forced_tool_choice?: boolean
  ready_for_chat?: boolean
  ready_for_plan?: boolean
  ready_for_auto_title?: boolean
  readiness_reason?: string
}

export interface LLMConfig {
  active: string
  quick_model?: string
  providers: LLMProviderConfig[]
}

export interface PortConfig {
  default: number
  range: number[]
  protocol: string
  description: string
  health_check_interval?: number
  bind_retry_count?: number
  bind_retry_delay?: number
  auto_fallback?: boolean
}

export interface PortsConfig {
  agent_system: PortConfig
}

export interface LogConfig {
  level: string
  dir: string
  max_size: number
  max_backups: number
  max_age: number
  max_total_size: number
  compress: boolean
}

export interface AgentRuntimeConfig {
  loop_max_iterations: number
}

export interface PerformanceConfig {
  tool_call_timeout: number
  agent_response_timeout: number
  max_concurrent_tasks: number
  memory?: {
    enabled?: boolean
    max_task_memory_mb?: number
  }
  cache?: {
    enabled?: boolean
    ttl?: number
    max_size_mb?: number
    strategy?: string
  }
}

export interface ComputeConfig {
  port: number
  max_workers: number
  python_path: string
  module_dir: string
}

export interface ServiceLifecycleConfig {
  auto_start_on_app_launch?: boolean
  auto_stop_on_app_exit?: boolean
  auto_restart_on_failure?: boolean
}

export interface ServicesConfig {
  lifecycle: ServiceLifecycleConfig
}

export interface SessionAutoArchiveConfig {
  enabled: boolean
  days: number
}

export interface SessionAutoCleanupConfig {
  enabled: boolean
  archive_retention_days: number
}

export interface SessionConfig {
  default_name_pattern: string
  default_execution_mode: string
  auto_title_enabled?: boolean
  auto_title_max_input_tokens?: number
  auto_snapshot_before_compress?: boolean
  max_snapshots_per_session: number
  auto_archive: SessionAutoArchiveConfig
  auto_cleanup: SessionAutoCleanupConfig
  timeout?: {
    idle_timeout_seconds?: number
    max_duration_seconds?: number
  }
  concurrency?: {
    max_active_sessions?: number
    max_messages_per_session?: number
  }
  recovery?: {
    auto_save_interval_seconds?: number
    enable_crash_recovery?: boolean
  }
}

export interface MainPanelConfig {
  visible?: boolean
  floating?: boolean
  area: number
  geometry: Record<string, unknown> | null
  tabified?: boolean
  tabified_with: string | null
}

export interface UIConfig {
  main_panel: MainPanelConfig
}

export interface BackendConfig {
  ws_url?: string
}

export interface QGISRecentProject {
  path: string
  name: string
  last_opened: string
}

export interface QGISConfig {
  executable_path: string
  recent_projects: QGISRecentProject[]
}

export interface IntegrationsConfig {
  osm: {
    enabled: boolean
    endpoints: string[]
    nominatim_url: string
    proxy_url: string
    timeout_seconds: number
    total_timeout_seconds: number
    max_attempts: number
    max_response_mb: number
  }
  gee: {
    enabled: boolean
    python: string
    project: string
    proxy_url: string
    timeout_seconds: number
  }
  browser: {
    enabled: boolean
    command: string
    args: string[]
    browser: 'chrome' | 'msedge' | 'chromium'
    headless: boolean
    timeout_seconds: number
  }
  web: {
    enabled: boolean
    timeout_seconds: number
    max_download_mb: number
  }
}

export interface AppConfig {
  llm: LLMConfig
  ports: PortsConfig
  backend?: BackendConfig
  log: LogConfig
  agent: AgentRuntimeConfig
  performance: PerformanceConfig
  compute: ComputeConfig
  services: ServicesConfig
  session: SessionConfig
  ui: UIConfig
  qgis?: QGISConfig
  integrations?: IntegrationsConfig
  data_dir: string
  [key: string]: unknown
}
