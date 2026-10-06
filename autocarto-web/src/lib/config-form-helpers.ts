import { invoke } from "@tauri-apps/api/core";

import { asRecord, asString, clampInt } from "@/lib/config-utils";

export const TIMESTAMP_PATTERN = "会话 MM-DD HH:mm";
export const PROJECT_SEQ_PATTERN = "项目名-序号";
export const DEFAULT_CUSTOM_NAME_PATTERN = "会话 {date}";

export type NamePatternMode = "timestamp" | "project-seq" | "custom";

export type StaticProvider = {
  id: string;
  base_url: string;
};

const DESKTOP_ONLY_ERROR =
  "当前运行环境不是 AutoCarto 桌面端，请在桌面应用中使用该功能。";

export function isDesktopRuntime() {
  if (typeof window === "undefined") {
    return false;
  }
  const runtime = window as Window & {
    __TAURI_INTERNALS__?: unknown;
    __TAURI__?: unknown;
  };
  return Boolean(runtime.__TAURI_INTERNALS__ || runtime.__TAURI__);
}

function ensureDesktopRuntime() {
  if (!isDesktopRuntime()) {
    throw new Error(DESKTOP_ONLY_ERROR);
  }
}

export async function detectQGISExecutable() {
  ensureDesktopRuntime();
  return invoke<string>("detect_qgis_executable");
}

export async function pickQGISExecutable() {
  ensureDesktopRuntime();
  return invoke<string | null>("pick_qgis_executable");
}

export async function pickFilePath() {
  ensureDesktopRuntime();
  return invoke<string | null>("pick_file_path");
}

export async function pickDirectoryPath() {
  ensureDesktopRuntime();
  return invoke<string | null>("pick_directory_path");
}

export function parseRange(
  value: unknown,
  fallback: [number, number],
): [number, number] {
  if (!Array.isArray(value) || value.length < 2) {
    return fallback;
  }
  const min = clampInt(Number(value[0] ?? fallback[0]), 1, 65535);
  const max = clampInt(Number(value[1] ?? fallback[1]), 1, 65535);
  return [min, max];
}

export function parseArgs(text: string): string[] {
  return text
    .split(/\n/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export function formatArgs(args: unknown): string {
  if (!Array.isArray(args)) {
    return "";
  }
  return args
    .map((item) => String(item || "").trim())
    .filter(Boolean)
    .join("\n");
}

export function parseStaticProvidersText(text: string): StaticProvider[] {
  const providers: StaticProvider[] = [];
  text
    .split(/\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .forEach((line) => {
      const eqIndex = line.indexOf("=");
      const commaIndex = line.indexOf(",");
      const splitIndex = eqIndex >= 0 ? eqIndex : commaIndex;
      if (splitIndex < 1) {
        return;
      }
      const id = line.slice(0, splitIndex).trim();
      const baseURL = line.slice(splitIndex + 1).trim();
      if (!id || !baseURL) {
        return;
      }
      providers.push({ id, base_url: baseURL });
    });
  return providers;
}

export function formatStaticProviders(value: unknown): string {
  if (!Array.isArray(value)) {
    return "";
  }
  const lines = value
    .map((item) => {
      const record = asRecord(item);
      const id = asString(record.id, "");
      const baseURL = asString(record.base_url, "");
      if (!id || !baseURL) {
        return "";
      }
      return `${id}=${baseURL}`;
    })
    .filter(Boolean);
  return lines.join("\n");
}

export function inferPatternMode(rawPattern: string): {
  mode: NamePatternMode;
  custom: string;
} {
  const pattern = rawPattern.trim();
  if (!pattern || pattern === TIMESTAMP_PATTERN) {
    return { mode: "timestamp", custom: DEFAULT_CUSTOM_NAME_PATTERN };
  }
  if (pattern === PROJECT_SEQ_PATTERN) {
    return { mode: "project-seq", custom: DEFAULT_CUSTOM_NAME_PATTERN };
  }
  return { mode: "custom", custom: pattern };
}

export function resolvePattern(form: {
  namePatternMode: NamePatternMode;
  customNamePattern: string;
}) {
  if (form.namePatternMode === "timestamp") {
    return TIMESTAMP_PATTERN;
  }
  if (form.namePatternMode === "project-seq") {
    return PROJECT_SEQ_PATTERN;
  }
  return form.customNamePattern.trim() || TIMESTAMP_PATTERN;
}
