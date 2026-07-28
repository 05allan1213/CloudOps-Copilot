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
  | "LayoutDashboard"
  | "Server"
  | "Gauge"
  | "Siren"
  | "Logs"
  | "ScanSearch"
  | "Bot"
  | "FirstAid"
  | "GitPullRequest"
  | "Settings";

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
      { index: "/overview", title: "总览", shortTitle: "总览", icon: "LayoutDashboard", group: "observe" },
      { index: "/infrastructure", title: "基础设施", shortTitle: "设施", icon: "Server", group: "observe" },
      { index: "/monitoring", title: "监控", shortTitle: "监控", icon: "Gauge", group: "observe" },
      { index: "/alerts", title: "告警", shortTitle: "告警", icon: "Siren", group: "observe" },
      { index: "/logs", title: "日志", shortTitle: "日志", icon: "Logs", group: "observe" },
      { index: "/traces", title: "链路", shortTitle: "链路", icon: "ScanSearch", group: "observe" },
    ],
  },
  {
    id: "operate",
    title: "处置",
    items: [
      { index: "/agent", title: "Agent", shortTitle: "Agent", icon: "Bot", group: "operate" },
      { index: "/incidents", title: "事件", shortTitle: "事件", icon: "FirstAid", group: "operate" },
      { index: "/devops", title: "DevOps", shortTitle: "DevOps", icon: "GitPullRequest", group: "operate" },
    ],
  },
  {
    id: "system",
    title: "系统",
    items: [
      { index: "/settings", title: "设置", shortTitle: "设置", icon: "Settings", group: "system" },
    ],
  },
];

export const primaryNavigation: readonly PrimaryNavigationItem[] = navigationGroups.flatMap((group) => group.items);

const mobilePaths = new Set(["/overview", "/alerts", "/agent", "/incidents"]);
export const mobilePrimaryNavigation = primaryNavigation.filter((item) => mobilePaths.has(item.index));

const mobileMorePaths = new Set(["/infrastructure", "/monitoring", "/logs", "/traces", "/devops", "/settings"]);
export const mobileMoreNavigation = primaryNavigation.filter((item) => mobileMorePaths.has(item.index));
