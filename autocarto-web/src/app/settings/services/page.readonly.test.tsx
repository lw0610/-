import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { expect, test, vi } from "vitest";

import ServicesSettingsPage from "./page";

const { fetchSnapshotMock } = vi.hoisted(() => ({
  fetchSnapshotMock: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("@/components/layout/AppShell", () => ({
  default: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

vi.mock("@/components/layout/WorkspaceShell", () => ({
  default: ({ children, actionBar }: { children: React.ReactNode; actionBar?: React.ReactNode }) => (
    <div>
      {actionBar}
      {children}
    </div>
  ),
}));

vi.mock("@/components/settings/FormSection", () => ({
  default: ({ children, description, title }: { children: React.ReactNode; description?: React.ReactNode; title: string }) => (
    <section>
      <h2>{title}</h2>
      {description ? <p>{description}</p> : null}
      {children}
    </section>
  ),
}));

vi.mock("@/components/settings/ActionBar", () => ({
  default: ({ left }: { left?: React.ReactNode }) => <div>{left}</div>,
  ActionButton: ({ children }: { children: React.ReactNode }) => <button type="button">{children}</button>,
}));

vi.mock("@/stores/connectionStore", () => ({
  useConnectionStore: (selector: (state: { connected: boolean }) => unknown) =>
    selector({ connected: true }),
}));

vi.mock("@/stores/runtimeSnapshotStore", () => ({
  useRuntimeSnapshotStore: (
    selector: (state: {
      fetchSnapshot: typeof fetchSnapshotMock;
      loading: boolean;
      snapshot: {
        active_instance_id: string | null;
        instances: Record<string, unknown>;
        mode_readiness: Record<string, unknown>;
        services: Record<string, { status: string }>;
        stale: boolean;
      };
    }) => unknown,
  ) =>
    selector({
      snapshot: {
        active_instance_id: "project-1",
        instances: {},
        mode_readiness: {},
        services: {
          agent: { status: "running" },
          qgis: { status: "running" },
        },
        stale: false,
      },
      loading: false,
      fetchSnapshot: fetchSnapshotMock,
    }),
}));

test("services settings page exposes exactly one neutral read-only badge", () => {
  const html = renderToStaticMarkup(<ServicesSettingsPage />);

  expect((html.match(/>只读</g) ?? []).length).toBe(1);
  expect(html).not.toContain("诊断只读");
});
