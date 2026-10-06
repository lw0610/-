"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
} from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";

import {
  SettingsViewportProvider,
  useSettingsViewport,
} from "@/components/settings/SettingsViewportContext";
import InfoTooltip from "@/components/settings/InfoTooltip";
import {
  getSettingsTask,
  searchSettingDescriptors,
} from "@/lib/settingsRegistry";
import { cn } from "@/lib/utils";
import { useSettingsWorkspaceStore } from "@/stores/settingsWorkspaceStore";
import { useConnectionStore } from "@/stores/connectionStore";

function WorkspaceShellContent({
  children,
  actionBar,
}: {
  children: React.ReactNode;
  actionBar?: React.ReactNode;
}) {
  const pathname = usePathname();
  const { registerScrollContainer, scrollToSection } =
    useSettingsViewport();
  const metadata = useSettingsWorkspaceStore((s) => s.metadata);
  const workspaceLoading = useSettingsWorkspaceStore((s) => s.loading);
  const workspaceLoaded = useSettingsWorkspaceStore((s) => s.loaded);
  const workspaceError = useSettingsWorkspaceStore((s) => s.error);
  const ensureWorkspaceLoaded = useSettingsWorkspaceStore((s) => s.ensureLoaded);
  const backendReady = useConnectionStore((s) => s.backendReady);
  const [query, setQuery] = useState("");
  const headerRef = useRef<HTMLDivElement | null>(null);
  const actionBarRef = useRef<HTMLDivElement | null>(null);
  const [chromeHeight, setChromeHeight] = useState({
    header: 0,
    actionBar: 0,
  });

  useEffect(() => {
    if (!backendReady) {
      return;
    }
    void ensureWorkspaceLoaded();
  }, [backendReady, ensureWorkspaceLoaded]);

  const workspace = metadata;
  const visibleTasks = useMemo(
    () => workspace.tasks.filter((item) => !item.hidden),
    [workspace.tasks],
  );
  const activeTask = useMemo(
    () =>
      workspace.tasks.length > 0 ? getSettingsTask(pathname, workspace.tasks) : null,
    [pathname, workspace.tasks],
  );
  const searchResults = useMemo(
    () => searchSettingDescriptors(query, workspace.descriptors),
    [query, workspace.descriptors],
  );

  const measureChrome = useCallback(() => {
    setChromeHeight({
      header: headerRef.current?.offsetHeight ?? 0,
      actionBar: actionBarRef.current?.offsetHeight ?? 0,
    });
  }, []);

  useEffect(() => {
    measureChrome();
    if (typeof ResizeObserver === "undefined") {
      return undefined;
    }
    const observer = new ResizeObserver(() => {
      measureChrome();
    });
    if (headerRef.current) {
      observer.observe(headerRef.current);
    }
    if (actionBarRef.current) {
      observer.observe(actionBarRef.current);
    }
    return () => observer.disconnect();
  }, [actionBar, activeTask?.key, measureChrome]);

  const shellStyle = useMemo(
    () =>
      ({
        "--settings-header-height": `${chromeHeight.header}px`,
        "--settings-action-height": `${chromeHeight.actionBar}px`,
      }) as CSSProperties,
    [chromeHeight],
  );

  return (
    <div
      className="settings-shell flex h-full min-h-0 bg-[#fafafa] dark:bg-zinc-950"
      style={shellStyle}
    >
      <div className="mx-auto flex h-full min-h-0 w-full max-w-[1440px]">
        <nav className="flex h-full w-[200px] shrink-0 flex-col border-r border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900/95">
          <div className="border-b border-zinc-200 px-3 py-2.5 dark:border-zinc-800">
            <div className="text-[11px] font-semibold uppercase tracking-[0.18em] text-zinc-500 dark:text-zinc-400">
              Workspace
            </div>
            <div className="mt-0.5 text-xs font-semibold text-zinc-950 dark:text-zinc-50">
              工作台
            </div>
          </div>

          <div className="min-h-0 flex-1 overflow-y-auto px-2 py-2.5 scrollbar-thin">
            <div className="mb-1.5 px-2 text-[11px] font-medium uppercase tracking-[0.16em] text-zinc-500 dark:text-zinc-400">
              任务域
            </div>
            <div className="space-y-1">
              {visibleTasks.map((item) => {
                const active = item.key === activeTask?.key;
                return (
                  <div key={item.key} className="rounded">
                    <div
                      className={cn(
                        "flex items-start gap-1 rounded-sm border-l-2 transition-colors",
                        active
                          ? "border-l-blue-500 bg-zinc-100 dark:border-l-blue-400 dark:bg-zinc-800/80"
                          : "border-l-transparent hover:border-l-zinc-300 hover:bg-zinc-50 dark:hover:border-l-zinc-700 dark:hover:bg-zinc-950/70",
                      )}
                    >
                      <Link
                        href={item.href}
                        className="min-w-0 flex-1 px-2.5 py-2"
                      >
                        <div className="flex items-center gap-1.5">
                          <div className="text-[11px] font-medium text-zinc-900 dark:text-zinc-50">
                            {item.label}
                          </div>
                          {item.hint ? <InfoTooltip content={item.hint} /> : null}
                        </div>
                      </Link>
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        </nav>

        <div className="min-w-0 flex flex-1 flex-col overflow-hidden">
          <div
            ref={headerRef}
            className="border-b border-zinc-200 bg-white px-4 py-2 dark:border-zinc-800 dark:bg-zinc-900/95"
          >
            <div className="flex flex-col gap-1.5 xl:flex-row xl:items-center xl:justify-between">
              <div className="min-w-0">
                <h1 className="text-sm font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">
                  {activeTask?.label || "设置工作区"}
                </h1>
              </div>

              <div className="w-full max-w-[460px]">
                <input
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  placeholder="搜索设置、分节、别名与关键词…"
                  className="h-8 w-full rounded-sm border border-zinc-200 bg-zinc-100 px-2.5 text-xs text-zinc-800 outline-none transition-colors placeholder:text-zinc-400 focus:border-blue-500 focus:bg-white focus:ring-2 focus:ring-blue-500/10 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100 dark:placeholder:text-zinc-500"
                />
                {query.trim() ? (
                  <div className="mt-1.5 rounded border border-zinc-200 bg-white p-1.5 dark:border-zinc-800 dark:bg-zinc-950/95">
                    {searchResults.length > 0 ? (
                      <div className="max-h-60 space-y-1 overflow-y-auto">
                        {searchResults.map((item) => (
                          <Link
                            key={item.id}
                            href={item.href}
                            className="block rounded border border-transparent px-2.5 py-1.5 transition-colors hover:border-zinc-200 hover:bg-zinc-50 dark:hover:border-zinc-800 dark:hover:bg-zinc-900"
                            onClick={(event) => {
                              setQuery("");
                              if (item.anchor && item.path === pathname) {
                                event.preventDefault();
                                scrollToSection(item.anchor);
                              }
                            }}
                          >
                            <div className="min-w-0">
                              <div className="text-xs font-medium text-zinc-900 dark:text-zinc-100">
                                {item.label}
                              </div>
                              <div className="mt-0.5 text-xs leading-4 text-zinc-500 dark:text-zinc-400">
                                {item.section} · {item.description || item.path}
                              </div>
                            </div>
                          </Link>
                        ))}
                      </div>
                    ) : (
                      <div className="rounded border border-dashed border-zinc-200 bg-zinc-50 px-2.5 py-2 text-xs leading-5 text-zinc-500 dark:border-zinc-800 dark:bg-zinc-900/60 dark:text-zinc-400">
                        未找到匹配的设置项，请换一个关键词。
                      </div>
                    )}
                  </div>
                ) : null}
              </div>
            </div>
          </div>

          {actionBar ? (
            <div
              ref={actionBarRef}
              className="border-b border-zinc-200 bg-white px-4 dark:border-zinc-800 dark:bg-zinc-900/95"
            >
              {actionBar}
            </div>
          ) : null}

          <main
            ref={registerScrollContainer}
            className="settings-scroll min-h-0 flex-1 overflow-y-auto px-4 py-3"
          >
            {visibleTasks.length > 0 ? (
              <div className="space-y-3">{children}</div>
            ) : (
              <div className="rounded border border-dashed border-zinc-300 bg-white px-4 py-5 text-sm text-zinc-600 dark:border-zinc-800 dark:bg-zinc-900/60 dark:text-zinc-300">
                {workspaceLoading && !workspaceLoaded
                  ? "正在加载设置工作区…"
                  : workspaceError ||
                    "设置工作区 contract 当前不可用，请检查后端 settings workspace 接口。"}
              </div>
            )}
          </main>
        </div>
      </div>
    </div>
  );
}

export default function WorkspaceShell({
  children,
  actionBar,
}: {
  children: React.ReactNode;
  actionBar?: React.ReactNode;
}) {
  return (
    <SettingsViewportProvider>
      <WorkspaceShellContent actionBar={actionBar}>
        {children}
      </WorkspaceShellContent>
    </SettingsViewportProvider>
  );
}
