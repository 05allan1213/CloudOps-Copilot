<script setup lang="ts">
import { computed, markRaw } from "vue";
import { useRoute, useRouter } from "vue-router";

import {
  AlarmClock,
  Bell,
  Box,
  ChatDotRound,
  CircleCheck,
  Clock,
  Coin,
  Connection,
  DataLine,
  Document,
  FirstAidKit,
  Grid,
  Guide,
  Message,
  Monitor,
  Odometer,
  Operation,
  Setting,
  User,
} from "@element-plus/icons-vue";

import { useAuthStore } from "../../stores/auth";
import { useMonitorStore } from "../../stores/monitor";

defineProps<{
  collapsed: boolean;
}>();

const route = useRoute();
const router = useRouter();
const auth = useAuthStore();
const monitor = useMonitorStore();

const iconMap: Record<string, typeof Monitor> = {
  Monitor: markRaw(Monitor),
  CircleCheck: markRaw(CircleCheck),
  Bell: markRaw(Bell),
  Clock: markRaw(Clock),
  ChatDotRound: markRaw(ChatDotRound),
  FirstAidKit: markRaw(FirstAidKit),
  Operation: markRaw(Operation),
  Document: markRaw(Document),
  Setting: markRaw(Setting),
  AlarmClock: markRaw(AlarmClock),
  Message: markRaw(Message),
  User: markRaw(User),
  Grid: markRaw(Grid),
  Box: markRaw(Box),
  Connection: markRaw(Connection),
  Coin: markRaw(Coin),
  DataLine: markRaw(DataLine),
  Guide: markRaw(Guide),
  Odometer: markRaw(Odometer),
};

interface MenuItem {
  index: string;
  title: string;
  icon?: string;
  group: string;
  admin?: boolean;
  nodesRequired?: boolean;
  children?: MenuItem[];
}

const menuItems: MenuItem[] = [
  { index: "/", title: "总览", icon: "Monitor", group: "monitor" },
  { index: "/hosts", title: "主机", icon: "Monitor", group: "monitor" },
  { index: "/status", title: "状态", icon: "CircleCheck", group: "monitor" },
  {
    index: "k8s-group",
    title: "K8s",
    icon: "Grid",
    group: "k8s",
    children: [
      { index: "/k8s", title: "集群概览", icon: "Grid", group: "k8s" },
      { index: "/k8s/nodes", title: "Nodes", icon: "Monitor", group: "k8s", nodesRequired: true },
      { index: "/k8s/workloads", title: "Workloads", icon: "Box", group: "k8s" },
      { index: "/k8s/services", title: "Services", icon: "Connection", group: "k8s" },
      { index: "/k8s/configmaps", title: "ConfigMaps", icon: "Document", group: "k8s" },
      { index: "/k8s/ingresses", title: "Ingress", icon: "Guide", group: "k8s" },
      { index: "/k8s/storage", title: "Storage", icon: "Coin", group: "k8s" },
      { index: "/k8s/quotas", title: "Quotas", icon: "DataLine", group: "k8s" },
      { index: "/k8s/hpas", title: "HPA", icon: "Odometer", group: "k8s" },
      { index: "/k8s/topology", title: "拓扑图", icon: "Share", group: "k8s" },
      { index: "/k8s/events", title: "Events", icon: "Bell", group: "k8s" },
    ],
  },
  {
    index: "alert-group",
    title: "告警",
    icon: "Bell",
    group: "alert",
    children: [
      { index: "/alerts", title: "当前告警", icon: "Bell", group: "alert" },
      { index: "/alert-histories", title: "历史告警", icon: "Clock", group: "alert" },
    ],
  },
  {
    index: "ai-group",
    title: "智能",
    icon: "ChatDotRound",
    group: "ai",
    children: [
      { index: "/copilot", title: "Copilot", icon: "ChatDotRound", group: "ai" },
      { index: "/diagnosis", title: "诊断", icon: "FirstAidKit", group: "ai" },
    ],
  },
  { index: "/actions", title: "动作", icon: "Operation", group: "admin", admin: true },
  { index: "/audit-logs", title: "审计日志", icon: "Document", group: "admin", admin: true },
  {
    index: "settings-group",
    title: "设置",
    icon: "Setting",
    group: "settings",
    admin: true,
    children: [
      { index: "/settings/alert-rules", title: "告警规则", icon: "AlarmClock", group: "settings", admin: true },
      { index: "/settings/channels", title: "通知渠道", icon: "Message", group: "settings", admin: true },
      { index: "/settings/users", title: "用户管理", icon: "User", group: "settings", admin: true },
    ],
  },
];

const visibleItems = computed(() =>
  menuItems
    .filter((item) => !item.admin || auth.isAdmin)
    .filter((item) => item.group !== "k8s" || monitor.k8sApiEnabled)
    .map((item) => {
      if (!item.children) return item;
      return {
        ...item,
        children: item.children.filter((child) => {
          if (child.group === "k8s" && !monitor.k8sApiEnabled) return false;
          if (child.nodesRequired && !monitor.k8sNodesEnabled) return false;
          return true;
        }),
      };
    }),
);

const activeMenu = computed(() => {
  const path = route.path;

  if (path.startsWith("/hosts/")) return "/hosts";
  if (path.startsWith("/k8s/nodes/")) return "/k8s/nodes";
  if (path.startsWith("/diagnosis/")) return "/diagnosis";
  if (path.startsWith("/actions/")) return "/actions";
  if (path.startsWith("/settings/")) return path;

  return path;
});

const defaultOpeneds = computed(() => {
  const groups: string[] = [];
  const path = route.path;
  if (path.startsWith("/alerts") || path.startsWith("/alert-histories")) {
    groups.push("alert-group");
  }
  if (path.startsWith("/k8s")) {
    groups.push("k8s-group");
  }
  if (path.startsWith("/copilot") || path.startsWith("/diagnosis")) {
    groups.push("ai-group");
  }
  if (path.startsWith("/settings/")) {
    groups.push("settings-group");
  }
  return groups;
});

function handleSelect(index: string) {
  if (index.includes("-group")) return;
  router.push(index);
}
</script>

<template>
  <el-menu
    :default-active="activeMenu"
    :default-openeds="defaultOpeneds"
    :collapse="collapsed"
    :collapse-transition="false"
    class="sidebar-menu"
    background-color="transparent"
    :text-color="'var(--cloudops-text-secondary)'"
    :active-text-color="'var(--cloudops-accent)'"
    @select="handleSelect"
  >
    <template v-for="item in visibleItems" :key="item.index">
      <el-sub-menu
        v-if="item.children && item.children.length > 0"
        :index="item.index"
      >
        <template #title>
          <el-icon v-if="item.icon && iconMap[item.icon]">
            <component :is="iconMap[item.icon]" />
          </el-icon>
          <span>{{ item.title }}</span>
        </template>
        <el-menu-item
          v-for="child in item.children.filter((c) => !c.admin || auth.isAdmin)"
          :key="child.index"
          :index="child.index"
        >
          <el-icon v-if="child.icon && iconMap[child.icon]">
            <component :is="iconMap[child.icon]" />
          </el-icon>
          <template #title>{{ child.title }}</template>
        </el-menu-item>
      </el-sub-menu>
      <el-menu-item v-else :index="item.index">
        <el-icon v-if="item.icon && iconMap[item.icon]">
          <component :is="iconMap[item.icon]" />
        </el-icon>
        <template #title>{{ item.title }}</template>
      </el-menu-item>
    </template>
  </el-menu>
</template>

<style scoped>
.sidebar-menu {
  border-right: none;
  flex: 1;
  overflow-y: auto;
  padding-top: 8px;
}

.sidebar-menu:not(.el-menu--collapse) {
  width: 220px;
}

:deep(.el-menu-item),
:deep(.el-sub-menu__title) {
  height: 40px;
  line-height: 40px;
  margin: 2px 8px;
  border-radius: 6px;
}

:deep(.el-menu-item.is-active) {
  background: var(--cloudops-bg-active) !important;
  color: var(--cloudops-accent) !important;
}

:deep(.el-menu-item:hover),
:deep(.el-sub-menu__title:hover) {
  background: var(--cloudops-bg-hover) !important;
}

:deep(.el-sub-menu .el-menu-item) {
  padding-left: 52px !important;
  min-width: auto;
}
</style>
