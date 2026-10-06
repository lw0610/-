export function normalizeRunEventSeqSessionId(value: unknown) {
  if (typeof value !== "string") {
    return "";
  }
  return value.trim();
}

export function createInitialRunEventSeqState(): Record<string, number> {
  return {};
}

export function upsertRunEventSeqState(
  current: Record<string, number>,
  sessionId: unknown,
  seq: number,
) {
  const normalizedSessionId = normalizeRunEventSeqSessionId(sessionId);
  if (!normalizedSessionId || !Number.isFinite(seq) || seq <= 0) {
    return { changed: false, nextState: current };
  }
  const normalizedSeq = Math.floor(seq);
  const previous = current[normalizedSessionId] || 0;
  if (normalizedSeq <= previous && normalizedSeq !== 1) {
    return { changed: false, nextState: current };
  }
  return {
    changed: true,
    nextState: {
      ...current,
      [normalizedSessionId]: normalizedSeq,
    },
  };
}

export function removeRunEventSeqState(
  current: Record<string, number>,
  sessionId: unknown,
) {
  const normalizedSessionId = normalizeRunEventSeqSessionId(sessionId);
  if (!normalizedSessionId || !(normalizedSessionId in current)) {
    return current;
  }
  const next = { ...current };
  delete next[normalizedSessionId];
  return next;
}
