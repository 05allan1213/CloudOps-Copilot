<script setup lang="ts">
import SidebarMenu from "./SidebarMenu.vue";

const props = defineProps<{
  collapsed: boolean;
  collapseLocked: boolean;
}>();

const emit = defineEmits<{ toggle: [] }>();

const collapseLabel = () => props.collapseLocked
  ? "当前宽度使用紧凑侧栏"
  : props.collapsed ? "展开侧栏" : "收起侧栏";
</script>

<template>
  <aside
    class="app-sidebar"
    :class="{ 'is-collapsed': collapsed }"
    aria-label="主导航"
  >
    <UButton
      class="sidebar-brand"
      color="neutral"
      variant="ghost"
      to="/overview"
      aria-label="CloudOps Operations Copilot"
    >
      <span class="brand-mark" aria-hidden="true">
        <UIcon name="i-lucide-orbit" />
        <i />
      </span>
      <span
        v-if="!collapsed"
        class="sidebar-brand-copy"
      >
        <strong>CloudOps</strong>
        <small>Operations Copilot</small>
      </span>
    </UButton>

    <SidebarMenu
      class="sidebar-navigation"
      :collapsed="collapsed"
    />

    <footer
      v-if="!collapsed"
      class="sidebar-footer"
    >
      <span class="runtime-state" aria-label="本地运行时已连接">
        <i aria-hidden="true" />
        <span>
          <strong>Local runtime</strong>
          <small>Owner boundary</small>
        </span>
      </span>
    </footer>

    <UTooltip
      v-if="!collapseLocked"
      :text="collapseLabel()"
      :content="{ side: 'right' }"
    >
      <UButton
        class="sidebar-toggle"
        color="neutral"
        variant="ghost"
        :icon="collapsed ? 'i-lucide-chevron-right' : 'i-lucide-chevron-left'"
        square
        :disabled="collapseLocked"
        :aria-label="collapseLabel()"
        @click="emit('toggle')"
      />
    </UTooltip>
  </aside>
</template>

<style scoped>
.app-sidebar {
  position: relative;
  z-index: calc(var(--co-z-header) + 1);
  display: flex;
  width: var(--co-sidebar-width);
  height: 100dvh;
  min-height: 0;
  flex: 0 0 var(--co-sidebar-width);
  overflow: visible;
  flex-direction: column;
  border-right: 1px solid color-mix(in srgb, var(--co-border-strong) 62%, transparent);
  background:
    linear-gradient(180deg, color-mix(in srgb, var(--co-bg-floating) 48%, var(--co-bg-surface)) 0, var(--co-bg-surface) 38%, color-mix(in srgb, var(--co-bg-surface) 90%, var(--co-bg-canvas)) 100%);
  box-shadow: inset -1px 0 0 color-mix(in srgb, var(--co-bg-floating) 46%, transparent);
  transition:
    width 300ms var(--co-ease-out),
    flex-basis 300ms var(--co-ease-out);
}

.app-sidebar.is-collapsed {
  width: var(--co-sidebar-rail-width);
  flex-basis: var(--co-sidebar-rail-width);
}

.sidebar-brand {
  min-height: 62px;
  flex: 0 0 62px;
  justify-content: flex-start;
  gap: 10px;
  margin: 10px 10px 4px;
  padding: 8px 6px;
  border-radius: 10px;
  transition: background var(--co-motion-fast) var(--co-ease-out);
}
.sidebar-brand:hover { background: color-mix(in srgb, var(--co-bg-floating) 48%, transparent); }

.is-collapsed .sidebar-brand {
  justify-content: center;
  margin-inline: 6px;
  padding-inline: 0;
}

.brand-mark {
  position: relative;
  display: grid;
  width: 32px;
  height: 32px;
  flex: 0 0 32px;
  place-items: center;
  border: 1px solid color-mix(in srgb, var(--co-border-strong) 70%, transparent);
  border-radius: 9px;
  color: var(--co-text-primary);
  background:
    linear-gradient(145deg, color-mix(in srgb, var(--co-bg-overlay) 78%, transparent), var(--co-bg-subtle));
  box-shadow: inset 0 1px 0 rgb(255 255 255 / 45%);
}
.brand-mark :deep(svg) { width: 18px; height: 18px; }
.brand-mark i {
  position: absolute;
  right: 4px;
  bottom: 4px;
  width: 5px;
  height: 5px;
  border: 1px solid var(--co-bg-surface);
  border-radius: 50%;
  background: var(--co-viz-live);
}

.sidebar-brand-copy {
  display: grid;
  min-width: 0;
  text-align: left;
  line-height: 1.15;
  white-space: nowrap;
}
.sidebar-brand-copy strong { color: var(--co-text-primary); font-size: 15px; font-weight: 800; }
.sidebar-brand-copy small { margin-top: 2px; color: var(--co-text-muted); font-size: 9px; font-weight: 600; }

.sidebar-navigation {
  min-height: 0;
  flex: 1 1 auto;
}

.sidebar-footer {
  min-height: 64px;
  flex: 0 0 64px;
  margin-top: auto;
  padding: 10px;
  border-top: 1px solid color-mix(in srgb, var(--co-border-default) 78%, transparent);
  background: color-mix(in srgb, var(--co-bg-surface) 82%, transparent);
}
.runtime-state {
  display: flex;
  min-height: 42px;
  align-items: center;
  gap: 10px;
  padding: 7px 9px;
  border: 1px solid transparent;
  border-radius: 9px;
  color: var(--co-text-secondary);
}
.runtime-state > i {
  width: 7px;
  height: 7px;
  flex: 0 0 7px;
  border-radius: 50%;
  background: var(--co-viz-live);
  box-shadow: 0 0 0 4px var(--co-viz-live-soft);
}
.runtime-state > span { display: grid; min-width: 0; line-height: 1.2; }
.runtime-state strong { font-size: 11px; }
.runtime-state small { color: var(--co-text-muted); font-size: 9px; }
.sidebar-toggle {
  position: absolute;
  top: 50%;
  right: -12px;
  z-index: 4;
  width: 24px;
  min-width: 24px;
  height: 52px;
  padding: 0;
  border: 1px solid color-mix(in srgb, var(--co-border-default) 82%, transparent);
  border-radius: 14px;
  color: var(--co-text-secondary);
  background: var(--co-bg-surface);
  box-shadow: 4px 0 14px rgb(45 40 34 / 16%);
  transform: translateY(-50%);
  transition:
    color var(--co-motion-fast) var(--co-ease-out),
    background var(--co-motion-fast) var(--co-ease-out),
    transform var(--co-motion-fast) var(--co-ease-out);
}
.sidebar-toggle:hover { color: var(--co-text-primary); background: var(--co-bg-overlay); transform: translate(2px, -50%); }
.sidebar-toggle:disabled { opacity: .38; }
</style>
