"use client";

import React, {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
import {
  getInfoTooltipPlacement,
  type InfoTooltipPlacement,
  type InfoTooltipSide,
} from "@/components/settings/infoTooltipPlacement";
import { cn } from "@/lib/utils";

interface InfoTooltipProps {
  content: React.ReactNode;
  side?: InfoTooltipSide;
  maxWidth?: number;
  className?: string;
}

const VIEWPORT_MARGIN = 8;
const TOOLTIP_GAP = 8;

export default function InfoTooltip({
  content,
  side = "top",
  maxWidth = 280,
  className,
}: InfoTooltipProps) {
  const anchorRef = useRef<HTMLSpanElement | null>(null);
  const tooltipRef = useRef<HTMLDivElement | null>(null);
  const [mounted, setMounted] = useState(false);
  const [open, setOpen] = useState(false);
  const [ready, setReady] = useState(false);
  const [placement, setPlacement] = useState<InfoTooltipPlacement>({
    top: VIEWPORT_MARGIN,
    left: VIEWPORT_MARGIN,
    side,
  });

  useEffect(() => {
    setMounted(true);
  }, []);

  useEffect(() => {
    if (!open) {
      setReady(false);
    }
  }, [open]);

  const ariaLabel = useMemo(
    () => (typeof content === "string" ? content : "查看说明"),
    [content],
  );

  const updatePosition = useCallback(() => {
    const anchor = anchorRef.current;
    const tooltip = tooltipRef.current;
    if (!anchor || !tooltip || typeof window === "undefined") {
      return;
    }

    const tooltipRect = tooltip.getBoundingClientRect();
    const nextPlacement = getInfoTooltipPlacement({
      anchorRect: anchor.getBoundingClientRect(),
      overlayWidth: tooltipRect.width || tooltip.offsetWidth || maxWidth,
      overlayHeight: tooltipRect.height || tooltip.offsetHeight || 40,
      viewportWidth: window.innerWidth,
      viewportHeight: window.innerHeight,
      preferredSide: side,
      margin: VIEWPORT_MARGIN,
      gap: TOOLTIP_GAP,
    });

    setPlacement(nextPlacement);
    setReady(true);
  }, [maxWidth, side]);

  useLayoutEffect(() => {
    if (!mounted || !open) {
      return;
    }

    updatePosition();
    const frame = window.requestAnimationFrame(updatePosition);
    const observer =
      typeof ResizeObserver === "undefined"
        ? null
        : new ResizeObserver(() => {
            updatePosition();
          });

    if (observer && anchorRef.current) {
      observer.observe(anchorRef.current);
    }
    if (observer && tooltipRef.current) {
      observer.observe(tooltipRef.current);
    }

    window.addEventListener("resize", updatePosition);
    window.addEventListener("scroll", updatePosition, true);

    return () => {
      window.cancelAnimationFrame(frame);
      observer?.disconnect();
      window.removeEventListener("resize", updatePosition);
      window.removeEventListener("scroll", updatePosition, true);
    };
  }, [mounted, open, updatePosition]);

  return (
    <span className={cn("inline-flex shrink-0", className)}>
      <span
        ref={anchorRef}
        tabIndex={0}
        aria-label={ariaLabel}
        className="inline-flex h-4 w-4 items-center justify-center rounded-full text-zinc-400 outline-none transition-colors hover:text-zinc-600 focus-visible:text-zinc-600 dark:text-zinc-500 dark:hover:text-zinc-300 dark:focus-visible:text-zinc-300"
        onMouseEnter={() => setOpen(true)}
        onMouseLeave={() => setOpen(false)}
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
      >
        <svg
          viewBox="0 0 16 16"
          className="h-3.5 w-3.5"
          fill="none"
          aria-hidden="true"
        >
          <circle cx="8" cy="8" r="6.25" stroke="currentColor" strokeWidth="1.2" />
          <path
            d="M6.85 6.3a1.26 1.26 0 1 1 2.18.86c-.4.44-.9.75-1.16 1.26-.13.25-.16.45-.16.83"
            stroke="currentColor"
            strokeWidth="1.2"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
          <circle cx="8" cy="11.35" r=".7" fill="currentColor" />
        </svg>
      </span>
      {mounted && open && typeof document !== "undefined"
        ? createPortal(
            <div
              ref={tooltipRef}
              role="tooltip"
              style={{
                top: placement.top,
                left: placement.left,
                maxWidth: `min(calc(100vw - ${VIEWPORT_MARGIN * 2}px), ${maxWidth}px)`,
                visibility: ready ? "visible" : "hidden",
              }}
              className={cn(
                "pointer-events-none fixed z-[90] rounded-sm border border-zinc-200 bg-white px-3 py-2 text-[11px] leading-4 text-zinc-600 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-200",
                "break-words whitespace-normal",
              )}
            >
              {content}
            </div>,
            document.body,
          )
        : null}
    </span>
  );
}
