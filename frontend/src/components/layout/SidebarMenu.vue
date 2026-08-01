<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";

import { navigationGroups } from "../../navigation";

defineProps<{ collapsed: boolean }>();

const route = useRoute();

function isActive(path: string): boolean {
  return route.path === path || route.path.startsWith(`${path}/`);
}

const menuItems = computed(() => navigationGroups.map((group) => [
  {
    type: "label" as const,
    label: group.title,
    value: `${group.id}-label`,
  },
  ...group.items.map((item) => ({
    label: item.title,
    "aria-label": item.title,
    icon: item.icon,
    to: item.index,
    value: item.index,
    active: isActive(item.index),
  })),
]));

const menuUI = {
  root: "w-full min-w-0",
  list: "gap-1",
  label: "px-2 pb-1 pt-3 text-[10px] font-bold uppercase text-[var(--co-text-muted)]",
  link: "min-h-9 gap-3 rounded-[9px] border border-transparent px-3 text-[13px] font-semibold",
  linkLeadingIcon: "size-[17px] shrink-0",
  linkLabel: "truncate",
};
</script>

<template>
  <UNavigationMenu
    class="sidebar-menu"
    orientation="vertical"
    variant="pill"
    color="neutral"
    tooltip
    :collapsed="collapsed"
    :items="menuItems"
    :ui="menuUI"
    aria-label="工作区"
  />
</template>

<style scoped>
.sidebar-menu {
  min-height: 0;
  padding: 4px 10px 10px;
  overflow-y: auto;
  overscroll-behavior: contain;
  scrollbar-width: none;
}
.sidebar-menu::-webkit-scrollbar { display: none; }
.sidebar-menu :deep(a) {
  position: relative;
  color: var(--co-text-secondary);
  transition:
    color var(--co-motion-fast) var(--co-ease-out),
    background var(--co-motion-fast) var(--co-ease-out),
    border-color var(--co-motion-fast) var(--co-ease-out),
    transform var(--co-motion-fast) var(--co-ease-out);
}
.sidebar-menu :deep(a:hover) {
  color: var(--co-text-primary);
  border-color: color-mix(in srgb, var(--co-border-default) 56%, transparent);
  background: color-mix(in srgb, var(--co-text-primary) 6%, transparent);
  transform: translateX(1px);
}
.sidebar-menu :deep(a[data-active="true"]),
.sidebar-menu :deep(a[aria-current="page"]) {
  color: var(--co-text-primary);
  border-color: color-mix(in srgb, var(--co-border-strong) 72%, transparent);
  background: color-mix(in srgb, var(--co-bg-floating) 74%, transparent);
  box-shadow: 0 7px 18px rgb(52 46 39 / 7%), inset 0 1px 0 rgb(255 255 255 / 42%);
}
.sidebar-menu :deep(a[data-active="true"]::before),
.sidebar-menu :deep(a[aria-current="page"]::before) {
  position: absolute;
  top: 9px;
  bottom: 9px;
  left: -1px;
  width: 2px;
  border-radius: 999px;
  background: var(--co-viz-live);
  box-shadow: 0 0 10px var(--co-viz-live-soft);
  content: "";
}
.sidebar-menu :deep(a[data-active="true"] svg),
.sidebar-menu :deep(a[aria-current="page"] svg) { color: var(--co-text-primary); }
</style>
