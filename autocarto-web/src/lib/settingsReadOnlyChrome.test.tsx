import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { expect, test } from "vitest";

import SettingSection from "@/components/settings/SettingSection";

test("shared read-only chrome renders one neutral badge", () => {
  const html = renderToStaticMarkup(
    <SettingSection title="服务状态" impact="read_only">
      <div>内容</div>
    </SettingSection>,
  );

  expect((html.match(/>只读</g) ?? []).length).toBe(1);
  expect(html).not.toContain("诊断只读");
});
