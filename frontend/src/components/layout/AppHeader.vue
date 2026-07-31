<script setup lang="ts">
import { computed } from "vue";

import type { ProviderHealth, ProviderState } from "../../api/platform";
import { useTheme } from "../../composables/useTheme";

const props = defineProps<{
  pageTitle: string;
  unreadCount: number;
  providerHealth?: ProviderHealth[];
  scenarioState: "inactive" | "active";
}>();

const emit = defineEmits<{
  openNotifications: [trigger: HTMLElement];
}>();

const { isDark, toggleTheme } = useTheme();
const themeActionLabel = computed(() => (isDark.value ? "切换浅色主题" : "切换深色主题"));
const notificationCountLabel = computed(() => (props.unreadCount > 99 ? "99+" : String(props.unreadCount)));
const notificationActionLabel = computed(() => props.unreadCount > 0
  ? `打开通知收件箱，${notificationCountLabel.value} 条未读`
  : "打开通知收件箱");
const availableProviders = computed(() => props.providerHealth?.filter((item) => item.state === "available").length ?? 0);
const providerCount = computed(() => props.providerHealth?.length ?? 0);
const providerSummary = computed(() => `${availableProviders.value}/${providerCount.value}`);
const providerWarning = computed(() => providerCount.value > 0 && availableProviders.value < providerCount.value);
const providerActionLabel = computed(() => providerCount.value
  ? `Provider 健康：${providerSummary.value} 可用，打开 Provider 设置`
  : "Provider 健康暂不可用，打开 Provider 设置");
const scenarioLabel = computed(() => props.scenarioState === "active" ? "Scenario Active" : "Live Mode");
const breadcrumbItems = computed(() => [
  { label: "控制台", to: "/overview" },
  { label: props.pageTitle || "Workspace" },
]);

const providerStateLabels: Record<ProviderState, string> = {
  available: "可用",
  partial: "部分可用",
  unavailable: "不可用",
  disabled: "已禁用",
  not_configured: "未配置",
};

function openNotifications(event: MouseEvent) {
  emit("openNotifications", event.currentTarget as HTMLElement);
}
</script>

<template>
  <header class="app-header">
    <UBreadcrumb
      class="header-breadcrumb"
      :items="breadcrumbItems"
      separator-icon="i-lucide-chevron-right"
      aria-label="面包屑"
    />

    <div class="header-actions">
      <UTooltip :content="{ side: 'bottom', align: 'end' }">
        <UButton
          class="provider-health-trigger"
          :color="providerWarning ? 'warning' : 'neutral'"
          :variant="providerWarning ? 'soft' : 'ghost'"
          icon="i-lucide-bolt"
          :label="providerSummary"
          to="/settings#providers"
          :aria-label="providerActionLabel"
        />
        <template #content>
          <section
            class="provider-health-detail"
            aria-label="Provider 健康明细"
          >
            <header>
              <strong>Provider 健康</strong>
              <span>{{ providerSummary }} 可用</span>
            </header>
            <ul v-if="providerHealth?.length">
              <li
                v-for="provider in providerHealth"
                :key="provider.provider"
              >
                <span class="provider-name">{{ provider.provider }}</span>
                <UBadge
                  size="xs"
                  :color="provider.state === 'available' ? 'success' : provider.state === 'partial' ? 'warning' : 'neutral'"
                  variant="soft"
                  :label="providerStateLabels[provider.state]"
                />
                <small>{{ provider.detail }}</small>
              </li>
            </ul>
            <p v-else>
              尚未返回 Provider 健康快照。
            </p>
            <footer>按 Enter 打开 Provider 设置</footer>
          </section>
        </template>
      </UTooltip>

      <UBadge
        class="scenario-boundary"
        :color="scenarioState === 'active' ? 'info' : 'success'"
        variant="soft"
        :icon="scenarioState === 'active' ? 'i-lucide-flask-conical' : 'i-lucide-radio'"
        :label="scenarioLabel"
        role="status"
        :aria-label="`运行模式：${scenarioLabel}`"
      />

      <div class="notification-control">
        <UTooltip
          text="通知"
          :content="{ side: 'bottom' }"
        >
          <UButton
            color="neutral"
            variant="ghost"
            icon="i-lucide-bell"
            square
            :aria-label="notificationActionLabel"
            @click="openNotifications"
          />
        </UTooltip>
        <UBadge
          v-if="unreadCount"
          class="notification-count"
          color="error"
          variant="solid"
          size="sm"
          :label="notificationCountLabel"
          aria-hidden="true"
        />
      </div>

      <UTooltip
        :text="themeActionLabel"
        :content="{ side: 'bottom' }"
      >
        <UButton
          color="neutral"
          variant="ghost"
          :icon="isDark ? 'i-lucide-sun' : 'i-lucide-moon'"
          square
          :aria-label="themeActionLabel"
          @click="toggleTheme"
        />
      </UTooltip>

      <span
        class="owner-identity"
        aria-label="本地 Owner 上下文"
      >
        <UAvatar
          icon="i-lucide-user-round"
          size="sm"
          color="neutral"
        />
        <span class="owner-copy"><strong>Owner</strong><small>local</small></span>
      </span>
    </div>
  </header>
</template>

<style scoped>
.app-header {
  position: sticky;
  top: 0;
  z-index: var(--co-z-header);
  display: flex;
  min-height: var(--co-header-height);
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-4);
  padding: 0 max(var(--co-space-4), env(safe-area-inset-right)) 0 var(--co-space-5);
  border-bottom: 1px solid var(--co-border-default);
  background: color-mix(in srgb, var(--co-bg-surface) 96%, transparent);
  backdrop-filter: blur(12px);
}

.header-breadcrumb {
  min-width: 0;
  overflow: hidden;
  font-size: 13px;
}

.header-actions {
  display: flex;
  min-width: 0;
  flex: 0 0 auto;
  align-items: center;
  gap: var(--co-space-2);
}

.provider-health-trigger { font-variant-numeric: tabular-nums; }
.scenario-boundary { min-height: 28px; white-space: nowrap; }

.notification-control {
  position: relative;
  display: grid;
  place-items: center;
}

.notification-count {
  position: absolute;
  top: -4px;
  right: -5px;
  z-index: 1;
  min-width: 18px;
  justify-content: center;
  border: 2px solid var(--co-bg-surface);
  pointer-events: none;
  font-variant-numeric: tabular-nums;
}

.owner-identity {
  display: flex;
  align-items: center;
  gap: var(--co-space-2);
  padding-left: var(--co-space-2);
}

.owner-copy { display: grid; line-height: 1.1; }
.owner-copy strong { font-size: 12px; }
.owner-copy small { color: var(--co-text-muted); font-size: 10px; }

.provider-health-detail {
  display: grid;
  width: min(360px, calc(100vw - 32px));
  gap: var(--co-space-3);
  padding: var(--co-space-2);
  color: var(--co-text-primary);
}

.provider-health-detail header,
.provider-health-detail li {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: var(--co-space-2);
}

.provider-health-detail header span,
.provider-health-detail footer,
.provider-health-detail p,
.provider-health-detail small {
  color: var(--co-text-muted);
  font-size: 11px;
}

.provider-health-detail ul {
  display: grid;
  gap: var(--co-space-2);
  margin: 0;
  padding: 0;
  list-style: none;
}

.provider-health-detail li {
  padding-top: var(--co-space-2);
  border-top: 1px solid var(--co-border-default);
}

.provider-health-detail small {
  grid-column: 1 / -1;
  overflow-wrap: anywhere;
}

.provider-name { font-family: var(--co-font-mono); font-size: 12px; }
.provider-health-detail p { margin: 0; }
.provider-health-detail footer { border-top: 1px solid var(--co-border-default); padding-top: var(--co-space-2); }

@media (max-width: 1100px) {
  .owner-copy { display: none; }
}
</style>
