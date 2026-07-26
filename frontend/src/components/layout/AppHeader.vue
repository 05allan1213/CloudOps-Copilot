<script setup lang="ts">
import { computed } from "vue";
import { Activity, Bell, ChevronRight, Moon, Network, Sun, UserRound } from "lucide-vue-next";

import type { OperationalScope, ProviderHealth } from "../../api/platform";
import { useTheme } from "../../composables/useTheme";

const props = defineProps<{
  pageTitle: string;
  unreadCount: number;
  activeScope?: OperationalScope;
  providerHealth?: ProviderHealth[];
}>();
const emit = defineEmits<{ openNotifications: [trigger: HTMLElement] }>();

const { isDark, toggleTheme } = useTheme();
const environmentLabel = import.meta.env.VITE_ENVIRONMENT_LABEL || "Demo / kind";
const themeActionLabel = computed(() => (isDark.value ? "切换浅色主题" : "切换深色主题"));
const scopeLabel = computed(() => props.activeScope?.cluster_id || environmentLabel);
const scopeDetail = computed(() => props.activeScope
  ? `${props.activeScope.environment} · ${props.activeScope.namespaces.join(", ")}`
  : "Operational Scope 暂不可用");
const availableProviders = computed(() => props.providerHealth?.filter((item) => item.state === "available").length ?? 0);
const providerCount = computed(() => props.providerHealth?.length ?? 0);

function openNotifications(event: MouseEvent) {
  emit("openNotifications", event.currentTarget as HTMLElement);
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
      <RouterLink class="environment-boundary" to="/settings#operational-scope" :title="scopeDetail">
        <Network :size="15" aria-hidden="true" />
        <span>{{ scopeLabel }}</span>
        <small v-if="providerCount">{{ availableProviders }}/{{ providerCount }}</small>
      </RouterLink>
      <button type="button" class="icon-button notification-button" aria-label="打开通知收件箱" title="通知" @click="openNotifications">
        <Bell :size="19" aria-hidden="true" />
        <span v-if="unreadCount" class="notification-count">{{ unreadCount > 99 ? "99+" : unreadCount }}</span>
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
.header-leading, .header-actions, .product-mark, .breadcrumb, .environment-boundary, .user-trigger { display: flex; align-items: center; }
.header-leading { min-width: 0; gap: var(--co-space-4); }
.header-actions { flex: 0 0 auto; gap: var(--co-space-2); }
.product-mark { min-height: 44px; gap: var(--co-space-2); color: var(--co-text-primary); }
.product-icon, .user-avatar { display: grid; flex: 0 0 auto; place-items: center; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-action-primary); background: var(--co-bg-subtle); }
.product-icon { width: 31px; height: 31px; }
.product-mark strong { font-size: 14px; }
.breadcrumb { min-width: 0; gap: var(--co-space-2); padding-left: var(--co-space-4); border-left: 1px solid var(--co-border-default); color: var(--co-text-muted); font-size: 13px; white-space: nowrap; }
.breadcrumb a { color: var(--co-action-primary); }
.breadcrumb span { overflow: hidden; text-overflow: ellipsis; }
.environment-boundary { min-height: 30px; max-width: 260px; gap: var(--co-space-2); padding: 0 var(--co-space-3); border: 1px solid var(--co-status-info-border); border-radius: var(--co-radius-pill); color: var(--co-status-info-fg); background: var(--co-status-info-bg); font-size: 11px; font-weight: 700; }
.environment-boundary:hover { border-color: var(--co-action-primary); background: var(--co-bg-active); }
.environment-boundary span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.environment-boundary small { padding-left: var(--co-space-2); border-left: 1px solid var(--co-status-info-border); font-family: var(--co-font-mono); font-size: 10px; }
.icon-button { position: relative; display: grid; width: 38px; height: 38px; flex: 0 0 38px; place-items: center; padding: 0; border: 1px solid transparent; border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: transparent; cursor: pointer; }
.icon-button:hover { border-color: var(--co-border-default); color: var(--co-text-primary); background: var(--co-bg-hover); }
.notification-count { position: absolute; top: 2px; right: 1px; min-width: 17px; height: 17px; padding: 0 4px; border: 2px solid var(--co-bg-surface); border-radius: var(--co-radius-pill); color: #fff; background: var(--co-status-critical-fg); font-size: 9px; font-weight: 800; line-height: 13px; }
.user-trigger { gap: var(--co-space-2); padding-left: var(--co-space-2); }
.user-avatar { width: 31px; height: 31px; }
.user-copy { display: grid; line-height: 1.1; }
.user-copy strong { font-size: 12px; }.user-copy small { color: var(--co-text-muted); font-size: 10px; }
@media (max-width: 900px) { .environment-boundary, .user-copy, .product-mark strong { display: none; } .breadcrumb { padding-left: 0; border-left: 0; } }
@media (max-width: 767px) { .app-header { padding-inline: max(var(--co-space-3), env(safe-area-inset-left)) max(var(--co-space-3), env(safe-area-inset-right)); } .product-mark { display: none; } .header-leading { overflow: hidden; } .breadcrumb a, .breadcrumb svg { display: none; } .breadcrumb span { color: var(--co-text-primary); font-weight: 750; } }
</style>
