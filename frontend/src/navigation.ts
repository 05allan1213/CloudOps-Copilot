export type WorkspaceKey =
  | "overview"
  | "infrastructure"
  | "monitoring"
  | "alerts"
  | "logs"
  | "traces"
  | "agent"
  | "incidents"
  | "devops"
  | "settings";

export type NavigationIcon =
  | "i-lucide-layout-dashboard"
  | "i-lucide-server"
  | "i-lucide-gauge"
  | "i-lucide-siren"
  | "i-lucide-logs"
  | "i-lucide-scan-search"
  | "i-lucide-bot"
  | "i-lucide-stethoscope"
  | "i-lucide-git-pull-request"
  | "i-lucide-settings";

export interface PrimaryNavigationItem {
  index: `/${WorkspaceKey}`;
  title: string;
  shortTitle: string;
  icon: NavigationIcon;
  group: "observe" | "operate" | "system";
}

export interface NavigationGroup {
  id: PrimaryNavigationItem["group"];
  title: string;
  items: readonly PrimaryNavigationItem[];
}

export const navigationGroups: readonly NavigationGroup[] = [
  {
    id: "observe",
    title: "运行态",
    items: [
      { index: "/overview", title: "总览", shortTitle: "总览", icon: "i-lucide-layout-dashboard", group: "observe" },
      { index: "/infrastructure", title: "基础设施", shortTitle: "设施", icon: "i-lucide-server", group: "observe" },
      { index: "/monitoring", title: "监控", shortTitle: "监控", icon: "i-lucide-gauge", group: "observe" },
      { index: "/alerts", title: "告警", shortTitle: "告警", icon: "i-lucide-siren", group: "observe" },
      { index: "/logs", title: "日志", shortTitle: "日志", icon: "i-lucide-logs", group: "observe" },
      { index: "/traces", title: "链路", shortTitle: "链路", icon: "i-lucide-scan-search", group: "observe" },
    ],
  },
  {
    id: "operate",
    title: "处置",
    items: [
      { index: "/incidents", title: "事件", shortTitle: "事件", icon: "i-lucide-stethoscope", group: "operate" },
      { index: "/devops", title: "DevOps", shortTitle: "DevOps", icon: "i-lucide-git-pull-request", group: "operate" },
    ],
  },
  {
    id: "system",
    title: "系统",
    items: [
      { index: "/settings", title: "设置", shortTitle: "设置", icon: "i-lucide-settings", group: "system" },
    ],
  },
];

export const primaryNavigation: readonly PrimaryNavigationItem[] = navigationGroups.flatMap((group) => group.items);

export const agentNavigation: PrimaryNavigationItem = {
  index: "/agent",
  title: "Agent",
  shortTitle: "Agent",
  icon: "i-lucide-bot",
  group: "operate",
};

export const workspacePaths: readonly PrimaryNavigationItem["index"][] = [
  ...primaryNavigation.map((item) => item.index),
  agentNavigation.index,
];
