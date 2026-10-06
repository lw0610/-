import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

describe("tauri config", () => {
  it("allows local backend image previews without permitting arbitrary remote images", () => {
    const parsed = JSON.parse(readFileSync(join(process.cwd(), "src-tauri", "tauri.conf.json"), "utf-8"));
    const imgSrc = String(parsed.app.security.csp).split(";").find((directive) => directive.trim().startsWith("img-src")) || "";
    expect(imgSrc).toContain("http://127.0.0.1:*");
    expect(imgSrc).toContain("http://localhost:*");
    expect(imgSrc.split(/\s+/)).not.toContain("*");
    expect(imgSrc.split(/\s+/)).not.toContain("https:");
    expect(imgSrc.split(/\s+/)).not.toContain("http:");
  });
  it("does not leave CSP disabled", () => {
    const raw = readFileSync(
      join(process.cwd(), "src-tauri", "tauri.conf.json"),
      "utf-8",
    );
    const parsed = JSON.parse(raw) as {
      app?: { security?: { csp?: string | null } };
    };

    expect(parsed.app?.security?.csp).not.toBeNull();
    expect(String(parsed.app?.security?.csp || "").trim()).not.toBe("");
  });
});
