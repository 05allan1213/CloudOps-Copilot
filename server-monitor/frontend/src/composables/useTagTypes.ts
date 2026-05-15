export function severityTagType(severity: string | undefined): "" | "success" | "warning" | "danger" | "info" {
  switch (severity) {
    case "critical":
      return "danger";
    case "high":
      return "danger";
    case "warning":
      return "warning";
    case "info":
      return "info";
    default:
      return "info";
  }
}

export function statusTagType(status: string | undefined): "" | "success" | "warning" | "danger" | "info" | "primary" {
  switch (status) {
    case "pending":
      return "warning";
    case "approved":
      return "primary";
    case "executed":
      return "success";
    case "rejected":
      return "danger";
    case "failed":
      return "danger";
    case "firing":
      return "danger";
    case "resolved":
      return "success";
    default:
      return "info";
  }
}

export function riskTagType(level: string | undefined): "" | "success" | "warning" | "danger" | "info" {
  switch (level) {
    case "high":
      return "danger";
    case "medium":
      return "warning";
    case "low":
      return "info";
    default:
      return "info";
  }
}
