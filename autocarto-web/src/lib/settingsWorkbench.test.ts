import { test, expect } from "vitest";

import {
  buildSettingsActionStatus,
  formatInlineHeaderStats,
} from "./settingsWorkbench";

test("buildSettingsActionStatus keeps success feedback lightweight", () => {
  expect(buildSettingsActionStatus("LLM 配置已保存")).toEqual({
    label: "最近操作：LLM 配置已保存",
    tone: "success",
  });
});

test("buildSettingsActionStatus upgrades to warning when mutation includes follow-up notice", () => {
  expect(
    buildSettingsActionStatus("工具组已删除", "仍有实例等待同步"),
  ).toEqual({
      label: "最近操作：工具组已删除；仍有实例等待同步",
      tone: "warning",
    });
});

test("formatInlineHeaderStats collapses card stats into compact text chips", () => {
  expect(
    formatInlineHeaderStats([
      { label: "结果", value: "8/12" },
      { label: "分类", value: 4 },
    ]),
  ).toEqual(["结果 8/12", "分类 4"]);
});
