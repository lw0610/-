import type { WSMessage } from "@/types/message";

export type SessionAttachStatus = "live" | "reattaching" | "replay_required";
export type SessionConnectionHint = "reattaching" | "replay_required";

function normalizeSessionId(value: unknown) {
  if (typeof value !== "string") {
    return "";
  }
  return value.trim();
}

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return {};
  }
  return value as Record<string, unknown>;
}

export function collectSessionAttachTargets(input: {
  activeSessionId?: string | null;
  chatSessionId?: string | null;
  runningSessionIds?: string[];
}) {
  const ordered = [
    input.activeSessionId,
    input.chatSessionId,
    ...(Array.isArray(input.runningSessionIds) ? input.runningSessionIds : []),
  ];
  const deduped = new Set<string>();
  for (const value of ordered) {
    const sessionId = normalizeSessionId(value);
    if (sessionId) {
      deduped.add(sessionId);
    }
  }
  return Array.from(deduped);
}

export function pruneSessionAttachStates(
  current: Record<string, SessionAttachStatus>,
  keepSessionIds: string[],
): Record<string, SessionAttachStatus> {
  const keep = new Set(
    keepSessionIds.map((item) => normalizeSessionId(item)).filter(Boolean),
  );
  return Object.fromEntries(
    Object.entries(current).filter(([sessionId]) => keep.has(sessionId)),
  ) as Record<string, SessionAttachStatus>;
}

export function markSessionAttachStatesReattaching(
  current: Record<string, SessionAttachStatus>,
  sessionIds: string[],
): Record<string, SessionAttachStatus> {
  const next = pruneSessionAttachStates(current, sessionIds);
  for (const sessionId of sessionIds) {
    const normalized = normalizeSessionId(sessionId);
    if (!normalized) {
      continue;
    }
    next[normalized] = "reattaching";
  }
  return next;
}

export function markSessionAttachStateLive(
  current: Record<string, SessionAttachStatus>,
  sessionId?: string | null,
): Record<string, SessionAttachStatus> {
  const normalized = normalizeSessionId(sessionId);
  if (!normalized) {
    return current;
  }
  return {
    ...current,
    [normalized]: "live",
  };
}

export function markSessionAttachStateReplayRequired(
  current: Record<string, SessionAttachStatus>,
  sessionId?: string | null,
): Record<string, SessionAttachStatus> {
  const normalized = normalizeSessionId(sessionId);
  if (!normalized) {
    return current;
  }
  return {
    ...current,
    [normalized]: "replay_required",
  };
}

export function getSessionConnectionHint(
  status?: SessionAttachStatus | null,
): SessionConnectionHint | undefined {
  if (status === "reattaching") {
    return "reattaching";
  }
  if (status === "replay_required") {
    return "replay_required";
  }
  return undefined;
}

export function resolveSessionAttachAckFromRawMessage(
  msg: Pick<WSMessage, "type" | "session_id" | "data">,
) {
  if (msg.type !== "run_event") {
    return "";
  }
  const data = asRecord(msg.data);
  const seq = Number(data.seq || 0);
  if (!Number.isFinite(seq) || seq <= 0) {
    return "";
  }
  const payload = asRecord(data.payload);
  return (
    normalizeSessionId(payload.session_id) || normalizeSessionId(msg.session_id)
  );
}

export function resolveSessionAttachAckFromNormalizedMessage(
  msg: Pick<WSMessage, "type" | "session_id" | "data">,
) {
  if (msg.type !== "session_state" && msg.type !== "session_runtime_state") {
    return "";
  }
  const data = asRecord(msg.data);
  return (
    normalizeSessionId(data.session_id) || normalizeSessionId(msg.session_id)
  );
}

export function resolveSessionAttachBlockFromNormalizedMessage(
  msg: Pick<WSMessage, "type" | "session_id" | "data">,
) {
  if (msg.type !== "session.response" && msg.type !== "session_response") {
    return null;
  }
  const data = asRecord(msg.data);
  if (
    String(data.action || "").trim() !== "attach" ||
    data.success !== false ||
    String(data.code || "").trim() !== "REPLAY_REQUIRED"
  ) {
    return null;
  }
  const sessionId =
    normalizeSessionId(data.session_id) || normalizeSessionId(msg.session_id);
  if (!sessionId) {
    return null;
  }
  return {
    sessionId,
    status: "replay_required" as const,
  };
}
