import { describe, expect, it } from "vitest";

import {
  createInitialRunEventSeqState,
  removeRunEventSeqState,
  upsertRunEventSeqState,
} from "./runEventSeqState";

describe("runEventSeqState", () => {
  it("starts from empty state instead of reading localStorage", () => {
    window.localStorage.setItem("legacy-seq-cache", JSON.stringify({ "sid-a": 9 }));

    expect(createInitialRunEventSeqState()).toEqual({});
  });

  it("tracks and removes session sequence in memory only", () => {
    const first = upsertRunEventSeqState({}, " sid-a ", 3);
    expect(first.nextState).toEqual({ "sid-a": 3 });
    expect(first.changed).toBe(true);

    const second = removeRunEventSeqState(first.nextState, "sid-a");
    expect(second).toEqual({});
  });
});
