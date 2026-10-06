import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { expect, test, vi } from "vitest";

import AboutPage from "./page";

vi.mock("next/image", () => ({
  default: ({ alt }: { alt: string; height: number; src: string; width: number }) => (
    <span data-next-image-alt={alt} />
  ),
}));

vi.mock("@/components/layout/AppShell", () => ({
  default: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

vi.mock("@/components/layout/WorkspaceShell", () => ({
  default: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

vi.mock("@/components/settings/FormSection", () => ({
  default: ({ children, description, title }: { children: React.ReactNode; description?: React.ReactNode; title: string }) => (
    <section>
      <h2>{title}</h2>
      {description ? <p>{description}</p> : null}
      {children}
    </section>
  ),
}));

test("about settings page exposes exactly one neutral read-only badge", () => {
  const html = renderToStaticMarkup(<AboutPage />);

  expect((html.match(/>只读</g) ?? []).length).toBe(1);
  expect(html).not.toContain("诊断只读");
});
