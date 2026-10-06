import { beforeEach, expect, test } from "vitest";

import { useSessionStore } from "./sessionStore";

function resetStore() {
  useSessionStore.setState({
    sessions: [],
    sessionsReady: false,
    activeSessionId: null,
    sessionMutationTick: 0,
    lastMutatedSessionId: null,
    sessionAttachState: {},
  });
}

beforeEach(() => {
  resetStore();
});

test("session attach state methods set and clear transient reattach state", () => {
  useSessionStore.getState().setSessions([
    {
      session_id: "sid-a",
      name: "A",
      created_at: "2026-03-13T10:00:00Z",
      updated_at: "2026-03-13T10:00:00Z",
      messages: [],
    },
  ]);

  useSessionStore.getState().setSessionAttachStatus("sid-a", "reattaching");
  expect(useSessionStore.getState().sessionAttachState["sid-a"]).toBe(
    "reattaching",
  );

  useSessionStore.getState().setSessionAttachStates({
    "sid-a": "live",
    "sid-b": "reattaching",
  });
  expect(useSessionStore.getState().sessionAttachState).toEqual({
    "sid-a": "live",
    "sid-b": "reattaching",
  });

  useSessionStore.getState().clearSessionAttachState("sid-a");
  expect(useSessionStore.getState().sessionAttachState).toEqual({
    "sid-b": "reattaching",
  });

  useSessionStore.getState().setSessionAttachStatus("sid-b", "replay_required");
  expect(useSessionStore.getState().sessionAttachState["sid-b"]).toBe(
    "replay_required",
  );
});

test("removeSession clears transient attach state for deleted session", () => {
  useSessionStore.getState().setSessions([
    {
      session_id: "sid-a",
      name: "A",
      created_at: "2026-03-13T10:00:00Z",
      updated_at: "2026-03-13T10:00:00Z",
      messages: [],
    },
    {
      session_id: "sid-b",
      name: "B",
      created_at: "2026-03-13T10:00:00Z",
      updated_at: "2026-03-13T10:00:00Z",
      messages: [],
    },
  ]);
  useSessionStore.getState().setSessionAttachStates({
    "sid-a": "reattaching",
    "sid-b": "live",
  });

  useSessionStore.getState().removeSession("sid-a");

  expect(useSessionStore.getState().sessionAttachState).toEqual({
    "sid-b": "live",
  });
});
