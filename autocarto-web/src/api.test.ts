import { afterEach, describe, expect, test, vi } from "vitest";

import {
  ApiClient,
  ApiError,
  buildControlContractHeaders,
  HTTP_CONTROL_CONTRACT_VERSION,
  HTTP_CONTROL_CONTRACT_VERSION_HEADER,
  normalizeToolCatalogResponse,
  type ToolCatalogResponse,
} from "@/lib/api";
import {
  buildUnsupportedControlContractData,
  LEGACY_RUNTIME_CONTRACT_VERSION,
} from "@/test-support/runtimeContract";

describe("ApiClient structured error parsing", () => {
  test("preserves optional metadata when an endpoint requests the envelope", async () => {
    const envelope = { success: true, data: [], metadata: { page: 1 } };
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async () => new Response(JSON.stringify(envelope), { headers: { 'Content-Type': 'application/json' } })));
    const client = new ApiClient('http://localhost:3000');
    expect(await client.postEnvelope('/api/guides', { values: {} })).toEqual(envelope);
    expect(await client.post('/api/guides', { values: {} })).toEqual([]);
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  async function expectApiError(promise: Promise<unknown>) {
    return promise.then(
      () => {
        throw new Error("expected ApiError");
      },
      (error: unknown) => {
        expect(error).toBeInstanceOf(ApiError);
        return error as ApiError;
      },
    );
  }

  test("preserves plan resource changes returned in error details", async () => {
    const details = { resource_change: { guide_id: "route", can_continue_snapshot: true } };
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
      success: false, code: "PLAN_RESOURCE_CHANGED", error: "指南资源已变化", details,
    }), { status: 409, headers: { "Content-Type": "application/json" } })));
    const error = await expectApiError(new ApiClient("http://localhost:3000").post("/api/sessions/s/plans/p/start", { version: 1 }));
    expect(error.code).toBe("PLAN_RESOURCE_CHANGED");
    expect(error.data).toEqual(details);
  });

  test("preserves structured envelope for non-ok responses", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            success: false,
            code: "HTTP_CONTRACT_VERSION_UNSUPPORTED",
            error: {
              code: "HTTP_CONTRACT_VERSION_UNSUPPORTED",
              message: "不支持的 HTTP contract 版本",
              origin: "control",
              stage: "runtime_contract",
              retryable: false,
              degraded: false,
              user_action: "请升级客户端后重试。",
              correlation_id: "corr-test-1",
              evidence_id: "evid-test-1",
              debug_context: {
                requested_contract_version: "99",
              },
            },
            data: {
              contract_version: HTTP_CONTROL_CONTRACT_VERSION,
              requested_contract_version: "99",
              supported_contract_versions: [HTTP_CONTROL_CONTRACT_VERSION],
            },
          }),
          {
            status: 400,
            headers: { "Content-Type": "application/json" },
          },
        ),
      ),
    );

    const client = new ApiClient("http://localhost:3000");
    const error = await expectApiError(client.get("/api/runtime/snapshot"));

    expect(error.message).toBe("不支持的 HTTP contract 版本");
    expect(error.code).toBe("HTTP_CONTRACT_VERSION_UNSUPPORTED");
    expect(error.envelope?.origin).toBe("control");
    expect(error.envelope?.stage).toBe("runtime_contract");
    expect(error.envelope?.debug_context?.requested_contract_version).toBe("99");
    expect(error.data).toEqual({
      contract_version: HTTP_CONTROL_CONTRACT_VERSION,
      requested_contract_version: "99",
      supported_contract_versions: [HTTP_CONTROL_CONTRACT_VERSION],
    });
  });

  test("overwrites legacy http contract version headers with v1", () => {
    const headers = buildControlContractHeaders({
      [HTTP_CONTROL_CONTRACT_VERSION_HEADER]: LEGACY_RUNTIME_CONTRACT_VERSION,
    });

    expect(headers.get(HTTP_CONTROL_CONTRACT_VERSION_HEADER)).toBe(
      HTTP_CONTROL_CONTRACT_VERSION,
    );
  });

  test("preserves structured envelope for success-false responses", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            success: false,
            code: "HTTP_CONTRACT_VERSION_UNSUPPORTED",
            error: {
              code: "HTTP_CONTRACT_VERSION_UNSUPPORTED",
              message: "需要匹配的 contract 版本",
              origin: "control",
              stage: "runtime_contract",
              retryable: false,
              degraded: false,
              user_action: "请改用支持的版本。",
              correlation_id: "corr-test-2",
              evidence_id: "evid-test-2",
              debug_context: {},
            },
            data: {
              contract_version: HTTP_CONTROL_CONTRACT_VERSION,
            },
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        ),
      ),
    );

    const client = new ApiClient("http://localhost:3000");
    const error = await expectApiError(client.get("/api/runtime/services"));

    expect(error.message).toBe("需要匹配的 contract 版本");
    expect(error.envelope?.correlation_id).toBe("corr-test-2");
    expect(error.envelope?.evidence_id).toBe("evid-test-2");
  });

  test("sends authoritative http contract version header on requests", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ success: true, data: { ok: true } }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("http://localhost:3000");
    await client.get("/api/runtime/snapshot");

    const options = fetchMock.mock.calls[0]?.[1] as RequestInit | undefined;
    const headers = new Headers(options?.headers);
    expect(headers.get(HTTP_CONTROL_CONTRACT_VERSION_HEADER)).toBe(
      HTTP_CONTROL_CONTRACT_VERSION,
    );
  });

  test("sends the Image 2 base URL in save and connection requests", async () => {
    const fetchMock = vi.fn().mockImplementation(async () =>
      new Response(
        JSON.stringify({
          success: true,
          data: {
            scope: "image2",
            persisted: true,
            status: "ok",
            model: "gpt-image-2",
          },
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("http://localhost:3000");
    await client.saveImage2Config({
      enabled: true,
      base_url: "https://image2.example.com/v1",
      api_key: "sk-image2",
    });
    await client.testImage2Connection({
      base_url: "https://image2.example.com/v1",
      api_key: "sk-image2",
    });

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "http://localhost:3000/api/image2/config",
      expect.objectContaining({
        method: "PUT",
        body: JSON.stringify({
          enabled: true,
          base_url: "https://image2.example.com/v1",
          api_key: "sk-image2",
        }),
      }),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "http://localhost:3000/api/image2/test",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          base_url: "https://image2.example.com/v1",
          api_key: "sk-image2",
        }),
      }),
    );
  });

  test("preserves structured envelope for settings workspace legacy version mismatch", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            success: false,
            code: "HTTP_CONTRACT_VERSION_UNSUPPORTED",
            error: {
              code: "HTTP_CONTRACT_VERSION_UNSUPPORTED",
              message: "设置工作区 contract 版本不受支持",
              origin: "control",
              stage: "runtime_contract",
              retryable: false,
              degraded: false,
              user_action: "请升级客户端后重试。",
              correlation_id: "corr-settings-1",
              evidence_id: "evid-settings-1",
            },
            data: buildUnsupportedControlContractData(
              LEGACY_RUNTIME_CONTRACT_VERSION,
            ),
          }),
          {
            status: 400,
            headers: { "Content-Type": "application/json" },
          },
        ),
      ),
    );

    const client = new ApiClient("http://localhost:3000");
    const error = await expectApiError(client.getSettingsWorkspace());

    expect(error.code).toBe("HTTP_CONTRACT_VERSION_UNSUPPORTED");
    expect(error.envelope?.correlation_id).toBe("corr-settings-1");
    expect(error.data).toEqual(
      buildUnsupportedControlContractData(LEGACY_RUNTIME_CONTRACT_VERSION),
    );
  });

  test("returns concise diagnostics when /api endpoint responds with html", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response("<!doctype html><html><body>404</body></html>", {
          status: 404,
          headers: { "Content-Type": "text/html; charset=utf-8" },
        }),
      ),
    );

    const client = new ApiClient("");
    const error = await expectApiError(client.getSettingsWorkspace());

    expect(error.status).toBe(404);
    expect(error.message).toContain("/api/settings/workspace returned HTML");
    expect(error.message).not.toContain("<!doctype html>");
  });

  test("normalizes instances snapshot while sending authoritative contract headers", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          success: true,
          data: {
            instances: [{ id: "qgis-1", status: "running" }],
            qgis_available: true,
            instance_count: 1,
          },
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("http://localhost:3000");
    const snapshot = await client.getInstanceSnapshot();

    expect(snapshot.qgis_available).toBe(true);
    expect(snapshot.instance_count).toBe(1);
    expect(snapshot.instances).toHaveLength(1);

    const options = fetchMock.mock.calls[0]?.[1] as RequestInit | undefined;
    const headers = new Headers(options?.headers);
    expect(headers.get(HTTP_CONTROL_CONTRACT_VERSION_HEADER)).toBe(
      HTTP_CONTROL_CONTRACT_VERSION,
    );
  });

  test("normalizes valid empty catalog semantics from authoritative contract", () => {
    const payload: Partial<ToolCatalogResponse> = {
      scope: "global",
      config_name: "default",
      runtime_revision: "runtime-rev-empty",
      catalog: {
        id: "tool_catalog/global",
        version: "1",
        source: "go_control_plane",
        capability_type: "tool_catalog",
        runtime_target: "mixed",
        availability: "valid_empty",
        degradation_status: "none",
        policy: {
          scope: "global",
          config_name: "default",
        },
        items: [],
      },
    };
    const normalized = normalizeToolCatalogResponse(payload);

    expect(normalized.catalog.availability).toBe("valid_empty");
    expect(normalized.catalog.degradation_status).toBe("none");
    expect(normalized.tool_infos).toEqual([]);
    expect(normalized.tools).toEqual([]);
  });

  test("normalizes degraded provider-missing catalog semantics from authoritative contract", () => {
    const payload: Partial<ToolCatalogResponse> = {
      scope: "global",
      config_name: "default",
      runtime_revision: "runtime-rev-provider-missing",
      catalog: {
        id: "tool_catalog/global",
        version: "1",
        source: "go_control_plane",
        capability_type: "tool_catalog",
        runtime_target: "mixed",
        availability: "degraded",
        missing_dependency_reason: "provider_missing",
        degradation_status: "provider_missing",
        policy: {
          scope: "global",
          config_name: "default",
        },
        items: [
          {
            id: "output_init_dir",
            source: "compute-provider",
            capability_type: "tool",
            runtime_target: "cli",
            availability: "unavailable",
            missing_dependency_reason: "provider_missing",
            degradation_status: "provider_missing",
            evidence_id: "EVIDENCE-7001",
            correlation_id: "CORR-7001",
            policy: {
              scope: "global",
            },
          },
        ],
      },
    };
    const normalized = normalizeToolCatalogResponse(payload);

    expect(normalized.catalog.degradation_status).toBe("provider_missing");
    expect(normalized.tool_infos).toHaveLength(1);
    expect(normalized.tool_infos[0]).toMatchObject({
      name: "output_init_dir",
      backend: "cli",
      available: false,
      missing_reason: "provider_missing",
      evidence_id: "EVIDENCE-7001",
      correlation_id: "CORR-7001",
    });
  });

  test("preserves hard-failure catalog semantics in ApiError payload", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            success: false,
            code: "TOOL_CATALOG_UNSUPPORTED_RUNTIME",
            error: {
              code: "TOOL_CATALOG_UNSUPPORTED_RUNTIME",
              message: "当前运行时不支持该工具目录",
              origin: "catalog",
              stage: "catalog_api",
              retryable: false,
              degraded: false,
            },
            data: {
              scope: "global",
              config_name: "default",
              runtime_revision: "runtime-rev-unsupported",
              catalog: {
                id: "tool_catalog/global",
                version: "1",
                source: "go_control_plane",
                capability_type: "tool_catalog",
                runtime_target: "mixed",
                availability: "unsupported",
                missing_dependency_reason: "unsupported_runtime",
                degradation_status: "unsupported_runtime",
                policy: {
                  scope: "global",
                  config_name: "default",
                },
                items: [
                  {
                    id: "wasm_render_map",
                    source: "wasm-provider",
                    capability_type: "tool",
                    runtime_target: "wasm",
                    availability: "unsupported",
                    missing_dependency_reason: "unsupported_runtime",
                    degradation_status: "unsupported_runtime",
                    policy: {
                      scope: "global",
                    },
                  },
                ],
              },
            },
          }),
          {
            status: 503,
            headers: { "Content-Type": "application/json" },
          },
        ),
      ),
    );

    const client = new ApiClient("http://localhost:3000");
    const error = await expectApiError(client.get("/api/tools/catalog"));
    const normalized = normalizeToolCatalogResponse(
      error.data as Partial<ToolCatalogResponse>,
    );

    expect(error.code).toBe("TOOL_CATALOG_UNSUPPORTED_RUNTIME");
    expect(normalized.catalog.degradation_status).toBe("unsupported_runtime");
    expect(normalized.tool_infos[0]).toMatchObject({
      name: "wasm_render_map",
      backend: "wasm",
      available: false,
      missing_reason: "unsupported_runtime",
    });
  });
});
