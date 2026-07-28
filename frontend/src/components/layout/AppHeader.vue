<script setup lang="ts">
import { computed } from "vue";
import { Activity, Bell, Bot, ChevronRight, FlaskConical, LoaderCircle, Moon, Network, Sun, UserRound } from "lucide-vue-next";

import type { OperationalScope, ProviderHealth } from "../../api/platform";
import { useTheme } from "../../composables/useTheme";

const props = defineProps<{
  pageTitle: string;
  unreadCount: number;
  activeScope?: OperationalScope;
  scopes: OperationalScope[];
  selectedScopeId: string;
  scopeSwitching: boolean;
  providerHealth?: ProviderHealth[];
  scenarioState: "inactive" | "active";
}>();
const emit = defineEmits<{
  changeScope: [scopeID: string];
  openAgent: [];
  openNotifications: [trigger: HTMLElement];
}>();

const { isDark, toggleTheme } = useTheme();
const environmentLabel = import.meta.env.VITE_ENVIRONMENT_LABEL || "Demo / kind";
const themeActionLabel = computed(() => (isDark.value ? "切换浅色主题" : "切换深色主题"));
const notificationCountLabel = computed(() => (props.unreadCount > 99 ? "99+" : String(props.unreadCount)));
const notificationActionLabel = computed(() => props.unreadCount > 0
  ? `打开通知收件箱，${notificationCountLabel.value} 条未读`
  : "打开通知收件箱");
const scopeDetail = computed(() => props.activeScope
  ? `${props.activeScope.environment} · ${props.activeScope.namespaces.join(", ")}`
  : "Operational Scope 暂不可用");
const selectableScopes = computed(() => props.scopes.filter((scope): scope is OperationalScope & { id: string } => Boolean(scope.id)));
const availableProviders = computed(() => props.providerHealth?.filter((item) => item.state === "available").length ?? 0);
const providerCount = computed(() => props.providerHealth?.length ?? 0);
const scenarioLabel = computed(() => props.scenarioState === "active" ? "Scenario Active" : "Live Mode");

function openNotifications(event: MouseEvent) {
  emit("openNotifications", event.currentTarget as HTMLElement);
}

function changeScope(event: Event) {
  const scopeID = (event.currentTarget as HTMLSelectElement).value;
  if (scopeID && scopeID !== props.activeScope?.id) emit("changeScope", scopeID);
}
</script>

<template>
  <header class="app-header">
    <div class="header-leading">
      <RouterLink class="product-mark" to="/overview" aria-label="CloudOps 总览">
        <span class="product-icon" aria-hidden="true"><Activity :size="19" /></span>
        <strong>CloudOps</strong>
      </RouterLink>
      <nav class="breadcrumb" aria-label="面包屑">
        <RouterLink to="/overview">控制台</RouterLink>
        <ChevronRight :size="14" aria-hidden="true" />
        <span aria-current="page">{{ pageTitle }}</span>
      </nav>
    </div>
    <div class="header-actions">
      <span class="scenario-boundary" :class="{ 'is-active': scenarioState === 'active' }" role="status" :aria-label="`运行模式：${scenarioLabel}`">
        <FlaskConical :size="14" aria-hidden="true" />
        <span>{{ scenarioLabel }}</span>
      </span>
      <label class="environment-boundary" :title="scopeDetail">
        <Network :size="15" aria-hidden="true" />
        <span class="visually-hidden">活动集群</span>
        <select
          aria-label="活动集群"
          autocomplete="off"
          name="active_cluster"
          :disabled="scopeSwitching || selectableScopes.length < 2"
          :value="selectedScopeId || activeScope?.id || ''"
          @change="changeScope"
        >
          <option v-if="!selectableScopes.length" value="">{{ activeScope?.cluster_id || environmentLabel }}</option>
          <option v-for="scope in selectableScopes" :key="scope.id" :value="scope.id">
            {{ scope.cluster_id }} · {{ scope.environment }}
          </option>
        </select>
        <LoaderCircle v-if="scopeSwitching" class="scope-spinner" :size="14" aria-hidden="true" />
        <small v-if="providerCount">{{ availableProviders }}/{{ providerCount }}</small>
      </label>
      <button type="button" class="icon-button" aria-label="打开全局 Agent 面板" title="Agent" @click="emit('openAgent')">
        <Bot :size="19" aria-hidden="true" />
      </button>
      <button type="button" class="icon-button notification-button" :aria-label="notificationActionLabel" title="通知" @click="openNotifications">
        <Bell :size="19" aria-hidden="true" />
        <span v-if="unreadCount" class="notification-count">{{ notificationCountLabel }}</span>
      </button>
      <button type="button" class="icon-button" :aria-label="themeActionLabel" :title="themeActionLabel" @click="toggleTheme">
        <Sun v-if="isDark" :size="19" aria-hidden="true" />
        <Moon v-else :size="19" aria-hidden="true" />
      </button>
      <span class="user-trigger" aria-label="本地 Owner 上下文"><span class="user-avatar" aria-hidden="true"><UserRound :size="17" /></span><span class="user-copy"><strong>Owner</strong><small>local</small></span></span>
    </div>
  </header>
</template>

<style scoped>
.app-header { position: sticky; top: 0; z-index: var(--co-z-header); display: flex; min-height: var(--co-header-height); align-items: center; justify-content: space-between; gap: var(--co-space-3); padding: 0 max(var(--co-space-4), env(safe-area-inset-right)) 0 var(--co-space-5); border-bottom: 1px solid var(--co-border-default); background: color-mix(in srgb, var(--co-bg-surface) 94%, transparent); backdrop-filter: blur(12px); }
.header-leading, .header-actions, .product-mark, .breadcrumb, .environment-boundary, .scenario-boundary, .user-trigger { display: flex; align-items: center; }
.header-leading { min-width: 0; gap: var(--co-space-4); }
.header-actions { flex: 0 0 auto; gap: var(--co-space-2); }
.product-mark { min-height: 44px; gap: var(--co-space-2); color: var(--co-text-primary); }
.product-icon, .user-avatar { display: grid; flex: 0 0 auto; place-items: center; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-action-primary); background: var(--co-bg-subtle); }
.product-icon { width: 31px; height: 31px; }
.product-mark strong { font-size: 14px; }
.breadcrumb { min-width: 0; gap: var(--co-space-2); padding-left: var(--co-space-4); border-left: 1px solid var(--co-border-default); color: var(--co-text-muted); font-size: 13px; white-space: nowrap; }
.breadcrumb a { color: var(--co-action-primary); }
.breadcrumb span { overflow: hidden; text-overflow: ellipsis; }
.environment-boundary { min-height: 34px; max-width: 280px; gap: var(--co-space-2); padding: 0 var(--co-space-2); border: 1px solid var(--co-status-info-border); border-radius: var(--co-radius-pill); color: var(--co-status-info-fg); background: var(--co-status-info-bg); font-size: 11px; font-weight: 700; }
.scenario-boundary { min-height: 34px; gap: var(--co-space-1); padding: 0 var(--co-space-2); border: 1px solid var(--co-status-success-border); border-radius: var(--co-radius-pill); color: var(--co-status-success-fg); background: var(--co-status-success-bg); font-size: 10px; font-weight: 800; white-space: nowrap; }
.scenario-boundary.is-active { border-color: var(--co-status-warning-border); color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }
.environment-boundary:hover, .environment-boundary:focus-within { border-color: var(--co-action-primary); background: var(--co-bg-active); }
.environment-boundary select { min-width: 0; max-width: 170px; border: 0; color: var(--co-status-info-fg); background-color: var(--co-status-info-bg); cursor: pointer; font-size: 11px; font-weight: 750; text-overflow: ellipsis; }
.environment-boundary option { color: var(--co-text-primary); background-color: var(--co-bg-surface); }
.environment-boundary select:disabled { cursor: default; opacity: 1; }
.environment-boundary small { padding-left: var(--co-space-2); border-left: 1px solid var(--co-status-info-border); font-family: var(--co-font-mono); font-size: 10px; }
.scope-spinner { flex: 0 0 auto; animation: scope-spin 0.8s linear infinite; }
.icon-button { position: relative; display: grid; width: 38px; height: 38px; flex: 0 0 38px; place-items: center; padding: 0; border: 1px solid transparent; border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: transparent; cursor: pointer; }
.icon-button:hover { border-color: var(--co-border-default); color: var(--co-text-primary); background: var(--co-bg-hover); }
.notification-count { position: absolute; top: 2px; right: 1px; min-width: 17px; height: 17px; padding: 0 4px; border: 2px solid var(--co-bg-surface); border-radius: var(--co-radius-pill); color: #fff; background: var(--co-status-critical-fg); font-size: 9px; font-weight: 800; line-height: 13px; }
.user-trigger { gap: var(--co-space-2); padding-left: var(--co-space-2); }
.user-avatar { width: 31px; height: 31px; }
.user-copy { display: grid; line-height: 1.1; }
.user-copy strong { font-size: 12px; }.user-copy small { color: var(--co-text-muted); font-size: 10px; }
@keyframes scope-spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) { .scope-spinner { animation: none; } }
@media (max-width: 1024px) { .scenario-boundary span { display: none; } }
@media (max-width: 900px) { .user-copy, .product-mark strong { display: none; } .breadcrumb { padding-left: 0; border-left: 0; } .environment-boundary small { display: none; } }
@media (max-width: 767px) { .app-header { padding-inline: max(var(--co-space-3), env(safe-area-inset-left)) max(var(--co-space-3), env(safe-area-inset-right)); } .product-mark, .user-trigger { display: none; } .header-leading { overflow: hidden; } .breadcrumb a, .breadcrumb svg { display: none; } .breadcrumb span { color: var(--co-text-primary); font-weight: 750; } .environment-boundary { max-width: 118px; padding-inline: var(--co-space-2); } .environment-boundary select { width: 84px; font-size: 16px; } }
@media (max-width: 359px) { .header-actions { gap: var(--co-space-1); } .environment-boundary { max-width: 108px; } .environment-boundary select { width: 74px; } }
</style>
