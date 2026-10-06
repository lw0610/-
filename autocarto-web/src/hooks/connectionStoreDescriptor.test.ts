import { beforeEach, describe, expect, it, vi } from "vitest";

describe("connection store discovery descriptor", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.resetModules();
  });

  it("starts without legacy default runtime urls", async () => {
    const { useConnectionStore } = await import("@/stores/connectionStore");

    expect(useConnectionStore.getState().backendUrl).toBe("");
    expect(useConnectionStore.getState().wsUrl).toBe("");
  });

  it("initializes directly to the websocket runtime without descriptor loading", async () => {
    delete process.env.NEXT_PUBLIC_AUTOCARTO_BACKEND_URL;
    delete process.env.NEXT_PUBLIC_API_URL;
    delete process.env.NEXT_PUBLIC_AUTOCARTO_WS_URL;
    delete process.env.NEXT_PUBLIC_WS_URL;
    const { useConnectionStore } = await import("@/stores/connectionStore");

    await useConnectionStore.getState().initializeRuntime();

    expect(useConnectionStore.getState().backendUrl).toBe("");
    expect(useConnectionStore.getState().wsUrl).toBe("");
    expect(useConnectionStore.getState().startupError).toBe("");
  });

  it("marks websocket runtime ready without HTTP probing", async () => {
    const { useConnectionStore } = await import("@/stores/connectionStore");
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    useConnectionStore.setState({
      backendUrl: "http://127.0.0.1:50321",
      wsUrl: "ws://127.0.0.1:50321/ws",
      startupError: "",
    });

    const ok = await useConnectionStore.getState().checkHealth();

    expect(ok).toBe(true);
    expect(fetchMock).not.toHaveBeenCalled();
    expect(useConnectionStore.getState().backendReady).toBe(true);
  });
});
