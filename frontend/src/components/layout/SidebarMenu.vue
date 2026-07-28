<script setup lang="ts">
import { markRaw, ref, type Component } from "vue";
import {
  Bot,
  ChevronDown,
  Gauge,
  GitPullRequest,
  LayoutDashboard,
  Logs,
  ScanSearch,
  Server,
  Settings,
  Siren,
  Stethoscope,
} from "lucide-vue-next";
import { useRoute } from "vue-router";

import { navigationGroups, type NavigationIcon } from "../../navigation";

withDefaults(
  defineProps<{
    variant?: "desktop" | "sheet";
    collapsed?: boolean;
  }>(),
  { variant: "desktop", collapsed: false },
);

defineEmits<{ navigate: [] }>();

const route = useRoute();
const collapsedGroups = ref(new Set<string>());
const iconMap: Record<NavigationIcon, Component> = {
  LayoutDashboard: markRaw(LayoutDashboard),
  Server: markRaw(Server),
  Gauge: markRaw(Gauge),
  Siren: markRaw(Siren),
  Logs: markRaw(Logs),
  ScanSearch: markRaw(ScanSearch),
  Bot: markRaw(Bot),
  FirstAid: markRaw(Stethoscope),
  GitPullRequest: markRaw(GitPullRequest),
  Settings: markRaw(Settings),
};

function isActive(path: string): boolean {
  return route.path === path || route.path.startsWith(`${path}/`);
}

function groupCollapsed(group: string): boolean {
  return collapsedGroups.value.has(group);
}

function toggleGroup(group: string) {
  const next = new Set(collapsedGroups.value);
  if (next.has(group)) next.delete(group);
  else next.add(group);
  collapsedGroups.value = next;
}
</script>

<template>
  <nav class="sidebar-menu" :class="[`sidebar-menu--${variant}`, { 'is-collapsed': collapsed }]" aria-label="工作区">
    <section v-for="group in navigationGroups" :key="group.id" class="navigation-group">
      <button
        v-if="!collapsed || variant === 'sheet'"
        type="button"
        class="navigation-group-toggle"
        :aria-expanded="!groupCollapsed(group.id)"
        @click="toggleGroup(group.id)"
      >
        <span>{{ group.title }}</span>
        <ChevronDown :size="15" aria-hidden="true" :class="{ 'is-closed': groupCollapsed(group.id) }" />
      </button>
      <div v-show="collapsed || !groupCollapsed(group.id)" class="navigation-items">
        <RouterLink
          v-for="item in group.items"
          :key="item.index"
          :to="item.index"
          class="sidebar-menu-item"
          :class="{ 'is-active': isActive(item.index) }"
          :aria-current="isActive(item.index) ? 'page' : undefined"
          :aria-label="item.title"
          :title="item.title"
          @click="$emit('navigate')"
        >
          <component :is="iconMap[item.icon]" :size="19" :stroke-width="1.8" aria-hidden="true" />
          <span class="sidebar-menu-label">{{ item.title }}</span>
        </RouterLink>
      </div>
    </section>
  </nav>
</template>

<style scoped>
.sidebar-menu { display: grid; align-content: start; gap: var(--co-space-2); min-width: 0; padding: var(--co-space-3) var(--co-space-2); overflow-y: auto; overscroll-behavior: contain; }
.navigation-group { min-width: 0; }
.navigation-group-toggle { display: flex; width: 100%; min-height: 30px; align-items: center; justify-content: space-between; padding: 0 var(--co-space-2); border: 0; color: var(--co-text-muted); background: transparent; cursor: pointer; font-size: 11px; font-weight: 750; letter-spacing: 0; }
.navigation-group-toggle:hover { color: var(--co-text-primary); }
.navigation-group-toggle svg { transition: transform var(--co-motion-fast) var(--co-ease-out); }
.navigation-group-toggle svg.is-closed { transform: rotate(-90deg); }
.navigation-items { display: grid; gap: var(--co-space-1); }
.sidebar-menu-item { position: relative; display: flex; min-width: 0; min-height: 40px; align-items: center; gap: var(--co-space-3); padding: 0 var(--co-space-3); border: 1px solid transparent; border-radius: var(--co-radius-control); color: var(--co-text-secondary); font-size: 13px; font-weight: 650; }
.sidebar-menu-item:hover { border-color: var(--co-border-default); color: var(--co-text-primary); background: var(--co-bg-hover); }
.sidebar-menu-item.is-active { border-color: var(--co-status-info-border); color: var(--co-action-primary); background: var(--co-bg-active); }
.sidebar-menu-item.is-active::before { position: absolute; top: 9px; bottom: 9px; left: -1px; width: 3px; border-radius: 0 2px 2px 0; background: var(--co-action-primary); content: ""; }
.sidebar-menu-item svg { flex: 0 0 auto; }
.sidebar-menu-label { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sidebar-menu.is-collapsed { padding-inline: 8px; }
.sidebar-menu.is-collapsed .navigation-group { display: contents; }
.sidebar-menu.is-collapsed .navigation-items { display: contents; }
.sidebar-menu.is-collapsed .sidebar-menu-item { justify-content: center; min-height: 44px; padding: 0; }
.sidebar-menu.is-collapsed .sidebar-menu-label { display: none; }
.sidebar-menu--sheet { gap: var(--co-space-3); padding: 0; }
.sidebar-menu--sheet .navigation-items { grid-template-columns: repeat(2, minmax(0, 1fr)); }
@media (min-width: 768px) and (max-width: 1199px) {
  .sidebar-menu--desktop { padding-inline: 8px; }
  .sidebar-menu--desktop .navigation-group { display: contents; }
  .sidebar-menu--desktop .navigation-group-toggle, .sidebar-menu--desktop .sidebar-menu-label { display: none; }
  .sidebar-menu--desktop .navigation-items { display: contents; }
  .sidebar-menu--desktop .sidebar-menu-item { justify-content: center; min-height: 44px; padding: 0; }
}
</style>
