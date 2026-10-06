export interface SettingsActionStatus {
  label: string;
  tone: "success" | "warning";
}

export interface InlineHeaderStat {
  label: string;
  value: unknown;
}

function normalizeInlineValue(value: unknown) {
  const text = String(value ?? "").trim();
  return text || "-";
}

export function buildSettingsActionStatus(
  message: string,
  warning?: string,
): SettingsActionStatus {
  const detail = [message, warning].filter(Boolean).join("；");
  return {
    label: `最近操作：${detail}`,
    tone: warning ? "warning" : "success",
  };
}

export function formatInlineHeaderStats(stats: InlineHeaderStat[]) {
  return stats
    .map((stat) => {
      const label = String(stat.label || "").trim();
      if (!label) {
        return "";
      }
      return `${label} ${normalizeInlineValue(stat.value)}`.trim();
    })
    .filter(Boolean);
}
