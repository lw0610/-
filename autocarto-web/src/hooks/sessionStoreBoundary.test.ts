import { describe, expect, it } from "vitest";

import { useSessionStore } from "@/stores/sessionStore";

describe("session store boundary", () => {
  it("does not own session panel collapsed ui state", () => {
    expect(
      Object.prototype.hasOwnProperty.call(
        useSessionStore.getState(),
        "sessionPanelCollapsed",
      ),
    ).toBe(false);
  });
});
