export type IncidentStatus = "investigating" | "waiting" | "executing" | "verifying" | "closed";
export type IncidentSeverity = "critical" | "warning" | "info";

export interface IncidentRow {
  id: string;
  summary: string;
  status: IncidentStatus;
  severity: IncidentSeverity;
  service: string;
  namespace: string;
  owner: string;
  updatedAt: string;
  evidence: number;
  hash: string;
}

const summaries = [
  "API 请求错误率持续高于发布基线",
  "支付回调延迟超过 SLO 并出现 Provider 分歧",
  "Deployment 可用副本与期望状态不一致",
  "关键依赖出现间歇性超时与重试放大",
  "日志摄取速率下降且部分分片不可用",
  "变更已派发但当前 Kubernetes 观察尚未闭合",
];
const services = ["cloudops-api", "checkout", "payments", "otel-collector", "prometheus", "worker"];
const statuses: IncidentStatus[] = ["investigating", "waiting", "executing", "verifying", "closed"];
const severities: IncidentSeverity[] = ["critical", "warning", "info"];

export const incidents: IncidentRow[] = Array.from({ length: 48 }, (_, index) => {
  const suffix = String(index + 1).padStart(2, "0");
  return {
    id: `inc-2026-07-${suffix}`,
    summary: summaries[index % summaries.length] + (index === 3 ? "，需要核对一个非常长的中文服务名称与精确责任边界" : ""),
    status: statuses[index % statuses.length],
    severity: severities[index % severities.length],
    service: services[index % services.length],
    namespace: index % 3 === 0 ? "cloudops-system" : "demo",
    owner: index % 4 === 0 ? "平台值班" : "应用值班",
    updatedAt: `2026-07-30T${String(8 + (index % 8)).padStart(2, "0")}:${String((index * 7) % 60).padStart(2, "0")}:00Z`,
    evidence: 3 + (index % 12),
    hash: `${(index + 17).toString(16).padStart(2, "0")}${"7f81aa0bb4d1c9e2".repeat(4)}`.slice(0, 64),
  };
});

export const statusLabel: Record<IncidentStatus, string> = {
  investigating: "调查中",
  waiting: "等待决策",
  executing: "执行中",
  verifying: "恢复验证",
  closed: "已关闭",
};

export const severityLabel: Record<IncidentSeverity, string> = {
  critical: "严重",
  warning: "警告",
  info: "信息",
};
