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
  label: "px-2 pb-1 pt-2 text-[11px] font-semibold text-[var(--co-text-muted)]",
  link: "min-h-10 gap-3 rounded-[var(--co-radius-control)] px-3 text-[13px] font-semibold",
  linkLeadingIcon: "size-[19px] shrink-0",
  linkLabel: "truncate",
};
</script>

<template>
  <UNavigationMenu
    class="sidebar-menu"
    orientation="vertical"
    variant="pill"
    color="primary"
    highlight
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
  padding: var(--co-space-2);
  overflow-y: auto;
  overscroll-behavior: contain;
}
</style>
