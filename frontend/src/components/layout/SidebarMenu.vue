<script setup lang="ts">
import { computed, markRaw } from "vue";
import { FirstAidKit } from "@element-plus/icons-vue";
import { useRoute } from "vue-router";

import { primaryNavigation } from "../../navigation";

withDefaults(
  defineProps<{
    variant?: "desktop" | "drawer";
  }>(),
  { variant: "desktop" },
);

defineEmits<{
  navigate: [];
}>();

const route = useRoute();
const iconMap: Record<string, typeof FirstAidKit> = {
  FirstAidKit: markRaw(FirstAidKit),
};
const visibleItems = computed(() => primaryNavigation);

function isActive(path: string): boolean {
  return route.path === path || route.path.startsWith(`${path}/`);
}
</script>

<template>
  <nav
    class="sidebar-menu"
    :class="`sidebar-menu--${variant}`"
    aria-label="Workbench"
  >
    <RouterLink
      v-for="item in visibleItems"
      :key="item.index"
      :to="item.index"
      class="sidebar-menu-item"
      :class="{ 'is-active': isActive(item.index) }"
      :aria-current="isActive(item.index) ? 'page' : undefined"
      @click="$emit('navigate')"
    >
      <el-icon
        v-if="item.icon && iconMap[item.icon]"
        :size="20"
        aria-hidden="true"
      >
        <component :is="iconMap[item.icon]" />
      </el-icon>
      <span class="sidebar-menu-label">{{ item.title }}</span>
    </RouterLink>
  </nav>
</template>

<style scoped>
.sidebar-menu {
  display: grid;
  align-content: start;
  gap: var(--co-space-1);
  min-width: 0;
  padding: var(--co-space-3) var(--co-space-2);
}

.sidebar-menu-item {
  position: relative;
  display: flex;
  min-width: 0;
  min-height: 44px;
  align-items: center;
  gap: var(--co-space-2);
  padding: 0 var(--co-space-2);
  border: 1px solid transparent;
  border-radius: var(--co-radius-control);
  color: var(--co-text-secondary);
  font-size: 12.5px;
  font-weight: 600;
  transition: color var(--co-motion-fast) var(--co-ease-out),
    border-color var(--co-motion-fast) var(--co-ease-out),
    background-color var(--co-motion-fast) var(--co-ease-out);
}

.sidebar-menu-item:hover {
  border-color: var(--co-border-default);
  color: var(--co-text-primary);
  background: var(--co-bg-hover);
}

.sidebar-menu-item.is-active {
  border-color: var(--co-status-info-border);
  color: var(--co-action-primary);
  background: var(--co-bg-active);
}

.sidebar-menu-item.is-active::before {
  position: absolute;
  top: 10px;
  bottom: 10px;
  left: -1px;
  width: 3px;
  border-radius: 0 var(--co-radius-pill) var(--co-radius-pill) 0;
  background: var(--co-action-primary);
  content: "";
}

.sidebar-menu-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (min-width: 768px) and (max-width: 1279px) {
  .sidebar-menu--desktop .sidebar-menu-item {
    justify-content: center;
    padding: 0;
  }

  .sidebar-menu--desktop .sidebar-menu-label {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
  }
}
</style>
