<script setup lang="ts">
import { ref } from "vue";

import type { AlertView } from "../../api/alerts";
import WorkspaceDenseList from "../workspace/WorkspaceDenseList.vue";
import AlertBadges from "./AlertBadges.vue";

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
    <WorkspaceDenseList
      :items="items"
      :item-key="(item) => item.id"
      :selected-key="selectedId"
      :severity="severityTone"
      label="Alert 处置队列；选择一项打开 Inspector"
      @select="(item, trigger) => emit('select', item, trigger)"
    >
      <template #leading="{ item }">
        <AlertBadges
          :status="item.status"
          :severity="item.severity"
        />
      </template>
      <template #title="{ item }">
        <span
          :data-alert-id="item.id"
          data-testid="alert-row-summary"
        >{{ item.summary }}</span>
      </template>
      <template #description="{ item }">
        <span>{{ item.namespace }}/{{ item.target_name }} · {{ item.target_kind }} · {{ item.service_name }}</span>
        <span>{{ item.signal_count }} Signals · {{ dispositionLabel(item) }}</span>
      </template>
      <template #meta="{ item }">
        <time
          :datetime="item.last_seen_at"
          :title="item.last_seen_at"
        >{{ formatRelative(item.last_seen_at) }}</time>
      </template>
      <template #trailing>
        <UIcon
          name="i-lucide-chevron-right"
          aria-hidden="true"
        />
      </template>
    </WorkspaceDenseList>
  </section>
</template>

<style scoped>
.alert-queue { min-width: 0; overflow: auto; background: var(--co-bg-surface); }
.alert-queue :deep(.workspace-dense-list-leading) { width: 132px; }
.alert-queue :deep(.workspace-dense-list-copy small) { display: grid; gap: 2px; }
.alert-queue time { white-space: nowrap; font-variant-numeric: tabular-nums; }

@media (max-width: 1120px) {
  .alert-queue :deep(.workspace-dense-list-leading) { width: 112px; }
}

@media (prefers-reduced-motion: reduce) {
  .alert-queue :deep(*) { scroll-behavior: auto; }
}
</style>
