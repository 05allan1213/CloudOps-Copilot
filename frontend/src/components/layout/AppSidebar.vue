<script setup lang="ts">
import { Activity, PanelLeftClose, PanelLeftOpen } from "lucide-vue-next";

import SidebarMenu from "./SidebarMenu.vue";

defineProps<{ collapsed: boolean }>();
defineEmits<{ toggle: [] }>();
</script>

<template>
  <aside class="app-sidebar" :class="{ 'is-collapsed': collapsed }" aria-label="主导航">
    <RouterLink class="sidebar-brand" to="/overview" aria-label="CloudOps 总览">
      <span class="sidebar-brand-icon" aria-hidden="true"><Activity :size="21" /></span>
      <span class="sidebar-brand-copy"><strong>CloudOps</strong><small>本地运维控制台</small></span>
    </RouterLink>
    <SidebarMenu variant="desktop" :collapsed="collapsed" />
    <div class="sidebar-footer">
      <span class="local-boundary"><span aria-hidden="true" />本地 Owner</span>
      <button type="button" class="sidebar-toggle" :aria-label="collapsed ? '展开侧栏' : '收起侧栏'" :title="collapsed ? '展开侧栏' : '收起侧栏'" @click="$emit('toggle')">
        <PanelLeftOpen v-if="collapsed" :size="18" aria-hidden="true" />
        <PanelLeftClose v-else :size="18" aria-hidden="true" />
      </button>
    </div>
  </aside>
</template>

<style scoped>
.app-sidebar { position: sticky; top: 0; z-index: var(--co-z-header); display: flex; flex: 0 0 var(--co-sidebar-width); width: var(--co-sidebar-width); height: 100dvh; min-height: 0; overflow: hidden; flex-direction: column; border-right: 1px solid var(--co-border-default); background: var(--co-bg-surface); transition: width var(--co-motion-standard) var(--co-ease-out), flex-basis var(--co-motion-standard) var(--co-ease-out); }
.app-sidebar.is-collapsed { flex-basis: var(--co-sidebar-rail-width); width: var(--co-sidebar-rail-width); }
.sidebar-brand { display: flex; min-height: var(--co-header-height); align-items: center; gap: var(--co-space-3); padding: 0 var(--co-space-4); border-bottom: 1px solid var(--co-border-default); color: var(--co-text-primary); }
.sidebar-brand:hover { background: var(--co-bg-hover); }
.sidebar-brand-icon { display: grid; width: 32px; height: 32px; flex: 0 0 32px; place-items: center; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-action-primary); background: var(--co-bg-subtle); }
.sidebar-brand-copy { display: grid; min-width: 0; line-height: 1.2; white-space: nowrap; }
.sidebar-brand-copy strong { font-size: 14px; }
.sidebar-brand-copy small { color: var(--co-text-muted); font-size: 10px; }
.sidebar-footer { display: flex; min-height: 52px; align-items: center; justify-content: space-between; gap: var(--co-space-2); margin-top: auto; padding: 0 var(--co-space-3); border-top: 1px solid var(--co-border-default); }
.local-boundary { display: inline-flex; align-items: center; gap: 7px; color: var(--co-text-muted); font-size: 11px; white-space: nowrap; }
.local-boundary > span { width: 7px; height: 7px; border-radius: 50%; background: var(--co-status-success-fg); }
.sidebar-toggle { display: grid; width: 34px; height: 34px; flex: 0 0 34px; place-items: center; border: 1px solid transparent; border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: transparent; cursor: pointer; }
.sidebar-toggle:hover { border-color: var(--co-border-default); color: var(--co-text-primary); background: var(--co-bg-hover); }
.app-sidebar.is-collapsed .sidebar-brand { justify-content: center; padding: 0; }
.app-sidebar.is-collapsed .sidebar-brand-copy, .app-sidebar.is-collapsed .local-boundary { display: none; }
.app-sidebar.is-collapsed .sidebar-footer { justify-content: center; padding: 0; }
@media (min-width: 768px) and (max-width: 1199px) {
  .app-sidebar { flex-basis: var(--co-sidebar-rail-width); width: var(--co-sidebar-rail-width); }
  .sidebar-brand { justify-content: center; padding: 0; }
  .sidebar-brand-copy, .local-boundary, .sidebar-toggle { display: none; }
  .sidebar-footer { min-height: 16px; padding: 0; }
}
@media (max-width: 767px) { .app-sidebar { display: none; } }
</style>
