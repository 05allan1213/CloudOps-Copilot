<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";

import type { OperationalScope } from "../../api/platform";
import SidebarMenu from "./SidebarMenu.vue";

const props = defineProps<{
  collapsed: boolean;
  collapseLocked: boolean;
  activeScope?: OperationalScope;
  scopes: OperationalScope[];
  selectedScopeId: string;
  scopeSwitching: boolean;
}>();

const emit = defineEmits<{
  toggle: [];
  changeScope: [scopeID: string];
  openAgent: [];
}>();

const route = useRoute();
const selectableScopes = computed(() => props.scopes
  .filter((scope): scope is OperationalScope & { id: string } => Boolean(scope.id))
  .map((scope) => ({
    label: scope.cluster_id,
    description: `${scope.environment} · ${scope.namespaces.join(", ")}`,
    value: scope.id,
  })));
const currentScopeLabel = computed(() => props.activeScope
  ? `${props.activeScope.cluster_id} · ${props.activeScope.environment}`
  : "运行范围暂不可用");
const selectedScope = computed(() => props.selectedScopeId || props.activeScope?.id || "");
const collapseLabel = computed(() => props.collapseLocked
  ? "当前宽度使用紧凑侧栏"
  : props.collapsed ? "展开侧栏" : "收起侧栏");

function changeScope(value: unknown) {
  if (typeof value === "string" && value && value !== props.activeScope?.id) emit("changeScope", value);
}
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
      icon="i-lucide-activity"
      aria-label="CloudOps 本地运维控制台"
    >
      <span
        v-if="!collapsed"
        class="sidebar-brand-copy"
      >
        <strong>CloudOps</strong>
        <small>本地运维控制台</small>
      </span>
    </UButton>

    <section
      class="scope-row"
      aria-labelledby="scope-row-label"
    >
      <div
        v-if="!collapsed"
        class="scope-heading"
      >
        <span id="scope-row-label">运行范围</span>
        <span
          v-if="scopeSwitching"
          role="status"
        >正在切换</span>
      </div>
      <USelect
        class="scope-select"
        :class="{ 'scope-select--rail': collapsed }"
        :model-value="selectedScope"
        :items="selectableScopes"
        value-key="value"
        label-key="label"
        icon="i-lucide-network"
        :loading="scopeSwitching"
        :disabled="scopeSwitching || selectableScopes.length < 2"
        :placeholder="activeScope?.cluster_id || '运行范围'"
        :aria-label="`活动运行范围：${currentScopeLabel}`"
        :title="currentScopeLabel"
        :ui="collapsed ? { base: 'w-10 justify-center px-0', value: 'sr-only', trailing: 'hidden' } : { base: 'w-full' }"
        @update:model-value="changeScope"
      />
      <p
        v-if="!collapsed"
        class="scope-detail"
        :title="activeScope?.namespaces.join(', ')"
      >
        {{ activeScope ? `${activeScope.environment} · ${activeScope.namespaces.join(", ")}` : "等待 Bootstrap 上下文" }}
      </p>
    </section>

    <SidebarMenu
      class="sidebar-navigation"
      :collapsed="collapsed"
    />

    <div
      class="agent-pin"
      :class="{ 'is-current': route.path === '/agent' }"
    >
      <UTooltip
        :text="collapsed ? '打开全局 Agent 面板' : undefined"
        :disabled="!collapsed"
        :content="{ side: 'right' }"
      >
        <UButton
          class="agent-pin-button"
          color="primary"
          :variant="route.path === '/agent' ? 'soft' : 'ghost'"
          icon="i-lucide-bot"
          :label="collapsed ? undefined : 'Agent'"
          :square="collapsed"
          :block="!collapsed"
          aria-label="打开全局 Agent 面板"
          @click="emit('openAgent')"
        />
      </UTooltip>
    </div>

    <footer class="sidebar-footer">
      <UTooltip
        :text="collapsed ? '本地 Owner' : undefined"
        :disabled="!collapsed"
        :content="{ side: 'right' }"
      >
        <UBadge
          class="owner-boundary"
          color="neutral"
          variant="soft"
          icon="i-lucide-user-round"
          :label="collapsed ? undefined : '本地 Owner'"
          :square="collapsed"
        />
      </UTooltip>
      <UTooltip
        :text="collapseLabel"
        :content="{ side: 'right' }"
      >
        <UButton
          class="sidebar-toggle"
          color="neutral"
          variant="ghost"
          :icon="collapsed ? 'i-lucide-panel-left-open' : 'i-lucide-panel-left-close'"
          square
          :disabled="collapseLocked"
          :aria-label="collapseLabel"
          @click="emit('toggle')"
        />
      </UTooltip>
    </footer>
  </aside>
</template>

<style scoped>
.app-sidebar {
  position: sticky;
  top: 0;
  z-index: var(--co-z-header);
  display: flex;
  width: var(--co-sidebar-width);
  height: 100dvh;
  min-height: 0;
  flex: 0 0 var(--co-sidebar-width);
  overflow: hidden;
  flex-direction: column;
  border-right: 1px solid var(--co-border-default);
  background: var(--co-bg-surface);
  transition: width var(--co-motion-standard) var(--co-ease-out),
    flex-basis var(--co-motion-standard) var(--co-ease-out);
}

.app-sidebar.is-collapsed {
  width: var(--co-sidebar-rail-width);
  flex-basis: var(--co-sidebar-rail-width);
}

.sidebar-brand {
  min-height: var(--co-header-height);
  flex: 0 0 var(--co-header-height);
  justify-content: flex-start;
  gap: var(--co-space-3);
  padding-inline: var(--co-space-4);
  border-bottom: 1px solid var(--co-border-default);
  border-radius: 0;
}

.is-collapsed .sidebar-brand {
  justify-content: center;
  padding-inline: 0;
}

.sidebar-brand-copy {
  display: grid;
  min-width: 0;
  text-align: left;
  line-height: 1.2;
  white-space: nowrap;
}

.sidebar-brand-copy strong { font-size: 14px; }
.sidebar-brand-copy small { color: var(--co-text-muted); font-size: 10px; }

.scope-row {
  display: grid;
  gap: var(--co-space-2);
  padding: var(--co-space-3);
  border-bottom: 1px solid var(--co-border-default);
}

.is-collapsed .scope-row {
  justify-items: center;
  padding-inline: var(--co-space-3);
}

.scope-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  color: var(--co-text-muted);
  font-size: 11px;
  font-weight: 700;
}

.scope-heading [role="status"] { color: var(--co-status-info-fg); font-size: 10px; }
.scope-select { min-width: 0; }
.scope-select--rail { width: 40px; }
.scope-detail {
  margin: 0;
  overflow: hidden;
  color: var(--co-text-muted);
  font-size: 10px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sidebar-navigation { flex: 1 1 auto; }

.agent-pin {
  padding: var(--co-space-2);
  border-top: 1px solid var(--co-border-default);
}

.agent-pin-button { justify-content: flex-start; }
.is-collapsed .agent-pin-button { margin-inline: auto; }

.sidebar-footer {
  display: flex;
  min-height: 52px;
  flex: 0 0 52px;
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-2);
  padding-inline: var(--co-space-3);
  border-top: 1px solid var(--co-border-default);
}

.is-collapsed .sidebar-footer {
  min-height: 88px;
  flex-basis: 88px;
  flex-direction: column;
  justify-content: center;
  gap: var(--co-space-1);
  padding: var(--co-space-2);
}

.owner-boundary { white-space: nowrap; }
</style>
