<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute } from "vue-router";

import { useAuthStore } from "../../stores/auth";
import SidebarMenu from "./SidebarMenu.vue";

const auth = useAuthStore();
const route = useRoute();
const collapsed = ref(false);
const isMobile = ref(false);
const overlayVisible = ref(false);

function checkScreenSize() {
  const width = window.innerWidth;
  isMobile.value = width < 768;
  if (width < 768) {
    collapsed.value = true;
    overlayVisible.value = false;
  } else if (width < 1024) {
    collapsed.value = true;
    overlayVisible.value = false;
  } else {
    collapsed.value = false;
    overlayVisible.value = false;
  }
}

function toggleCollapse() {
  if (isMobile.value) {
    overlayVisible.value = !overlayVisible.value;
    collapsed.value = !overlayVisible.value;
  } else {
    collapsed.value = !collapsed.value;
  }
}

function closeMobileSidebar() {
  if (isMobile.value) {
    overlayVisible.value = false;
    collapsed.value = true;
  }
}

watch(() => route.path, () => {
  closeMobileSidebar();
});

onMounted(() => {
  checkScreenSize();
  window.addEventListener("resize", checkScreenSize);
});

onBeforeUnmount(() => {
  window.removeEventListener("resize", checkScreenSize);
});
</script>

<template>
  <el-aside
    :width="collapsed ? '64px' : '220px'"
    class="app-sidebar"
    :class="{ 'sidebar-mobile-open': overlayVisible }"
  >
    <div
      v-if="overlayVisible"
      class="sidebar-overlay"
      @click="closeMobileSidebar"
    />
    <div class="sidebar-content">
      <div class="sidebar-header">
        <div v-if="!collapsed" class="sidebar-brand">
          <div class="sidebar-logo"></div>
          <span class="sidebar-title">CloudOps</span>
        </div>
        <div v-else class="sidebar-logo-collapsed"></div>
        <button
          class="sidebar-collapse-btn"
          :aria-label="collapsed ? '展开侧边栏' : '折叠侧边栏'"
          :title="collapsed ? '展开侧边栏' : '折叠侧边栏'"
          @click="toggleCollapse"
        >
          <el-icon :size="16">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <polyline v-if="collapsed" points="9 18 15 12 9 6" />
              <polyline v-else points="15 18 9 12 15 6" />
            </svg>
          </el-icon>
        </button>
      </div>
      <SidebarMenu :collapsed="collapsed" />
    </div>
  </el-aside>
</template>

<style scoped>
.app-sidebar {
  background: var(--cloudops-sidebar-bg);
  border-right: 1px solid var(--cloudops-border-color);
  transition: width 0.25s ease;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  position: relative;
}

.sidebar-content {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

.sidebar-overlay {
  display: none;
}

.sidebar-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: var(--cloudops-header-height);
  padding: 0 16px;
  border-bottom: 1px solid var(--cloudops-border-color);
  flex-shrink: 0;
}

.sidebar-brand {
  display: flex;
  align-items: center;
  gap: 10px;
  overflow: hidden;
}

.sidebar-logo {
  width: 28px;
  height: 28px;
  border-radius: 6px;
  background: linear-gradient(135deg, var(--cloudops-accent), #6366f1);
  flex-shrink: 0;
}

.sidebar-title {
  font-size: 16px;
  font-weight: 700;
  color: var(--cloudops-text-primary);
  white-space: nowrap;
  letter-spacing: -0.02em;
}

.sidebar-logo-collapsed {
  width: 28px;
  height: 28px;
  border-radius: 6px;
  background: linear-gradient(135deg, var(--cloudops-accent), #6366f1);
  flex-shrink: 0;
}

.sidebar-collapse-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 4px;
  color: var(--cloudops-text-muted);
  cursor: pointer;
  flex-shrink: 0;
  transition: color 0.15s, background 0.15s;
  border: none;
  background: none;
}

.sidebar-collapse-btn:hover {
  color: var(--cloudops-text-primary);
  background: var(--cloudops-bg-hover);
}

@media (max-width: 767px) {
  .app-sidebar {
    position: fixed;
    top: 0;
    left: 0;
    bottom: 0;
    z-index: 2000;
  }

  .app-sidebar.sidebar-mobile-open {
    width: 220px !important;
  }

  .sidebar-overlay {
    display: block;
    position: fixed;
    top: 0;
    left: 220px;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.5);
    z-index: 1999;
  }
}
</style>
