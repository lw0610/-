import { test, expect } from "vitest";

import {
  DEFAULT_CUSTOM_NAME_PATTERN,
  PROJECT_SEQ_PATTERN,
  TIMESTAMP_PATTERN,
  formatArgs,
  formatStaticProviders,
  inferPatternMode,
  parseArgs,
  parseRange,
  parseStaticProvidersText,
  resolvePattern,
} from "./config-form-helpers";

test("parseRange keeps bounded port tuples", () => {
  expect(parseRange([70000, 0], [50000, 50099])).toEqual([65535, 1]);
  expect(parseRange(undefined, [50000, 50099])).toEqual([50000, 50099]);
});

test("argument helpers convert between newline text and arrays", () => {
  expect(parseArgs("--port\n50100\n")).toEqual(["--port", "50100"]);
  expect(formatArgs(["--port", "50100"])).toBe("--port\n50100");
  expect(formatArgs(undefined)).toBe("");
});

test("static provider helpers parse both equals and comma forms", () => {
  expect(parseStaticProvidersText("compute=http://127.0.0.1:50301")).toEqual([
    { id: "compute", base_url: "http://127.0.0.1:50301" },
  ]);
  expect(parseStaticProvidersText("middleware,http://127.0.0.1:50100")).toEqual([
    { id: "middleware", base_url: "http://127.0.0.1:50100" },
  ]);
  expect(
    formatStaticProviders([{ id: "compute", base_url: "http://127.0.0.1:50301" }]),
  ).toBe("compute=http://127.0.0.1:50301");
});

test("name pattern helpers preserve preset and custom modes", () => {
  expect(inferPatternMode(TIMESTAMP_PATTERN)).toEqual({
    mode: "timestamp",
    custom: DEFAULT_CUSTOM_NAME_PATTERN,
  });
  expect(inferPatternMode(PROJECT_SEQ_PATTERN)).toEqual({
    mode: "project-seq",
    custom: DEFAULT_CUSTOM_NAME_PATTERN,
  });
  expect(inferPatternMode("项目-{date}")).toEqual({
    mode: "custom",
    custom: "项目-{date}",
  });
  expect(
    resolvePattern({
      namePatternMode: "custom",
      customNamePattern: "项目-{date}",
    }),
  ).toBe("项目-{date}");
});
