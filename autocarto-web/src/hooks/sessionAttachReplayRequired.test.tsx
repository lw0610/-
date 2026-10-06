import { beforeEach, describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";

import SessionRuntimeSummary from "@/components/runtime/SessionRuntimeSummary";
import {
  getSessionConnectionHint,
  resolveSessionAttachBlockFromNormalizedMessage,
} from "@/lib/sessionReattach";
import { useSessionStore } from "@/stores/sessionStore";

describe("session attach replay_required handling", () => {
  beforeEach(() => {
    useSessionStore.setState({
      sessions: [],
      sessionsReady: false,
      activeSessionId: null,
      sessionMutationTick: 0,
      lastMutatedSessionId: null,
      sessionAttachState: {},
    });
  });

  it("detects explicit attach replay_required block from session response", () => {
    expect(
      resolveSessionAttachBlockFromNormalizedMessage({
        type: "session.response",
        content: "",
        data: {
          action: "attach",
          success: false,
          code: "REPLAY_REQUIRED",
          session_id: "sid-blocked",
        },
      }),
    ).toEqual({
      sessionId: "sid-blocked",
      status: "replay_required",
    });
  });

  it("stores replay_required as an explicit attach state hint", () => {
    useSessionStore.getState().setSessionAttachStatus("sid-blocked", "replay_required");

    expect(useSessionStore.getState().sessionAttachState["sid-blocked"]).toBe(
      "replay_required",
    );
    expect(getSessionConnectionHint("replay_required")).toBe("replay_required");
  });

  it("renders replay_required as an explicit blocked hint", () => {
    const html = renderToStaticMarkup(
      <SessionRuntimeSummary
        runtime={null}
        connectionHint="replay_required"
        badgeSize="xs"
      />,
    );

    expect(html).toMatch(/需重同步/);
  });
});
