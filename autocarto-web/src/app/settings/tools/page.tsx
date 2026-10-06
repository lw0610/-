"use client";

import AppShell from "@/components/layout/AppShell";
import WorkspaceShell from "@/components/layout/WorkspaceShell";
import ToolCenterWorkspace from "@/components/tool-center/ToolCenterWorkspace";

export default function ToolsSettingsPage() {
  return (
    <AppShell>
      <WorkspaceShell>
        <ToolCenterWorkspace />
      </WorkspaceShell>
    </AppShell>
  );
}
