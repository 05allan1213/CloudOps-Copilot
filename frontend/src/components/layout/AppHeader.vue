<script setup lang="ts">
import { computed } from "vue";

import type { OperationalScope, ProviderHealth, ProviderState } from "../../api/platform";
import { useTheme } from "../../composables/useTheme";

const props = defineProps<{
  unreadCount: number;
  providerHealth?: ProviderHealth[];
  scenarioState: "inactive" | "active";
  activeScope?: OperationalScope;
  scopes: OperationalScope[];
  selectedScopeId: string;
  scopeSwitching: boolean;
}>();

const emit = defineEmits<{
  openNotifications: [trigger: HTMLElement];
  changeScope: [scopeID: string];
}>();

const { isDark, toggleTheme } = useTheme();
const selectableScopes = computed(() => props.scopes
  .filter((scope): scope is OperationalScope & { id: string } => Boolean(scope.id))
  .map((scope) => ({
    label: scope.cluster_id,
    description: `${scope.environment} · ${scope.namespaces.join(", ")}`,
    value: scope.id,
  })));
const selectedScope = computed(() => props.selectedScopeId || props.activeScope?.id || "");
const scopeSummary = computed(() => props.activeScope
  ? `${props.activeScope.cluster_id} · ${props.activeScope.environment}`
  : "运行范围暂不可用");
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
const scenarioLabel = computed(() => props.scenarioState === "active" ? "Scenario" : "Live");

const providerStateLabels: Record<ProviderState, string> = {
  available: "可用",
  partial: "部分可用",
  unavailable: "不可用",
  disabled: "已禁用",
  not_configured: "未配置",
};

function changeScope(value: unknown) {
  if (typeof value === "string" && value && value !== props.activeScope?.id) emit("changeScope", value);
}

function openNotifications(event: MouseEvent) {
  emit("openNotifications", event.currentTarget as HTMLElement);
}
</script>

<template>
  <header
    class="app-header"
    aria-label="全局运行上下文"
  >
    <div class="context-cluster">
      <span class="context-orbit" aria-hidden="true">
        <UIcon name="i-lucide-orbit" />
      </span>
      <div class="scope-control">
        <span>Operational scope</span>
        <USelect
          :model-value="selectedScope"
          :items="selectableScopes"
          value-key="value"
          label-key="label"
          variant="none"
          :loading="scopeSwitching"
          :disabled="scopeSwitching || selectableScopes.length < 2"
          :placeholder="activeScope?.cluster_id || '运行范围'"
          :aria-label="`活动运行范围：${scopeSummary}`"
          :title="scopeSummary"
          :ui="{ base: 'scope-select-base', value: 'scope-select-value', trailing: 'scope-select-trailing' }"
          @update:model-value="changeScope"
        />
      </div>
      <span class="context-divider" aria-hidden="true" />
      <span class="environment-label">
        {{ activeScope?.environment || "local" }}
      </span>
      <span
        class="live-state"
        :class="{ 'is-scenario': scenarioState === 'active' }"
        role="status"
      >
        <i aria-hidden="true" />
        {{ scenarioLabel }}
      </span>
    </div>

    <div class="header-actions">
      <UTooltip :content="{ side: 'bottom', align: 'end' }">
        <UButton
          class="provider-health-trigger"
          color="neutral"
          variant="ghost"
          icon="i-lucide-plug-zap"
          :label="providerSummary"
          to="/settings#providers"
          :aria-label="providerActionLabel"
          :class="{ 'has-warning': providerWarning }"
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
            <footer>打开 Provider 设置查看完整上下文</footer>
          </section>
        </template>
      </UTooltip>

      <span class="action-divider" aria-hidden="true" />

      <div class="notification-control">
        <UTooltip text="通知" :content="{ side: 'bottom' }">
          <UButton
            color="neutral"
            variant="ghost"
            icon="i-lucide-bell"
            square
            :aria-label="notificationActionLabel"
            @click="openNotifications"
          />
        </UTooltip>
        <span
          v-if="unreadCount"
          class="notification-count"
          aria-hidden="true"
        >{{ notificationCountLabel }}</span>
      </div>

      <UTooltip :text="themeActionLabel" :content="{ side: 'bottom' }">
        <UButton
          color="neutral"
          variant="ghost"
          :icon="isDark ? 'i-lucide-sun' : 'i-lucide-moon'"
          square
          :aria-label="themeActionLabel"
          @click="toggleTheme"
        />
      </UTooltip>

      <UTooltip text="本地 Owner" :content="{ side: 'bottom', align: 'end' }">
        <UButton
          class="owner-action"
          color="neutral"
          variant="ghost"
          icon="i-lucide-user-round"
          square
          aria-label="本地 Owner"
        />
      </UTooltip>
    </div>
  </header>
</template>

<style scoped>
.app-header {
  position: relative;
  z-index: var(--co-z-header);
  height: var(--co-header-height);
  flex: 0 0 var(--co-header-height);
  pointer-events: none;
}

.context-cluster,
.header-actions {
  position: absolute;
  top: 20px;
  display: flex;
  height: 40px;
  align-items: center;
  border: 1px solid color-mix(in srgb, var(--co-border-default) 46%, transparent);
  background: color-mix(in srgb, var(--co-bg-canvas) 84%, transparent);
  box-shadow: 0 7px 22px rgb(52 46 39 / 5%);
  backdrop-filter: blur(12px);
  pointer-events: auto;
}

.context-cluster {
  left: clamp(18px, 2vw, 28px);
  max-width: min(520px, calc(100% - 330px));
  gap: 8px;
  padding: 4px 10px 4px 5px;
  border-radius: 12px;
}

.context-orbit {
  display: grid;
  width: 30px;
  height: 30px;
  flex: 0 0 30px;
  place-items: center;
  border: 1px solid color-mix(in srgb, var(--co-viz-live) 16%, var(--co-border-default));
  border-radius: 9px;
  color: var(--co-viz-live);
  background: color-mix(in srgb, var(--co-bg-surface) 80%, transparent);
}

.context-orbit :deep(svg) { width: 17px; height: 17px; }
.scope-control { display: grid; min-width: 0; gap: 1px; }
.scope-control > span {
  padding-left: 8px;
  color: var(--co-text-muted);
  font-family: var(--co-font-mono);
  font-size: 8px;
  font-weight: 700;
  line-height: 1;
  text-transform: uppercase;
}
.scope-control :deep(.scope-select-base) {
  width: auto;
  min-height: 24px;
  min-width: 0;
  max-width: 220px;
  padding: 0 28px 0 8px;
  color: var(--co-text-primary);
  font-size: 12px;
  font-weight: 700;
  box-shadow: none;
}
.scope-control :deep(.scope-select-value) { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.scope-control :deep(.scope-select-trailing) { right: 4px; width: 20px; justify-content: center; }
.context-divider,
.action-divider { width: 1px; height: 22px; flex: 0 0 1px; background: var(--co-border-default); }
.environment-label {
  max-width: 90px;
  overflow: hidden;
  color: var(--co-text-secondary);
  font-family: var(--co-font-mono);
  font-size: 10px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.live-state {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--co-status-success-fg);
  font-family: var(--co-font-mono);
  font-size: 9px;
  font-weight: 800;
  letter-spacing: .08em;
  text-transform: uppercase;
}
.live-state i {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--co-viz-live);
  box-shadow: 0 0 0 4px var(--co-viz-live-soft);
}
.live-state.is-scenario { color: var(--co-status-warning-fg); }
.live-state.is-scenario i { background: var(--co-viz-amber); box-shadow: 0 0 0 4px color-mix(in srgb, var(--co-viz-amber) 12%, transparent); }

.header-actions {
  right: clamp(18px, 2vw, 28px);
  gap: 2px;
  padding: 3px 5px;
  border-radius: 12px;
}
.header-actions :deep(button),
.header-actions :deep(a) { width: 32px; min-width: 32px; height: 32px; border-radius: 9px; }
.provider-health-trigger { width: auto !important; font-family: var(--co-font-mono); font-variant-numeric: tabular-nums; }
.provider-health-trigger.has-warning { color: var(--co-status-warning-fg); }
.action-divider { margin-inline: 3px; }
.notification-control { position: relative; display: grid; place-items: center; }
.notification-count {
  position: absolute;
  top: -2px;
  right: -3px;
  display: grid;
  min-width: 15px;
  height: 15px;
  padding-inline: 3px;
  place-items: center;
  border: 2px solid var(--co-bg-floating);
  border-radius: 999px;
  color: white;
  background: var(--co-viz-failure);
  font-size: 8px;
  font-weight: 800;
  line-height: 1;
  pointer-events: none;
}
.owner-action { background: transparent; }

.provider-health-detail {
  display: grid;
  width: 340px;
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
.provider-health-detail small { color: var(--co-text-muted); font-size: 11px; }
.provider-health-detail ul { display: grid; gap: var(--co-space-2); margin: 0; padding: 0; list-style: none; }
.provider-health-detail li { padding-top: var(--co-space-2); border-top: 1px solid var(--co-border-default); }
.provider-health-detail small { grid-column: 1 / -1; overflow-wrap: anywhere; }
.provider-name { font-family: var(--co-font-mono); font-size: 12px; }
.provider-health-detail p { margin: 0; }
.provider-health-detail footer { padding-top: var(--co-space-2); border-top: 1px solid var(--co-border-default); }

@media (max-width: 1160px) {
  .context-cluster { max-width: calc(100% - 246px); }
  .environment-label, .context-divider { display: none; }
}
</style>
