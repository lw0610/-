import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, HTTP_CONTROL_CONTRACT_VERSION_HEADER } from "./api";

describe("integration diagnostics API", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("posts only the saved service selector and retains the control contract header", async () => {
    const diagnostic = { service: "gee", success: false, error: "Authentication required", result: { initialized: false } };
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ success: true, data: diagnostic }), { headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    const result = await new ApiClient("http://localhost:8080").checkIntegration("gee", 300);
    expect(result).toEqual(diagnostic);
    const [url, options] = fetchMock.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/integrations/check");
    expect(options.method).toBe("POST");
    expect(JSON.parse(options.body)).toEqual({ service: "gee" });
    expect(options.headers.get(HTTP_CONTROL_CONTRACT_VERSION_HEADER)).toBe("v1");
    expect(options.signal).toBeInstanceOf(AbortSignal);
  });
});
