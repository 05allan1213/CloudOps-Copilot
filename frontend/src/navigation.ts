export interface PrimaryNavigationItem {
  index: string;
  title: string;
  icon: "FirstAidKit";
  group: "incident";
}

export const primaryNavigation: readonly PrimaryNavigationItem[] = [
  { index: "/incidents", title: "Incident Workbench", icon: "FirstAidKit", group: "incident" },
];
