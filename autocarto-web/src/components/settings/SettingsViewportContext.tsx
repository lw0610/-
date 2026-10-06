"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { usePathname } from "next/navigation";

export interface SettingsSectionController {
  expand?: () => void;
}

export interface SettingsViewportValue {
  registerSection: (
    id: string,
    controller?: SettingsSectionController,
  ) => () => void;
  registerScrollContainer: (node: HTMLElement | null) => void;
  scrollToSection: (id: string) => void;
  activeSectionId?: string;
}

const noop = () => {};

const SettingsViewportContext = createContext<SettingsViewportValue>({
  registerSection: () => noop,
  registerScrollContainer: noop,
  scrollToSection: noop,
  activeSectionId: undefined,
});

export function SettingsViewportProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  const pathname = usePathname();
  const sectionControllersRef = useRef<Map<string, SettingsSectionController>>(
    new Map(),
  );
  const [scrollContainer, setScrollContainer] = useState<HTMLElement | null>(
    null,
  );
  const [activeSectionId, setActiveSectionId] = useState<string>();

  const registerSection = useCallback(
    (id: string, controller: SettingsSectionController = {}) => {
      sectionControllersRef.current.set(id, controller);
      return () => {
        const current = sectionControllersRef.current.get(id);
        if (current === controller) {
          sectionControllersRef.current.delete(id);
        }
      };
    },
    [],
  );

  const registerScrollContainer = useCallback((node: HTMLElement | null) => {
    setScrollContainer((current) => (current === node ? current : node));
  }, []);

  const resolveAnchorTargets = useCallback((container: HTMLElement | null) => {
    if (!container) {
      return [];
    }
    return Array.from(
      container.querySelectorAll<HTMLElement>(".settings-anchor-target[id]"),
    );
  }, []);

  const updateActiveSection = useCallback(() => {
    if (!scrollContainer) {
      setActiveSectionId(undefined);
      return;
    }

    const anchors = resolveAnchorTargets(scrollContainer);
    if (anchors.length === 0) {
      setActiveSectionId(undefined);
      return;
    }

    const containerRect = scrollContainer.getBoundingClientRect();
    const visibleAnchors = anchors.filter((element) => {
      const rect = element.getBoundingClientRect();
      return rect.bottom > containerRect.top + 8 && rect.top < containerRect.bottom - 8;
    });

    const candidates =
      visibleAnchors.length > 0
        ? visibleAnchors
        : anchors.filter(
            (element) =>
              element.getBoundingClientRect().top <= containerRect.top + 16,
          );

    const sorted = (candidates.length > 0 ? candidates : anchors)
      .map((element) => ({
        id: element.id,
        distance: Math.abs(
          element.getBoundingClientRect().top - containerRect.top,
        ),
      }))
      .sort((left, right) => left.distance - right.distance);

    setActiveSectionId(sorted[0]?.id);
  }, [resolveAnchorTargets, scrollContainer]);

  const scrollToSection = useCallback((id: string) => {
    sectionControllersRef.current.get(id)?.expand?.();
    const resolveAndScroll = (retries = 8) => {
      const element = document.getElementById(id);
      if (!element) {
        if (retries > 0) {
          requestAnimationFrame(() => resolveAndScroll(retries - 1));
        }
        return;
      }
      if (typeof window !== "undefined") {
        window.history.replaceState(null, "", `${pathname}#${id}`);
      }
      element.scrollIntoView({ behavior: "smooth", block: "start" });
      requestAnimationFrame(() => updateActiveSection());
    };
    requestAnimationFrame(() => resolveAndScroll());
  }, [pathname, updateActiveSection]);

  useEffect(() => {
    setActiveSectionId(undefined);
  }, [pathname]);

  useEffect(() => {
    if (!scrollContainer || typeof IntersectionObserver === "undefined") {
      updateActiveSection();
      return undefined;
    }

    const anchors = resolveAnchorTargets(scrollContainer);
    if (anchors.length === 0) {
      setActiveSectionId(undefined);
      return undefined;
    }

    const observer = new IntersectionObserver(
      () => {
        updateActiveSection();
      },
      {
        root: scrollContainer,
        threshold: [0, 0.2, 0.4, 0.6, 0.8, 1],
      },
    );

    anchors.forEach((anchor) => observer.observe(anchor));
    scrollContainer.addEventListener("scroll", updateActiveSection, {
      passive: true,
    });
    window.addEventListener("resize", updateActiveSection);

    const frame = requestAnimationFrame(() => updateActiveSection());

    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      scrollContainer.removeEventListener("scroll", updateActiveSection);
      window.removeEventListener("resize", updateActiveSection);
    };
  }, [resolveAnchorTargets, scrollContainer, updateActiveSection]);

  useEffect(() => {
    if (typeof window === "undefined") {
      return undefined;
    }
    const hash = window.location.hash.replace(/^#/, "").trim();
    if (!hash) {
      return undefined;
    }
    const frame = requestAnimationFrame(() => {
      scrollToSection(hash);
    });
    return () => cancelAnimationFrame(frame);
  }, [pathname, scrollToSection]);

  const value = useMemo(
    () => ({
      registerSection,
      registerScrollContainer,
      scrollToSection,
      activeSectionId,
    }),
    [activeSectionId, registerScrollContainer, registerSection, scrollToSection],
  );

  return (
    <SettingsViewportContext.Provider value={value}>
      {children}
    </SettingsViewportContext.Provider>
  );
}

export function useSettingsViewport() {
  return useContext(SettingsViewportContext);
}

export default SettingsViewportContext;
