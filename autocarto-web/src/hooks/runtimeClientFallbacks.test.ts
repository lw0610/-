import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { WebSocketClient } from "@/lib/websocket";

describe("runtime client legacy fallback removal", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.resetModules();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("does not default api requests to legacy port 3000", async () => {
    const fetchMock = vi.fn(async () => ({
      ok: true,
      json: async () => ({ success: true, data: { ok: true } }),
    }));
    vi.stubGlobal("fetch", fetchMock);

    const { default: api } = await import("@/lib/api");
    await api.get<{ ok: boolean }>("/api/health");

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/health");
  });

  it("does not default websocket connections to legacy port 3000", () => {
    class FakeWebSocket {
      static CONNECTING = 0;
      static OPEN = 1;
      static lastUrl = "__unset__";

      readyState = FakeWebSocket.CONNECTING;
      onopen: (() => void) | null = null;
      onmessage: ((event: MessageEvent) => void) | null = null;
      onclose: (() => void) | null = null;
      onerror: (() => void) | null = null;

      constructor(url: string) {
        FakeWebSocket.lastUrl = url;
      }

      close() {}

      send() {}
    }

    vi.stubGlobal("WebSocket", FakeWebSocket);

    const client = new WebSocketClient();
    client.connect();

    expect(FakeWebSocket.lastUrl).toBe("");
  });
});
