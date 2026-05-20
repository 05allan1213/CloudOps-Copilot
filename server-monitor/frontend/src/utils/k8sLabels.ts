const k8sPhaseLabels: Record<string, string> = {
  Running: "运行中",
  Pending: "等待中",
  Failed: "失败",
  Succeeded: "已完成",
  Unknown: "未知",
};

const k8sNodeStatusLabels: Record<string, string> = {
  Ready: "就绪",
  NotReady: "未就绪",
};

const k8sEventTypes: Record<string, string> = {
  Normal: "正常",
  Warning: "警告",
};

const k8sJobStatusLabels: Record<string, string> = {
  Completed: "已完成",
  Failed: "失败",
  Running: "运行中",
  Suspended: "已暂停",
};

const k8sPVStatusLabels: Record<string, string> = {
  Bound: "已绑定",
  Available: "可用",
  Released: "已释放",
  Failed: "失败",
};

const k8sPVCStatusLabels: Record<string, string> = {
  Bound: "已绑定",
  Pending: "等待中",
  Lost: "丢失",
};

export function k8sPhaseLabel(phase: string): string {
  return k8sPhaseLabels[phase] ?? phase;
}

export function k8sNodeStatusLabel(status: string): string {
  return k8sNodeStatusLabels[status] ?? status;
}

export function k8sEventTypeLabel(type: string): string {
  return k8sEventTypes[type] ?? type;
}

export function k8sJobStatusLabel(status: string): string {
  return k8sJobStatusLabels[status] ?? status;
}

export function k8sPVStatusLabel(status: string): string {
  return k8sPVStatusLabels[status] ?? status;
}

export function k8sPVCStatusLabel(status: string): string {
  return k8sPVCStatusLabels[status] ?? status;
}

export function k8sDeploymentStatusLabel(available: number, replicas: number): string {
  return available >= replicas ? "正常" : "异常";
}
