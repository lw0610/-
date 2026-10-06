import test from "node:test";
import assert from "node:assert/strict";

import { getInfoTooltipPlacement } from "./infoTooltipPlacement";

function createRect(rect: Partial<DOMRect>): DOMRect {
  return {
    x: rect.left ?? 0,
    y: rect.top ?? 0,
    top: rect.top ?? 0,
    left: rect.left ?? 0,
    right: rect.right ?? 0,
    bottom: rect.bottom ?? 0,
    width: rect.width ?? Math.max(0, (rect.right ?? 0) - (rect.left ?? 0)),
    height:
      rect.height ?? Math.max(0, (rect.bottom ?? 0) - (rect.top ?? 0)),
    toJSON() {
      return this;
    },
  } as DOMRect;
}

test("preferred top placement flips to bottom when top space is insufficient", () => {
  const placement = getInfoTooltipPlacement({
    anchorRect: createRect({ top: 10, left: 100, right: 132, bottom: 42 }),
    overlayWidth: 220,
    overlayHeight: 96,
    viewportWidth: 640,
    viewportHeight: 480,
    preferredSide: "top",
  });

  assert.equal(placement.side, "bottom");
  assert.ok(placement.top >= 42);
});

test("preferred right placement flips horizontally when viewport edge would clip it", () => {
  const placement = getInfoTooltipPlacement({
    anchorRect: createRect({ top: 120, left: 604, right: 628, bottom: 152 }),
    overlayWidth: 240,
    overlayHeight: 88,
    viewportWidth: 640,
    viewportHeight: 480,
    preferredSide: "right",
  });

  assert.notEqual(placement.side, "right");
  assert.ok(placement.left <= 640 - 8 - 240);
});

test("bottom placement clamps inside viewport when near lower edge", () => {
  const placement = getInfoTooltipPlacement({
    anchorRect: createRect({ top: 420, left: 240, right: 280, bottom: 452 }),
    overlayWidth: 220,
    overlayHeight: 90,
    viewportWidth: 640,
    viewportHeight: 480,
    preferredSide: "bottom",
  });

  assert.notEqual(placement.side, "bottom");
  assert.ok(placement.top >= 8);
  assert.ok(placement.top <= 480 - 8 - 90);
});

test("tooltip placement keeps overlay inside viewport for scroll-container triggers", () => {
  const placement = getInfoTooltipPlacement({
    anchorRect: createRect({ top: 210, left: 6, right: 28, bottom: 234 }),
    overlayWidth: 260,
    overlayHeight: 84,
    viewportWidth: 360,
    viewportHeight: 640,
    preferredSide: "top",
  });

  assert.ok(placement.left >= 8);
  assert.ok(placement.left <= 360 - 8 - 260);
  assert.ok(placement.top >= 8);
  assert.ok(placement.top <= 640 - 8 - 84);
});
