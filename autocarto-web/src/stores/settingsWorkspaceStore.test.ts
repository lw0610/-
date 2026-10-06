import { test, expect, vi, beforeEach, afterEach } from "vitest";
import { useSettingsWorkspaceStore } from "./settingsWorkspaceStore";
import api from "@/lib/api";
import { emptySettingsWorkspaceMetadata } from "@/lib/settingsRegistry";

vi.mock("@/lib/api", () => {
  return {
    default: {
      getSettingsWorkspace: vi.fn(),
    },
  };
});

beforeEach(() => {
  useSettingsWorkspaceStore.setState({
    metadata: emptySettingsWorkspaceMetadata,
    loading: false,
    loaded: false,
    error: undefined,
  });
});

afterEach(() => {
  vi.clearAllMocks();
});

test("store enforces authority-only semantics on successful fetch", async () => {
  const mockPayload = {
    tasks: [
      {
        key: "system",
        href: "/settings/system",
        label: "System",
        routes: ["/settings/system"],
      },
    ],
    descriptors: [
      {
        id: "system-log-dir",
        task: "system",
        section: "System",
        path: "/settings/system",
        label: "Log Dir",
      },
    ],
  };

  vi.mocked(api.getSettingsWorkspace).mockResolvedValueOnce(mockPayload as Record<string, unknown>);

  await useSettingsWorkspaceStore.getState().ensureLoaded();

  const state = useSettingsWorkspaceStore.getState();
  expect(state.loaded).toBe(true);
  expect(state.error).toBeUndefined();
  expect(state.metadata.tasks.length).toBe(1);
  expect(state.metadata.tasks[0].key).toBe("system");
  expect(state.metadata.descriptors.length).toBe(1);
  expect(state.metadata.descriptors[0].id).toBe("system-log-dir");
});

test("store does not synthesize complete workspace on fetch failure", async () => {
  vi.mocked(api.getSettingsWorkspace).mockRejectedValueOnce(
    new Error("Network Error")
  );

  await useSettingsWorkspaceStore.getState().ensureLoaded();

  const state = useSettingsWorkspaceStore.getState();
  expect(state.loaded).toBe(true);
  expect(state.error).toBe("Network Error");
  expect(state.metadata).toEqual(emptySettingsWorkspaceMetadata);
  expect(state.metadata.tasks.length).toBe(0);
});

test("store handles explicit empty payload without fabricating default workspace", async () => {
  const emptyPayload = { tasks: [], descriptors: [] };
  vi.mocked(api.getSettingsWorkspace).mockResolvedValueOnce(emptyPayload as Record<string, unknown>);

  await useSettingsWorkspaceStore.getState().ensureLoaded();

  const state = useSettingsWorkspaceStore.getState();
  expect(state.loaded).toBe(true);
  expect(state.error).toBe("settings workspace contract unavailable");
  expect(state.metadata).toEqual(emptySettingsWorkspaceMetadata);
  expect(state.metadata.tasks.length).toBe(0);
});

test("store preserves read-only mutability from backend payload", async () => {
  const mockPayload = {
    tasks: [
      {
        key: "services",
        href: "/settings/services",
        label: "服务状态",
        routes: ["/settings/services"],
      },
    ],
    descriptors: [
      {
        id: "services-runtime",
        task: "services",
        section: "服务状态",
        path: "/settings/services",
        label: "运行态服务",
        impact: "read_only",
      },
    ],
  };

  vi.mocked(api.getSettingsWorkspace).mockResolvedValueOnce(mockPayload as Record<string, unknown>);

  await useSettingsWorkspaceStore.getState().ensureLoaded();

  const state = useSettingsWorkspaceStore.getState();
  expect(state.loaded).toBe(true);
  expect(state.error).toBeUndefined();
  expect(state.metadata.descriptors[0]).toMatchObject({
    id: "services-runtime",
    impact: "read_only",
    mutability: "read_only",
  });
});

test("store retries loading after transient fetch failure", async () => {
  vi.mocked(api.getSettingsWorkspace)
    .mockRejectedValueOnce(new Error("startup race"))
    .mockResolvedValueOnce({
      tasks: [
        {
          key: "system",
          href: "/settings/system",
          label: "System",
          routes: ["/settings/system"],
        },
      ],
      descriptors: [
        {
          id: "system-log-dir",
          task: "system",
          section: "System",
          path: "/settings/system",
          label: "Log Dir",
        },
      ],
    } as Record<string, unknown>);

  await useSettingsWorkspaceStore.getState().ensureLoaded();
  expect(useSettingsWorkspaceStore.getState().error).toBe("startup race");

  await useSettingsWorkspaceStore.getState().ensureLoaded();

  const state = useSettingsWorkspaceStore.getState();
  expect(state.loaded).toBe(true);
  expect(state.error).toBeUndefined();
  expect(state.metadata.tasks.length).toBe(1);
  expect(state.metadata.descriptors.length).toBe(1);
  expect(api.getSettingsWorkspace).toHaveBeenCalledTimes(2);
});
