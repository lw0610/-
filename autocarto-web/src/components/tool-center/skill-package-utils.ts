export function normalizeKindLabel(kind: string) {
  switch (
    String(kind || "")
      .trim()
      .toLowerCase()
  ) {
    case "category":
      return "分类包";
    case "workflow":
      return "流程包";
    case "knowledge":
      return "知识包";
    default:
      return kind || "未分类";
  }
}

export function normalizeScopeLabel(scope: string) {
  switch (
    String(scope || "")
      .trim()
      .toLowerCase()
  ) {
    case "builtin":
      return "内置";
    case "user":
      return "用户";
    case "exclusive":
      return "专属";
    case "global":
      return "全局";
    default:
      return scope || "全局";
  }
}

export function packageKey(
  item?: {
    package_id?: string;
    scope?: string;
    owner_config?: string;
  } | null,
) {
  const packageId = String(item?.package_id || "").trim();
  const scope = String(item?.scope || "all").trim() || "all";
  const ownerConfig = String(item?.owner_config || "").trim();
  return `${packageId}::${scope}::${ownerConfig}`;
}
