import { create } from "zustand";
import api from "@/lib/api";
import type { ServicePortsMap } from "@/lib/backendRuntime";
import { getBackendRuntime, isDesktopRuntime } from "@/lib/tauri";

export const HEALTH_CHECK_INTERVAL_MS = 60000;
const BACKEND_URL_ENV_KEYS = [
  "NEXT_PUBLIC_AUTOCARTO_BACKEND_URL",
  "NEXT_PUBLIC_API_URL",
] as const;
const WS_URL_ENV_KEYS = [
  "NEXT_PUBLIC_AUTOCARTO_WS_URL",
  "NEXT_PUBLIC_WS_URL",
] as const;

function normalizeConfiguredBackendUrl(raw: unknown) {
  if (typeof raw !== "string") {
    return "";
  }
  const trimmed = raw.trim();
  if (!trimmed) {
    return "";
  }
  try {
    const parsed = new URL(trimmed);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      return "";
    }
    return parsed.origin;
  } catch {
    return "";
  }
}

function normalizeConfiguredWebSocketUrl(raw: unknown) {
  if (typeof raw !== "string") {
    return "";
  }
  const trimmed = raw.trim();
  if (!trimmed) {
    return "";
  }
  try {
    const parsed = new URL(trimmed);
    if (parsed.protocol !== "ws:" && parsed.protocol !== "wss:") {
      return "";
    }
    return parsed.toString().replace(/\/$/, "");
  } catch {
    return "";
  }
}

function deriveWebSocketUrl(backendUrl: string, override?: string) {
  const normalizedOverride = normalizeConfiguredWebSocketUrl(override);
  if (normalizedOverride) {
    return normalizedOverride;
  }
  try {
    const parsed = new URL(backendUrl);
    parsed.protocol = parsed.protocol === "https:" ? "wss:" : "ws:";
    parsed.pathname = "/ws";
    parsed.search = "";
    parsed.hash = "";
    return parsed.toString().replace(/\/$/, "");
  } catch {
    return "";
  }
}

function readFirstConfiguredEnv(keys: readonly string[]) {
  for (const key of keys) {
    const value = process.env[key];
    if (typeof value === "string" && value.trim()) {
      return value.trim();
    }
  }
  return "";
}

function buildWebSocketRuntimeSelection(backendUrlOverride?: string) {
  const backendUrl =
    normalizeConfiguredBackendUrl(backendUrlOverride) ||
    normalizeConfiguredBackendUrl(readFirstConfiguredEnv(BACKEND_URL_ENV_KEYS));
  return {
    backendUrl,
    wsUrl: backendUrl
      ? deriveWebSocketUrl(backendUrl, readFirstConfiguredEnv(WS_URL_ENV_KEYS))
      : normalizeConfiguredWebSocketUrl(readFirstConfiguredEnv(WS_URL_ENV_KEYS)),
  };
}

async function resolveRuntimeSelection(backendUrlOverride?: string) {
  if (isDesktopRuntime()) {
    const runtime = await getBackendRuntime();
    return {
      backendUrl: runtime.backendUrl,
      wsUrl: runtime.wsUrl,
      startupError: runtime.startupError || "",
    };
  }
  return {
    ...buildWebSocketRuntimeSelection(backendUrlOverride),
    startupError: "",
  };
}

interface ConnectionState {
  connected: boolean;
  backendReady: boolean;
  backendUrl: string;
  wsUrl: string;
  startupError: string;
  authoritativeStartupError: string;
  servicePorts: ServicePortsMap;
  setConnected: (connected: boolean) => void;
  setBackendUrl: (url: string) => void;
  setWsUrl: (url: string) => void;
  initializeRuntime: () => Promise<void>;
  checkHealth: () => Promise<boolean>;
  waitForBackend: () => Promise<boolean>;
  refreshServicePorts: () => Promise<void>;
}

export const useConnectionStore = create<ConnectionState>((set, get) => ({
  connected: false,
  backendReady: false,
  backendUrl: "",
  wsUrl: "",
  startupError: "",
  authoritativeStartupError: "",
  servicePorts: {},
  setConnected: (connected) => set({ connected }),
  setBackendUrl: (backendUrl) => {
    const runtime = buildWebSocketRuntimeSelection(backendUrl);
    api.setBaseURL(runtime.backendUrl);
    set({
      backendUrl: runtime.backendUrl,
      wsUrl: runtime.wsUrl,
      startupError: "",
      authoritativeStartupError: "",
    });
  },
  setWsUrl: (wsUrl) => set({ wsUrl: normalizeConfiguredWebSocketUrl(wsUrl) || wsUrl }),

  initializeRuntime: async () => {
    const runtime = await resolveRuntimeSelection();
    api.setBaseURL(runtime.backendUrl);
    set({
      backendReady: true,
      backendUrl: runtime.backendUrl,
      wsUrl: runtime.wsUrl,
      startupError: runtime.startupError,
      authoritativeStartupError: runtime.startupError,
      servicePorts: {},
    });
  },

  checkHealth: async () => {
    const wsUrl = get().wsUrl.trim();
    if (!wsUrl) {
      const runtime = await resolveRuntimeSelection(get().backendUrl);
      api.setBaseURL(runtime.backendUrl);
      set({
        backendReady: true,
        backendUrl: runtime.backendUrl,
        wsUrl: runtime.wsUrl,
        startupError: runtime.startupError,
        authoritativeStartupError: runtime.startupError,
      });
      return true;
    }
    set({
      backendReady: true,
      startupError: get().authoritativeStartupError.trim(),
    });
    return true;
  },

  waitForBackend: async () => {
    const ok = await get().checkHealth();
    if (ok) {
      await get().refreshServicePorts();
    }
    return ok;
  },

  refreshServicePorts: async () => {
    set({ servicePorts: {} });
  },
}));
