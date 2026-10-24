function normalizePathname(pathname: string) {
  const trimmed = String(pathname || "").trim();
  if (!trimmed) {
    return "/";
  }
  if (trimmed.length > 1 && trimmed.endsWith("/")) {
    return trimmed.slice(0, -1);
  }
  return trimmed;
}

export function isSettingsPath(pathname: string) {
  const normalized = normalizePathname(pathname);
  return normalized === "/settings" || normalized.startsWith("/settings/");
}

export function isDiagnosticsPath(pathname: string) {
  const normalized = normalizePathname(pathname);
  return normalized === "/diagnostics" || normalized.startsWith("/diagnostics/");
}

export function isToolCenterPath(pathname: string) {
  const normalized = normalizePathname(pathname);
  return normalized === "/tool-center" || normalized.startsWith("/tool-center/");
}

export function isWorkspacePath(pathname: string) {
  return isSettingsPath(pathname) || isDiagnosticsPath(pathname) || isToolCenterPath(pathname) || pathname === "/flows" || pathname.startsWith("/flows/");
}

export function routeIsActive(pathname: string, href: string) {
  const normalizedPath = normalizePathname(pathname);
  const normalizedHref = normalizePathname(href);
  return (
    normalizedPath === normalizedHref ||
    normalizedPath.startsWith(`${normalizedHref}/`)
  );
}
