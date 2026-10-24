import { test, expect } from "vitest";

import {
  emptySettingsWorkspaceMetadata,
  getSettingsTask,
  normalizeSettingsWorkspaceMetadata,
  searchSettingDescriptors,
  type SettingDescriptor,
} from "./settingsRegistry";

test("getSettingsTask handles empty array properly", () => {
  expect(getSettingsTask("/settings/system", [])).toBeUndefined();
});

test("normalizeSettingsWorkspaceMetadata keeps subsection children from api payload", () => {
  const metadata = normalizeSettingsWorkspaceMetadata({
    tasks: [
      {
        key: "system",
        href: "/settings/system",
        label: "系统",
        routes: ["/settings/system"],
        children: [
          { id: "system-runtime", label: "系统运行" },
          { id: "system-session", label: "会话策略", hint: "命名与清理" },
        ],
      },
    ],
    descriptors: [
      {
        id: 'system-log-dir',
        task: 'system',
        section: '系统运行',
        path: '/settings/system',
        label: '日志目录',
      }
    ],
  });

  expect(metadata.tasks[0]?.children).toEqual([
    { id: "system-runtime", label: "系统运行" },
    { id: "system-session", label: "会话策略", hint: "命名与清理" },
  ]);
});

test("normalizeSettingsWorkspaceMetadata keeps empty contract explicit instead of falling back", () => {
  expect(normalizeSettingsWorkspaceMetadata(null)).toEqual(emptySettingsWorkspaceMetadata);
  expect(
    normalizeSettingsWorkspaceMetadata({ tasks: [], descriptors: [] }),
  ).toEqual(emptySettingsWorkspaceMetadata);
});

test("normalizeSettingsWorkspaceMetadata preserves read-only mutability when impact is read-only", () => {
  const metadata = normalizeSettingsWorkspaceMetadata({
    tasks: [
      {
        key: "services",
        href: "/settings/services",
        label: "服务状态",
        routes: ["/settings/services"],
      },
    ],
    descriptors: [
      {
        id: "services-runtime",
        task: "services",
        section: "服务状态",
        path: "/settings/services",
        label: "运行态服务",
        impact: "read_only",
      },
    ],
  });

  expect(metadata.descriptors[0]).toMatchObject({
    id: "services-runtime",
    impact: "read_only",
    mutability: "read_only",
  });
});

test("search results rank and filter correctly", () => {
  const descriptors: SettingDescriptor[] = [
    {
      id: 'system-log-dir',
      task: 'system',
      section: '系统运行',
      path: '/settings/system',
      label: '日志目录',
      description: '设置日志输出',
      priority: 10,
      impact: 'instant',
      mutability: 'editable',
    },
    {
      id: 'system-about',
      task: 'about',
      section: '关于',
      path: '/settings/about',
      label: '应用信息',
      description: '查看版本',
      priority: 5,
      impact: 'read_only',
      mutability: 'read_only',
    }
  ];

  const logResults = searchSettingDescriptors("日志", descriptors);
  expect(logResults.length).toBeGreaterThan(0);
  expect(logResults[0]?.id).toBe("system-log-dir");

  const aboutResults = searchSettingDescriptors("版本", descriptors);
  expect(aboutResults[0]?.id).toBe("system-about");
  
  const emptyResults = searchSettingDescriptors("notfound", descriptors);
  expect(emptyResults.length).toBe(0);
});
