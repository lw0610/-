import { test, expect } from "vitest";

import {
  isDiagnosticsPath,
  isSettingsPath,
  isToolCenterPath,
  isWorkspacePath,
  routeIsActive,
} from "./workspaceRoutes";

test("workspace route helpers distinguish settings and diagnostics", () => {
  expect(isSettingsPath("/settings/system")).toBe(true);
  expect(isSettingsPath("/diagnostics")).toBe(false);
  expect(isDiagnosticsPath("/diagnostics")).toBe(true);
  expect(isDiagnosticsPath("/diagnostics/runtime")).toBe(true);
  expect(isToolCenterPath("/tool-center")).toBe(true);
  expect(isWorkspacePath("/tool-center")).toBe(true);
  expect(isWorkspacePath("/diagnostics")).toBe(true);
  expect(isWorkspacePath("/chat")).toBe(false);
});

test("routeIsActive matches exact and nested workspace routes", () => {
  expect(routeIsActive("/settings/system", "/settings")).toBe(true);
  expect(routeIsActive("/settings/system", "/settings/system")).toBe(true);
  expect(routeIsActive("/diagnostics/runtime", "/diagnostics")).toBe(true);
  expect(routeIsActive("/tool-center", "/tool-center")).toBe(true);
  expect(routeIsActive("/sessions", "/diagnostics")).toBe(false);
});
