"use client";

import { useState } from "react";
import AppShell from "@/components/layout/AppShell";
import WorkspaceShell from "@/components/layout/WorkspaceShell";
import ModelConfigWorkspace from "@/components/settings/ModelConfigWorkspace";
import AgentConfigWorkspace from "@/components/settings/AgentConfigWorkspace";
import Image2ConfigWorkspace from "@/components/settings/Image2ConfigWorkspace";
import { ErrorBoundary } from "react-error-boundary";

function ErrorFallback({ error, resetErrorBoundary }: { error: unknown, resetErrorBoundary: () => void }) {
  const message = error instanceof Error ? error.message : String(error || "未知错误");
  return (
    <div className="rounded-md border border-red-200 bg-red-50 p-4 dark:border-red-900/50 dark:bg-red-950/20">
      <h3 className="text-sm font-medium text-red-800 dark:text-red-400">加载组件失败</h3>
      <div className="mt-2 text-xs text-red-700 dark:text-red-500">
        <p>{message}</p>
      </div>
      <button
        type="button"
        onClick={resetErrorBoundary}
        className="mt-3 rounded bg-red-100 px-3 py-1.5 text-xs font-medium text-red-800 hover:bg-red-200 dark:bg-red-900/30 dark:text-red-400 dark:hover:bg-red-900/50"
      >
        重试
      </button>
    </div>
  );
}

export default function ModelsAgentsPage() {
  const [activeTab, setActiveTab] = useState<"models" | "agents" | "image2">("models");
  
  return (
    <AppShell>
      <WorkspaceShell>
        <div className="mb-4 border-b border-zinc-200 dark:border-zinc-800">
          <nav className="-mb-px flex space-x-6" aria-label="Tabs">
            <button
              onClick={() => setActiveTab("models")}
              className={`
                whitespace-nowrap border-b-2 py-3 text-sm font-medium
                ${
                  activeTab === "models"
                    ? "border-blue-500 text-blue-600 dark:text-blue-400"
                    : "border-transparent text-zinc-500 hover:border-zinc-300 hover:text-zinc-700 dark:text-zinc-400 dark:hover:border-zinc-700 dark:hover:text-zinc-300"
                }
              `}
            >
              模型配置
            </button>
            <button
              onClick={() => setActiveTab("agents")}
              className={`
                whitespace-nowrap border-b-2 py-3 text-sm font-medium
                ${
                  activeTab === "agents"
                    ? "border-blue-500 text-blue-600 dark:text-blue-400"
                    : "border-transparent text-zinc-500 hover:border-zinc-300 hover:text-zinc-700 dark:text-zinc-400 dark:hover:border-zinc-700 dark:hover:text-zinc-300"
                }
              `}
            >
              智能体方案
            </button>
            <button
              onClick={() => setActiveTab("image2")}
              className={`
                whitespace-nowrap border-b-2 py-3 text-sm font-medium
                ${
                  activeTab === "image2"
                    ? "border-blue-500 text-blue-600 dark:text-blue-400"
                    : "border-transparent text-zinc-500 hover:border-zinc-300 hover:text-zinc-700 dark:text-zinc-400 dark:hover:border-zinc-700 dark:hover:text-zinc-300"
                }
              `}
            >
              Image 2 设置
            </button>
          </nav>
        </div>

        <ErrorBoundary FallbackComponent={ErrorFallback}>
          <div className={activeTab === "models" ? "block" : "hidden"}>
            <ModelConfigWorkspace />
          </div>
          
          <div className={activeTab === "agents" ? "block" : "hidden"}>
            <AgentConfigWorkspace />
          </div>

          <div className={activeTab === "image2" ? "block" : "hidden"}>
            <Image2ConfigWorkspace />
          </div>
        </ErrorBoundary>
      </WorkspaceShell>
    </AppShell>
  );
}
