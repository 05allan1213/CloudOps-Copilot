export type WorkspaceStateKind =
  | "loading"
  | "empty"
  | "error"
  | "partial"
  | "stale"
  | "disconnected"
  | "permission-denied"
  | "expired"
  | "invalid"
  | "deleted";

export type PresentationColor = "error" | "warning" | "info" | "neutral" | "success";

export interface WorkspaceStateDefinition {
  title: string;
  description: string;
  icon: string;
  color: PresentationColor;
  role: "alert" | "status";
}

const workspaceStateDefinitions: Record<WorkspaceStateKind, WorkspaceStateDefinition> = {
  loading: {
    title: "正在读取当前数据",
    description: "首次内容加载会保留稳定布局。",
    icon: "i-lucide-loader-circle",
    color: "neutral",
    role: "status",
  },
  empty: {
    title: "当前条件下没有结果",
    description: "Scope、筛选与时间范围保持不变。",
    icon: "i-lucide-inbox",
    color: "neutral",
    role: "status",
  },
  error: {
    title: "读取失败",
    description: "已加载上下文和输入保持不变。",
    icon: "i-lucide-circle-x",
    color: "error",
    role: "alert",
  },
  partial: {
    title: "仅返回部分结果",
    description: "可用结果继续显示，缺失来源单独标明。",
    icon: "i-lucide-split",
    color: "warning",
    role: "status",
  },
  stale: {
    title: "数据连续性已过期",
    description: "页面停止声明实时完整，现有内容仍可读取。",
    icon: "i-lucide-cloud-off",
    color: "neutral",
    role: "status",
  },
  disconnected: {
    title: "实时连接已断开",
    description: "现有内容保持原位，重连不会移动当前阅读位置。",
    icon: "i-lucide-unplug",
    color: "warning",
    role: "status",
  },
  "permission-denied": {
    title: "无权访问当前目标",
    description: "周边 Scope、筛选和列表上下文保持可见。",
    icon: "i-lucide-shield-x",
    color: "error",
    role: "alert",
  },
  expired: {
    title: "当前授权已过期",
    description: "旧 authority 或 exact hash 不会被继续使用。",
    icon: "i-lucide-key-round",
    color: "error",
    role: "alert",
  },
  invalid: {
    title: "目标标识无效",
    description: "不会自动选择第一行或推断替代目标。",
    icon: "i-lucide-circle-off",
    color: "warning",
    role: "alert",
  },
  deleted: {
    title: "目标已删除",
    description: "不会使用缓存内容推断当前事实。",
    icon: "i-lucide-trash-2",
    color: "warning",
    role: "alert",
  },
};

export function workspaceStateDefinition(kind: WorkspaceStateKind): WorkspaceStateDefinition {
  return workspaceStateDefinitions[kind];
}

export type RealtimeTrustState =
  | "connecting"
  | "live"
  | "reconnecting"
  | "disconnected"
  | "stale"
  | "cursor-expired"
  | "resyncing"
  | "resync-failed"
  | "stopped";

export interface RealtimeTrustDefinition {
  label: string;
  claim: string;
  icon: string;
  color: PresentationColor;
  live: boolean;
  animated: boolean;
}

const realtimeTrustDefinitions: Record<RealtimeTrustState, RealtimeTrustDefinition> = {
  connecting: {
    label: "Connecting",
    claim: "正在建立实时连接",
    icon: "i-lucide-loader-circle",
    color: "info",
    live: false,
    animated: true,
  },
  live: {
    label: "Live",
    claim: "游标连续，当前可声明实时",
    icon: "i-lucide-radio",
    color: "success",
    live: true,
    animated: false,
  },
  reconnecting: {
    label: "Reconnecting",
    claim: "现有内容可读，新事件暂缓",
    icon: "i-lucide-refresh-cw",
    color: "warning",
    live: false,
    animated: true,
  },
  disconnected: {
    label: "Disconnected",
    claim: "连接已断开，不声明实时",
    icon: "i-lucide-unplug",
    color: "warning",
    live: false,
    animated: false,
  },
  stale: {
    label: "Stale",
    claim: "连续性不可信，保留当前内容",
    icon: "i-lucide-cloud-off",
    color: "neutral",
    live: false,
    animated: false,
  },
  "cursor-expired": {
    label: "Cursor expired",
    claim: "旧游标不可继续，等待有界重同步",
    icon: "i-lucide-history",
    color: "error",
    live: false,
    animated: false,
  },
  resyncing: {
    label: "Resyncing",
    claim: "正在核对有界完整窗口",
    icon: "i-lucide-list-restart",
    color: "info",
    live: false,
    animated: true,
  },
  "resync-failed": {
    label: "Resync failed",
    claim: "重同步失败，继续保持 stale",
    icon: "i-lucide-circle-x",
    color: "error",
    live: false,
    animated: false,
  },
  stopped: {
    label: "Stopped",
    claim: "连接与监听器已清理",
    icon: "i-lucide-power",
    color: "neutral",
    live: false,
    animated: false,
  },
};

export function realtimeTrustDefinition(state: RealtimeTrustState): RealtimeTrustDefinition {
  return realtimeTrustDefinitions[state];
}

export type RiskConfirmationKind =
  | "acknowledgement"
  | "configuration"
  | "approval"
  | "rollback"
  | "forced-termination";

export interface RiskConfirmationFacts {
  target: string;
  effect: string;
  authority?: string;
  exactHash?: string;
  version?: string;
  irreversible?: string;
  recovery?: string;
}

export interface RiskConfirmationDefinition {
  title: string;
  confirmLabel: string;
  icon: string;
  color: "primary" | "warning" | "error";
  dismissible: boolean;
  requiredFacts: readonly (keyof RiskConfirmationFacts)[];
}

const riskConfirmationDefinitions: Record<RiskConfirmationKind, RiskConfirmationDefinition> = {
  acknowledgement: {
    title: "确认已知悉影响",
    confirmLabel: "确认已知悉",
    icon: "i-lucide-circle-check",
    color: "primary",
    dismissible: true,
    requiredFacts: ["target", "effect"],
  },
  configuration: {
    title: "应用可逆配置",
    confirmLabel: "应用配置",
    icon: "i-lucide-sliders-horizontal",
    color: "warning",
    dismissible: true,
    requiredFacts: ["target", "effect", "recovery"],
  },
  approval: {
    title: "批准精确版本",
    confirmLabel: "批准精确版本",
    icon: "i-lucide-file-key-2",
    color: "warning",
    dismissible: false,
    requiredFacts: ["target", "effect", "authority", "exactHash"],
  },
  rollback: {
    title: "确认回滚",
    confirmLabel: "确认回滚",
    icon: "i-lucide-rotate-ccw",
    color: "error",
    dismissible: false,
    requiredFacts: ["target", "effect", "authority", "version", "recovery"],
  },
  "forced-termination": {
    title: "强制终止当前操作",
    confirmLabel: "强制终止",
    icon: "i-lucide-octagon-x",
    color: "error",
    dismissible: false,
    requiredFacts: ["target", "effect", "authority", "version", "irreversible"],
  },
};

export function riskConfirmationDefinition(kind: RiskConfirmationKind): RiskConfirmationDefinition {
  return riskConfirmationDefinitions[kind];
}

export function missingRiskConfirmationFacts(
  kind: RiskConfirmationKind,
  facts: RiskConfirmationFacts,
): (keyof RiskConfirmationFacts)[] {
  return riskConfirmationDefinitions[kind].requiredFacts.filter((key) => !facts[key]?.trim());
}
