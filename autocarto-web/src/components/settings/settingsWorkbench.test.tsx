import test from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";

import DetailList from "./DetailList";
import ListDetailLayout from "./ListDetailLayout";
import ListPanelHeader from "./ListPanelHeader";
import MetricTile from "./MetricTile";
import PageHeader from "./PageHeader";

test("ListDetailLayout preserves sticky list rail classes when requested", () => {
  const html = renderToStaticMarkup(
    <ListDetailLayout stickyList list={<div>左列</div>} detail={<div>右列</div>} />,
  );

  assert.match(html, /左列/);
  assert.match(html, /右列/);
  assert.match(html, /lg:sticky/);
});

test("ListDetailLayout renders collapsed mobile list entry for long directory rails", () => {
  const html = renderToStaticMarkup(
    <ListDetailLayout
      listTitle="方案目录"
      listSectionId="agent-config-list"
      list={<div>目录内容</div>}
      detail={<div>详情内容</div>}
    />,
  );

  assert.match(html, /id="agent-config-list"/);
  assert.match(html, /settings-anchor-target/);
  assert.match(html, /方案目录/);
  assert.match(html, /lg:hidden/);
  assert.match(html, /详情内容/);
});

test("ListPanelHeader renders compact inline stats instead of metric tiles", () => {
  const html = renderToStaticMarkup(
    <ListPanelHeader
      eyebrow="Provider 目录"
      title="模型配置集合"
      description="紧凑统计"
      stats={[
        { label: "结果", value: "8/12" },
        { label: "激活", value: "openai-main" },
      ]}
    >
      <div>筛选器</div>
    </ListPanelHeader>,
  );

  assert.match(html, /Provider 目录/);
  assert.match(html, /模型配置集合/);
  assert.match(html, /<dl/);
  assert.match(html, /结果/);
  assert.match(html, /openai-main/);
  assert.doesNotMatch(html, /min-w-\[72px\]/);
});

test("PageHeader renders description as tooltip trigger instead of paragraph copy", () => {
  const html = renderToStaticMarkup(
    <PageHeader title="诊断工作台" description="顶部辅助说明" />,
  );

  assert.match(html, /诊断工作台/);
  assert.match(html, /aria-label="顶部辅助说明"/);
  assert.doesNotMatch(html, /max-w-3xl text-\[11px\] leading-4/);
});

test("ListPanelHeader renders description as tooltip trigger instead of stacked helper text", () => {
  const html = renderToStaticMarkup(
    <ListPanelHeader
      eyebrow="工具组目录"
      title="工具组检索"
      description="切换右侧编辑区"
    />,
  );

  assert.match(html, /工具组检索/);
  assert.match(html, /aria-label="切换右侧编辑区"/);
  assert.doesNotMatch(html, /mt-0\.5 text-\[11px\] leading-4 text-zinc-500/);
});

test("MetricTile renders hint as tooltip trigger without third text row", () => {
  const html = renderToStaticMarkup(
    <MetricTile label="字符数" value="128" hint="用于快速了解说明长度" />,
  );

  assert.match(html, /字符数/);
  assert.match(html, /128/);
  assert.match(html, /aria-label="用于快速了解说明长度"/);
  assert.doesNotMatch(html, /leading-4 text-zinc-500/);
});

test("DetailList renders item hint as tooltip trigger without extra block text", () => {
  const html = renderToStaticMarkup(
    <DetailList
      items={[
        {
          label: "配置文件",
          value: "/tmp/config.yaml",
          hint: "运行时读取该路径",
        },
      ]}
    />,
  );

  assert.match(html, /配置文件/);
  assert.match(html, /\/tmp\/config\.yaml/);
  assert.match(html, /aria-label="运行时读取该路径"/);
  assert.doesNotMatch(html, /<dd class="mt-0\.5 text-\[11px\] leading-4 text-zinc-500/);
});
