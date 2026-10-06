import test from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";

import SessionRuntimeSummary from "./SessionRuntimeSummary";

test("SessionRuntimeSummary renders reattaching hint even without runtime payload", () => {
  const html = renderToStaticMarkup(
    <SessionRuntimeSummary
      runtime={null}
      connectionHint="reattaching"
      badgeSize="xs"
    />,
  );

  assert.match(html, /重连中/);
});

test("SessionRuntimeSummary renders runtime status and reattaching hint together", () => {
  const html = renderToStaticMarkup(
    <SessionRuntimeSummary
      runtime={{
        session_id: "sid-runtime",
        status: "running",
        bound_instance_id: "inst-a",
      }}
      connectionHint="reattaching"
      badgeSize="xs"
    />,
  );

  assert.match(html, /运行中/);
  assert.match(html, /重连中/);
  assert.match(html, /实例 inst-a/);
});

test("SessionRuntimeSummary renders replay_required hint as explicit blocked state", () => {
  const html = renderToStaticMarkup(
    <SessionRuntimeSummary
      runtime={null}
      connectionHint="replay_required"
      badgeSize="xs"
    />,
  );

  assert.match(html, /需重同步/);
});
