<script setup lang="ts">
import { DataAnalysis } from "@element-plus/icons-vue";

import SidebarMenu from "./SidebarMenu.vue";
</script>

<template>
  <aside
    class="app-sidebar"
    aria-label="Primary application navigation"
  >
    <RouterLink
      class="sidebar-brand"
      to="/incidents"
      aria-label="CloudOps Incident Agent home"
    >
      <span
        class="sidebar-brand-icon"
        aria-hidden="true"
      >
        <el-icon :size="22">
          <DataAnalysis />
        </el-icon>
      </span>
      <span class="sidebar-brand-copy">
        <strong>CloudOps</strong>
        <small>Incident Agent</small>
      </span>
    </RouterLink>

    <SidebarMenu variant="desktop" />

    <div class="sidebar-footer">
      <span>Evidence-first Workbench</span>
      <small>V3 MySQL projection</small>
    </div>
  </aside>
</template>

<style scoped>
.app-sidebar {
  position: relative;
  z-index: var(--co-z-header);
  display: flex;
  flex: 0 0 var(--co-sidebar-width);
  width: var(--co-sidebar-width);
  min-height: 100dvh;
  overflow: hidden;
  flex-direction: column;
  border-right: 1px solid var(--co-border-default);
  background: var(--co-bg-surface);
  transition: width var(--co-motion-standard) var(--co-ease-out),
    flex-basis var(--co-motion-standard) var(--co-ease-out);
}

.sidebar-brand {
  display: flex;
  min-height: var(--co-header-height);
  align-items: center;
  gap: var(--co-space-3);
  padding: 0 var(--co-space-4);
  border-bottom: 1px solid var(--co-border-default);
  color: var(--co-text-primary);
}

.sidebar-brand:hover {
  background: var(--co-bg-hover);
}

.sidebar-brand-icon {
  display: grid;
  width: 32px;
  height: 32px;
  flex: 0 0 32px;
  place-items: center;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-control);
  color: var(--co-action-primary);
  background: var(--co-bg-subtle);
}

.sidebar-brand-copy {
  display: grid;
  min-width: 0;
  line-height: 1.2;
  white-space: nowrap;
}

.sidebar-brand-copy strong {
  font-size: 14px;
}

.sidebar-brand-copy small,
.sidebar-footer small {
  color: var(--co-text-muted);
  font-size: 11px;
}

.sidebar-footer {
  display: grid;
  gap: 2px;
  margin-top: auto;
  padding: var(--co-space-4);
  border-top: 1px solid var(--co-border-default);
  color: var(--co-text-secondary);
  font-size: 12px;
  white-space: nowrap;
}

@media (min-width: 768px) and (max-width: 1279px) {
  .app-sidebar {
    flex-basis: var(--co-sidebar-rail-width);
    width: var(--co-sidebar-rail-width);
  }

  .sidebar-brand {
    justify-content: center;
    padding: 0;
  }

  .sidebar-brand-copy,
  .sidebar-footer {
    display: none;
  }
}

@media (max-width: 767px) {
  .app-sidebar {
    display: none;
  }
}
</style>
