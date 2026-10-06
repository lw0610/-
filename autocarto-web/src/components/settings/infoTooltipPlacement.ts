export type InfoTooltipSide = "top" | "bottom" | "right";

interface InfoTooltipPlacementOptions {
  anchorRect: DOMRect;
  overlayWidth: number;
  overlayHeight: number;
  viewportWidth: number;
  viewportHeight: number;
  preferredSide?: InfoTooltipSide;
  margin?: number;
  gap?: number;
}

interface CandidatePlacement {
  side: InfoTooltipSide;
  top: number;
  left: number;
}

export interface InfoTooltipPlacement {
  side: InfoTooltipSide;
  top: number;
  left: number;
}

function clamp(value: number, min: number, max: number) {
  if (max <= min) {
    return min;
  }
  return Math.min(max, Math.max(min, value));
}

function getPlacementPriority(
  preferredSide: InfoTooltipSide,
): InfoTooltipSide[] {
  if (preferredSide === "bottom") {
    return ["bottom", "top", "right"];
  }
  if (preferredSide === "right") {
    return ["right", "top", "bottom"];
  }
  return ["top", "bottom", "right"];
}

function buildCandidatePlacement(
  side: InfoTooltipSide,
  anchorRect: DOMRect,
  overlayWidth: number,
  overlayHeight: number,
  gap: number,
): CandidatePlacement {
  const centerX = anchorRect.left + anchorRect.width / 2;
  const centerY = anchorRect.top + anchorRect.height / 2;

  if (side === "bottom") {
    return {
      side,
      top: anchorRect.bottom + gap,
      left: centerX - overlayWidth / 2,
    };
  }

  if (side === "right") {
    return {
      side,
      top: centerY - overlayHeight / 2,
      left: anchorRect.right + gap,
    };
  }

  return {
    side,
    top: anchorRect.top - gap - overlayHeight,
    left: centerX - overlayWidth / 2,
  };
}

export function getInfoTooltipPlacement({
  anchorRect,
  overlayWidth,
  overlayHeight,
  viewportWidth,
  viewportHeight,
  preferredSide = "top",
  margin = 8,
  gap = 8,
}: InfoTooltipPlacementOptions): InfoTooltipPlacement {
  const maxLeft = Math.max(margin, viewportWidth - overlayWidth - margin);
  const maxTop = Math.max(margin, viewportHeight - overlayHeight - margin);

  const scoredPlacements = getPlacementPriority(preferredSide).map((side) => {
    const candidate = buildCandidatePlacement(
      side,
      anchorRect,
      overlayWidth,
      overlayHeight,
      gap,
    );
    const overflowLeft = Math.max(0, margin - candidate.left);
    const overflowRight = Math.max(
      0,
      candidate.left + overlayWidth + margin - viewportWidth,
    );
    const overflowTop = Math.max(0, margin - candidate.top);
    const overflowBottom = Math.max(
      0,
      candidate.top + overlayHeight + margin - viewportHeight,
    );

    return {
      ...candidate,
      top: clamp(candidate.top, margin, maxTop),
      left: clamp(candidate.left, margin, maxLeft),
      score: overflowLeft + overflowRight + overflowTop + overflowBottom,
    };
  });

  const exactMatch = scoredPlacements.find((item) => item.score === 0);
  if (exactMatch) {
    return exactMatch;
  }

  return scoredPlacements.reduce((best, current) =>
    current.score < best.score ? current : best,
  );
}
