import { beforeEach, describe, expect, it, vi } from "vitest";

import { buildLegacyDescriptorUnsupportedStartupError } from "@/test-support/runtimeContract";

const invokeMock = vi.fn();

vi.mock("@tauri-apps/api/core", () => ({
  invoke: invokeMock,
}));

describe("desktop runtime descriptor", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    invokeMock.mockReset();
    Object.assign(window, {
      __TAURI__: {},
    });
  });

  it("rejects runtime payloads that omit runtime port-file urls", async () => {
    invokeMock.mockResolvedValueOnce({
      port: 3000,
      backend_url: "",
      ws_url: "",
      startup_error: null,
    });

    const { getBackendRuntime } = await import("@/lib/tauri");

    await expect(getBackendRuntime()).rejects.toThrow(/runtime port file/i);
  });

  it("accepts authoritative degraded descriptors and preserves startup_error", async () => {
    invokeMock.mockResolvedValueOnce({
      port: 50002,
      backend_url: "http://127.0.0.1:50002",
      ws_url: "ws://127.0.0.1:50002/ws",
      startup_error: "discovery descriptor 未就绪: readiness=degraded",
    });

    const { getBackendRuntime } = await import("@/lib/tauri");

    await expect(getBackendRuntime()).resolves.toMatchObject({
      port: 50002,
      backendUrl: "http://127.0.0.1:50002",
      wsUrl: "ws://127.0.0.1:50002/ws",
      startupError: "discovery descriptor 未就绪: readiness=degraded",
    });
  });

  it("surfaces legacy descriptor protocol failures from startup_error", async () => {
    invokeMock.mockResolvedValueOnce({
      port: 3000,
      backend_url: "",
      ws_url: "",
      startup_error: buildLegacyDescriptorUnsupportedStartupError(),
    });

    const { getBackendRuntime } = await import("@/lib/tauri");

    await expect(getBackendRuntime()).rejects.toThrow(/contract version 不受支持: 1/i);
  });
  it("treats Tauri user agent as desktop runtime signal", async () => {
    const originalDescriptor = Object.getOwnPropertyDescriptor(
      window.navigator,
      "userAgent",
    );
    delete (window as Window & { __TAURI__?: unknown }).__TAURI__;
    Object.defineProperty(window.navigator, "userAgent", {
      value: "Mozilla/5.0 AutoCarto Tauri/2.0",
      configurable: true,
    });
    invokeMock.mockResolvedValueOnce({
      port: 3000,
      backend_url: "http://127.0.0.1:3000",
      ws_url: "ws://127.0.0.1:3000/ws",
      startup_error: null,
    });

    try {
      const { isDesktopRuntime, getBackendRuntime } = await import("@/lib/tauri");
      expect(isDesktopRuntime()).toBe(true);
      await expect(getBackendRuntime()).resolves.toMatchObject({
        port: 3000,
        backendUrl: "http://127.0.0.1:3000",
        wsUrl: "ws://127.0.0.1:3000/ws",
      });
    } finally {
      if (originalDescriptor) {
        Object.defineProperty(window.navigator, "userAgent", originalDescriptor);
      }
    }
  });
});
