export function formatTime(value?: string): string {
  if (!value) return "-";
  return new Date(value).toLocaleString("zh-CN", { hour12: false });
}

export function formatTimeShort(value?: string): string {
  if (!value) return "--";
  return new Date(value).toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function severityClass(severity: string | undefined): string {
  switch (severity) {
    case "critical":
      return "severity-critical";
    case "warning":
      return "severity-warning";
    case "info":
      return "severity-info";
    default:
      return "severity-unknown";
  }
}

export function statusLabel(status: string): string {
  switch (status) {
    case "firing":
      return "告警中";
    case "resolved":
      return "已恢复";
    case "pending":
      return "待审批";
    case "approved":
      return "已审批";
    case "rejected":
      return "已拒绝";
    case "executing":
      return "执行中";
    case "executed":
      return "已执行";
    case "failed":
      return "执行失败";
    case "completed":
      return "已完成";
    default:
      return status;
  }
}
