"use client";

import Image from "next/image";
import tauriConfig from "../../../../src-tauri/tauri.conf.json";
import AppShell from "@/components/layout/AppShell";
import WorkspaceShell from "@/components/layout/WorkspaceShell";
import SettingSection from "@/components/settings/SettingSection";

export default function AboutPage() {
  const appVersion = tauriConfig.version || "0.1.0";
  const appName = tauriConfig.productName || "AutoCarto AI";

  return (
    <AppShell>
      <WorkspaceShell>
        <div className="space-y-6">
          <div className="flex flex-col items-center justify-center py-12 text-center">
            <div className="mb-4 flex items-center justify-center">
              <Image src="/logo.svg" alt="AutoCarto AI Logo" width={64} height={64} />
            </div>
            <h1 className="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-50">
              {appName}
            </h1>
            <p className="mt-2 text-sm text-zinc-500 dark:text-zinc-400">
              基于多智能体系统的 GIS 智能助手桌面应用
            </p>
            <div className="mt-4 inline-flex items-center rounded-full border border-zinc-200 bg-zinc-50 px-3 py-1 text-xs font-medium text-zinc-600 dark:border-zinc-800 dark:bg-zinc-900/50 dark:text-zinc-300">
              Version {appVersion}
            </div>
          </div>

          <SettingSection
            title="关于项目"
            description="AutoCarto AI 通过 AI 技术增强 GIS 数据处理、空间分析和地图制图能力，将自然语言指令转化为 QGIS 操作。"
            impact="read_only"
          >
            <div className="rounded-lg border border-zinc-200 bg-white p-4 dark:border-zinc-800 dark:bg-zinc-900/50">
              <dl className="divide-y divide-zinc-200 text-sm dark:divide-zinc-800">
                <div className="flex justify-between py-3">
                  <dt className="font-medium text-zinc-500 dark:text-zinc-400">版本号</dt>
                  <dd className="text-zinc-900 dark:text-zinc-100">{appVersion}</dd>
                </div>
                <div className="flex justify-between py-3">
                  <dt className="font-medium text-zinc-500 dark:text-zinc-400">核心架构</dt>
                  <dd className="text-zinc-900 dark:text-zinc-100">Next.js + Tauri + Go + Python</dd>
                </div>
                <div className="flex justify-between py-3">
                  <dt className="font-medium text-zinc-500 dark:text-zinc-400">多智能体后端</dt>
                  <dd className="text-zinc-900 dark:text-zinc-100">carto_agent (Go)</dd>
                </div>
                <div className="flex justify-between py-3">
                  <dt className="font-medium text-zinc-500 dark:text-zinc-400">GIS 引擎</dt>
                  <dd className="text-zinc-900 dark:text-zinc-100">QGIS Plugin</dd>
                </div>
              </dl>
            </div>
          </SettingSection>

        </div>
      </WorkspaceShell>
    </AppShell>
  );
}
