import { invoke } from "@tauri-apps/api/core";

const DESKTOP_ONLY_ERROR =
  "当前运行环境不是 AutoCarto 桌面端，请在桌面应用中使用该功能。";

const RUNTIME_PORT_REQUIRED_ERROR =
  "桌面运行时缺少 runtime port file，请等待 carto-agent 写入 data/.port 后重试。";

function hasTauriRuntime() {
  if (typeof window === "undefined") {
    return false;
  }
  const runtime = window as Window & {
    __TAURI_INTERNALS__?: unknown;
    __TAURI__?: unknown;
  };
  if (runtime.__TAURI_INTERNALS__ || runtime.__TAURI__) {
    return true;
  }
  const userAgent = window.navigator?.userAgent || "";
  return /tauri/i.test(userAgent);
}

function ensureTauriRuntime() {
  if (!hasTauriRuntime()) {
    throw new Error(DESKTOP_ONLY_ERROR);
  }
}

export function isDesktopRuntime() {
  return hasTauriRuntime();
}

export interface BackendRuntimeInfo {
  port: number;
  backend_url: string;
  ws_url: string;
  startup_error?: string | null;
}

export async function detectQGISExecutable() {
  ensureTauriRuntime();
  return invoke<string>("detect_qgis_executable");
}

export async function launchQGIS(options?: {
  executablePath?: string | null;
  projectPath?: string | null;
  forceNewInstance?: boolean;
}) {
  ensureTauriRuntime();
  return invoke<string>("launch_qgis", {
    executablePath: options?.executablePath ?? null,
    projectPath: options?.projectPath ?? null,
    forceNewInstance: options?.forceNewInstance ?? false,
  });
}

export async function pickQGISProject() {
  ensureTauriRuntime();
  return invoke<string | null>("pick_qgis_project");
}

export async function pickQGISProjectDirectory() {
  ensureTauriRuntime();
  return invoke<string | null>("pick_qgis_project_directory");
}

export async function pickQGISExecutable() {
  ensureTauriRuntime();
  return invoke<string | null>("pick_qgis_executable");
}

export async function getBackendRuntime() {
  ensureTauriRuntime();
  const runtime = await invoke<BackendRuntimeInfo>("get_backend_runtime");
  const port = Number(runtime?.port);
  const backendUrl =
    typeof runtime?.backend_url === "string" ? runtime.backend_url.trim() : "";
  const wsUrl = typeof runtime?.ws_url === "string" ? runtime.ws_url.trim() : "";
  const startupError =
    typeof runtime?.startup_error === "string" && runtime.startup_error.trim()
      ? runtime.startup_error.trim()
      : "";
  if (!Number.isInteger(port) || port <= 0 || port > 65535 || !backendUrl || !wsUrl) {
    throw new Error(startupError || RUNTIME_PORT_REQUIRED_ERROR);
  }
  return {
    port,
    backendUrl,
    wsUrl,
    startupError,
  };
}
