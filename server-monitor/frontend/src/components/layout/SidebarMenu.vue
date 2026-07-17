<script setup lang="ts">
import { computed, markRaw } from "vue";
import { useRoute, useRouter } from "vue-router";

import {
  AlarmClock,
  Bell,
  ChatDotRound,
  CircleCheck,
  Clock,
  Document,
  FirstAidKit,
  Grid,
  Message,
  Monitor,
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
  { index: "/incidents", title: "Incident Workbench", icon: "FirstAidKit", group: "incident" },
  {
    index: "compatibility-group",
    title: "Legacy / Compatibility",
    icon: "Grid",
    group: "legacy",
    children: [
      { index: "/overview", title: "旧版总览", icon: "Monitor", group: "legacy" },
      { index: "/hosts", title: "主机", icon: "Monitor", group: "legacy" },
      { index: "/status", title: "状态", icon: "CircleCheck", group: "legacy" },
      { index: "/alerts", title: "当前告警（Legacy）", icon: "Bell", group: "alert" },
      { index: "/alert-histories", title: "历史告警", icon: "Clock", group: "alert" },
      { index: "/copilot", title: "Copilot（Legacy）", icon: "ChatDotRound", group: "ai" },
      { index: "/diagnosis", title: "诊断（Legacy）", icon: "FirstAidKit", group: "ai" },
      { index: "/actions", title: "动作历史（Deprecated）", icon: "Operation", group: "admin", admin: true },
      { index: "/audit-logs", title: "旧版审计日志", icon: "Document", group: "admin", admin: true },
      { index: "/k8s", title: "Kubernetes Dashboard", icon: "Grid", group: "k8s" },
    ],
  },
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
  if (path.startsWith("/incidents/")) return "/incidents";
  if (path.startsWith("/k8s/nodes/")) return "/k8s/nodes";
  if (path.startsWith("/diagnosis/")) return "/diagnosis";
  if (path.startsWith("/actions/")) return "/actions";
  if (path.startsWith("/settings/")) return path;

  return path;
});

const defaultOpeneds = computed(() => {
  const groups: string[] = [];
  const path = route.path;
  if (["/overview", "/hosts", "/status", "/alerts", "/alert-histories", "/copilot", "/diagnosis", "/actions", "/audit-logs", "/k8s"].some((prefix) => path.startsWith(prefix))) {
    groups.push("compatibility-group");
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
    <template
      v-for="item in visibleItems"
      :key="item.index"
    >
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
          <template #title>
            {{ child.title }}
          </template>
        </el-menu-item>
      </el-sub-menu>
      <el-menu-item
        v-else
        :index="item.index"
      >
        <el-icon v-if="item.icon && iconMap[item.icon]">
          <component :is="iconMap[item.icon]" />
        </el-icon>
        <template #title>
          {{ item.title }}
        </template>
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
