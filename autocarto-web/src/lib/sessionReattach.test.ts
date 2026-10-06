import { test, expect } from "vitest";

import {
  collectSessionAttachTargets,
  getSessionConnectionHint,
  markSessionAttachStateLive,
  markSessionAttachStatesReattaching,
  pruneSessionAttachStates,
  resolveSessionAttachBlockFromNormalizedMessage,
  resolveSessionAttachAckFromNormalizedMessage,
  resolveSessionAttachAckFromRawMessage,
} from "./sessionReattach";

test("collectSessionAttachTargets dedupes active chat and running sessions in stable order", () => {
  const targets = collectSessionAttachTargets({
    activeSessionId: "sid-active",
    chatSessionId: "sid-chat",
    runningSessionIds: ["sid-active", "sid-run", "sid-chat", ""],
  });

  expect(targets).toEqual(["sid-active", "sid-chat", "sid-run"]);
});

test("markSessionAttachStatesReattaching keeps only desired sessions and sets them reattaching", () => {
  const next = markSessionAttachStatesReattaching(
    {
      stale: "live",
      current: "live",
    },
    ["current", "pending"],
  );

  expect(next).toEqual({
    current: "reattaching",
    pending: "reattaching",
  });
});

test("markSessionAttachStateLive promotes a session to live and prune removes undesired sessions", () => {
  const live = markSessionAttachStateLive(
    {
      "sid-a": "reattaching",
      "sid-b": "live",
    },
    "sid-a",
  );
  expect(live).toEqual({
    "sid-a": "live",
    "sid-b": "live",
  });

  const pruned = pruneSessionAttachStates(live, ["sid-a"]);
  expect(pruned).toEqual({
    "sid-a": "live",
  });
  const sidAStatus = pruned["sid-a"] as "live" | undefined;
  expect(getSessionConnectionHint(sidAStatus)).toBeUndefined();
  expect(getSessionConnectionHint("reattaching")).toBe("reattaching");
  expect(getSessionConnectionHint("replay_required")).toBe("replay_required");
});

test("resolveSessionAttachAckFromRawMessage accepts run_event with seq and ignores invalid payloads", () => {
  expect(
    resolveSessionAttachAckFromRawMessage({
      type: "run_event",
      data: {
        seq: 3,
        payload: { session_id: "sid-run" },
      },
    }),
    "sid-run",
  ).toBe("sid-run");

  expect(
    resolveSessionAttachAckFromRawMessage({
      type: "run_event",
      session_id: "sid-no-seq",
      data: { payload: { session_id: "sid-no-seq" } },
    }),
    "",
  ).toBe("");

  expect(
    resolveSessionAttachAckFromRawMessage({
      type: "status",
      session_id: "sid-status",
      data: {},
    }),
    "",
  ).toBe("");
});

test("resolveSessionAttachAckFromNormalizedMessage accepts session_state and session_runtime_state only", () => {
  expect(
    resolveSessionAttachAckFromNormalizedMessage({
      type: "session_state",
      data: { session_id: "sid-snapshot" },
    }),
    "sid-snapshot",
  ).toBe("sid-snapshot");

  expect(
    resolveSessionAttachAckFromNormalizedMessage({
      type: "session_runtime_state",
      session_id: "sid-runtime",
      data: {},
    }),
    "sid-runtime",
  ).toBe("sid-runtime");

  expect(
    resolveSessionAttachAckFromNormalizedMessage({
      type: "session_list_changed",
      session_id: "sid-list",
      data: {},
    }),
    "",
  ).toBe("");
});

test("resolveSessionAttachBlockFromNormalizedMessage detects explicit attach replay_required block", () => {
  expect(
    resolveSessionAttachBlockFromNormalizedMessage({
      type: "session.response",
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

  expect(
    resolveSessionAttachBlockFromNormalizedMessage({
      type: "session.response",
      data: {
        action: "attach",
        success: true,
        code: "REPLAY_REQUIRED",
        session_id: "sid-live",
      },
    }),
    null,
  ).toBeNull();
});
