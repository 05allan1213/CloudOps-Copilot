<script setup lang="ts">
import { ref } from "vue";

import type { AlertView } from "../../api/alerts";

defineProps<{
  items: AlertView[];
  selectedId?: string;
}>();

const emit = defineEmits<{
  select: [alert: AlertView, trigger: HTMLElement | null];
}>();

const root = ref<HTMLElement | null>(null);

function severityTone(item: AlertView): "critical" | "warning" | "info" | "neutral" {
  if (item.severity === "critical") return "critical";
  if (item.severity === "warning") return "warning";
  if (item.severity === "info") return "info";
  return "neutral";
}

function dispositionLabel(item: AlertView): string {
  const facts: string[] = [];
  if (item.acknowledgement) facts.push("已知悉");
  if (item.silence?.status === "active") facts.push("静默中");
  if (item.incident_links.length) facts.push(`${item.incident_links.length} 个 Incident`);
  if (item.investigations.length) facts.push(`${item.investigations.length} 次调查`);
  return facts.length ? facts.join(" · ") : "等待处置";
}

function severityLabel(item: AlertView): string {
  return ({ critical: "严重", warning: "警告", info: "信息", unknown: "未知" } as const)[item.severity];
}

function statusLabel(item: AlertView): string {
  return item.status === "firing" ? "正在触发" : "已恢复";
}

function selectItem(event: MouseEvent, item: AlertView) {
  emit("select", item, event.currentTarget instanceof HTMLElement ? event.currentTarget : null);
}

function formatRelative(value: string): string {
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) return value;
  const seconds = Math.round((timestamp - Date.now()) / 1000);
  const formatter = new Intl.RelativeTimeFormat("zh-CN", { numeric: "auto" });
  if (Math.abs(seconds) < 60) return formatter.format(seconds, "second");
  const minutes = Math.round(seconds / 60);
  if (Math.abs(minutes) < 60) return formatter.format(minutes, "minute");
  const hours = Math.round(minutes / 60);
  if (Math.abs(hours) < 24) return formatter.format(hours, "hour");
  return formatter.format(Math.round(hours / 24), "day");
}

function getRowElement(alertID: string): HTMLElement | null {
  const escaped = typeof CSS === "undefined" ? alertID : CSS.escape(alertID);
  return root.value?.querySelector<HTMLElement>(`[data-alert-id="${escaped}"]`)?.closest<HTMLElement>("button") ?? null;
}

function getScrollElement(): HTMLElement | null {
  return root.value;
}

defineExpose({ getRowElement, getScrollElement });
</script>

<template>
  <section
    ref="root"
    class="alert-queue"
    data-testid="alert-results"
    aria-label="Alert 处置队列"
  >
    <ul>
      <li
        v-for="item in items"
        :key="item.id"
        :data-severity="severityTone(item)"
        :data-status="item.status"
      >
        <UButton
          class="alert-queue__item"
          :class="{ 'is-selected': selectedId === item.id }"
          color="neutral"
          variant="ghost"
          :aria-current="selectedId === item.id ? 'true' : undefined"
          @click="selectItem($event, item)"
        >
          <span
            class="alert-queue__state"
            :data-status="item.status"
            :data-severity="severityTone(item)"
          >
            <UIcon
              :name="item.status === 'firing' ? 'i-lucide-siren' : 'i-lucide-circle-check'"
              aria-hidden="true"
            />
            <span>{{ item.status === "resolved" ? "恢复" : severityLabel(item) }}</span>
          </span>
          <span class="alert-queue__copy">
            <span class="alert-queue__titleline">
              <strong
                :data-alert-id="item.id"
                data-testid="alert-row-summary"
              >{{ item.summary }}</strong>
              <time
                :datetime="item.last_seen_at"
                :title="item.last_seen_at"
              >{{ formatRelative(item.last_seen_at) }}</time>
            </span>
            <span class="alert-queue__target">
              <UIcon
                name="i-lucide-box"
                aria-hidden="true"
              />
              {{ item.target_kind }} · {{ item.namespace }}/{{ item.target_name }}
              <span v-if="item.service_name">· {{ item.service_name }}</span>
            </span>
            <span class="alert-queue__disposition">
              <b>{{ statusLabel(item) }}</b>
              <span>{{ item.signal_count }} 次 Signal</span>
              <span>{{ dispositionLabel(item) }}</span>
            </span>
          </span>
          <span class="alert-queue__open">
            <UIcon
              name="i-lucide-chevron-right"
              aria-hidden="true"
            />
          </span>
        </UButton>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.alert-queue {
  min-width: 0;
  overflow: visible;
}
.alert-queue ul {
  display: grid;
  margin: 0;
  padding: 0;
  overflow: visible;
  gap: var(--co-space-2);
  list-style: none;
}
.alert-queue li {
  position: relative;
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--co-border-subtle);
  border-radius: var(--co-radius-frame);
  background: color-mix(in srgb, var(--co-bg-surface) 88%, var(--co-bg-canvas));
  box-shadow: var(--co-shadow-row);
  transition: border-color var(--co-motion-fast) var(--co-ease-out), background var(--co-motion-fast) var(--co-ease-out), box-shadow var(--co-motion-fast) var(--co-ease-out), transform var(--co-motion-fast) var(--co-ease-out);
}
.alert-queue li:hover { z-index: 1; border-color: var(--co-border-default); background: var(--co-bg-hover); box-shadow: var(--co-shadow-section); transform: translateY(-1px); }
.alert-queue li[data-status="firing"][data-severity="critical"] { background: color-mix(in srgb, var(--co-status-critical-bg) 46%, transparent); }
.alert-queue li[data-status="firing"][data-severity="warning"] { background: color-mix(in srgb, var(--co-status-warning-bg) 38%, transparent); }
.alert-queue__item { display: grid; width: 100%; min-width: 0; min-height: 78px; grid-template-columns: 52px minmax(0, 1fr) 34px; align-items: center; justify-content: stretch; gap: var(--co-space-3); border-radius: inherit; padding: var(--co-space-3) var(--co-space-4); text-align: left; }
.alert-queue__item.is-selected { border-radius: inherit; background: var(--co-bg-active); box-shadow: inset 0 0 0 1px var(--co-action-primary); }
.alert-queue__state { display: grid; width: 46px; min-height: 46px; place-content: center; justify-items: center; gap: 3px; border-radius: var(--co-radius-panel); color: var(--co-text-secondary); background: var(--co-bg-canvas); font-size: 10px; font-weight: 800; }
.alert-queue__state[data-status="resolved"] { color: var(--co-status-success-fg); background: var(--co-status-success-bg); }
.alert-queue__state[data-status="firing"][data-severity="critical"] { color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.alert-queue__state[data-status="firing"][data-severity="warning"] { color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }
.alert-queue__state[data-status="firing"][data-severity="info"] { color: var(--co-status-info-fg); background: var(--co-status-info-bg); }
.alert-queue__copy { display: grid; min-width: 0; gap: 5px; }
.alert-queue__titleline { display: flex; min-width: 0; align-items: baseline; justify-content: space-between; gap: var(--co-space-4); }
.alert-queue__titleline strong { min-width: 0; overflow: hidden; color: var(--co-text-primary); font-size: 14px; text-overflow: ellipsis; white-space: nowrap; }
.alert-queue__titleline time { flex: 0 0 auto; color: var(--co-text-muted); font-size: 10px; font-variant-numeric: tabular-nums; white-space: nowrap; }
.alert-queue__target,
.alert-queue__disposition { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; gap: var(--co-space-1); color: var(--co-text-muted); font-size: 10px; }
.alert-queue__target svg { flex: 0 0 auto; }
.alert-queue__disposition b { color: var(--co-text-secondary); font-size: 10px; }
.alert-queue__disposition > span::before { content: "\00b7"; margin-right: var(--co-space-1); }
.alert-queue__open { display: grid; width: 30px; height: 30px; place-items: center; border-radius: var(--co-radius-control); color: var(--co-text-muted); background: color-mix(in srgb, var(--co-bg-canvas) 68%, transparent); transition: background var(--co-motion-fast) var(--co-ease-out), color var(--co-motion-fast) var(--co-ease-out), transform var(--co-motion-fast) var(--co-ease-out); }
.alert-queue li:hover .alert-queue__open,
.alert-queue__item.is-selected .alert-queue__open { transform: translateX(2px); background: var(--co-bg-canvas); color: var(--co-text-primary); }

@media (max-width: 1120px) {
  .alert-queue__item { grid-template-columns: 54px minmax(0, 1fr) 24px; padding-inline: var(--co-space-3); }
  .alert-queue__state { width: 48px; }
}

@media (prefers-reduced-motion: reduce) {
  .alert-queue li,
  .alert-queue__open { transition: none; }
  .alert-queue li:hover .alert-queue__open { transform: none; }
  .alert-queue li:hover { transform: none; }
}
</style>
