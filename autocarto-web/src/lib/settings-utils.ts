export function describeToolMissingReason(
  reason: string,
  fallback = "运行时不可用",
) {
  if (reason === "interactive_instance_disconnected") {
    return "QGIS 交互实例未连接";
  }
  if (reason === "compute_catalog_pending") {
    return "Compute 已启动，工具目录同步中";
  }
  if (reason === "compute_unavailable") {
    return "Compute 服务未就绪";
  }
  return String(reason || "").trim() || fallback;
}

export function formatDateTime(value?: string, emptyLabel = "未记录") {
  const raw = String(value || "").trim();
  if (!raw) {
    return emptyLabel;
  }
  const parsed = new Date(raw);
  if (Number.isNaN(parsed.getTime())) {
    return raw;
  }
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(parsed);
}

export function resolveSkillId(
  skill?: {
    skill_id?: string;
    skill_name?: string;
    name?: string;
  } | null,
): string {
  if (!skill) {
    return "";
  }
  if (typeof skill.skill_id === "string" && skill.skill_id.trim()) {
    return skill.skill_id.trim();
  }
  if (typeof skill.skill_name === "string" && skill.skill_name.trim()) {
    return skill.skill_name.trim();
  }
  if (typeof skill.name === "string" && skill.name.trim()) {
    return skill.name.trim();
  }
  return "";
}
