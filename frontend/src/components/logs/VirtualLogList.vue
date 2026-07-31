<script setup lang="ts">
import { useVirtualizer } from "@tanstack/vue-virtual";
import type { ComponentPublicInstance } from "vue";
import { computed, nextTick, ref, watch } from "vue";

import type { LogEntry } from "../../api/telemetry";
import { logRawValue } from "../../models/telemetry";

const props = defineProps<{
  entries: LogEntry[];
  wrap: boolean;
  selectedIDs: Set<string>;
  inspectedID: string;
}>();

const emit = defineEmits<{
  inspect: [entry: LogEntry, trigger: HTMLElement];
  toggle: [entryID: string];
  openTrace: [entry: LogEntry];
  copied: [entryID: string];
}>();

const viewport = ref<HTMLDivElement | null>(null);
const copiedID = ref("");
let copyTimer: number | undefined;

const virtualizer = useVirtualizer<HTMLDivElement, HTMLElement>(computed(() => ({
  count: props.entries.length,
  getScrollElement: () => viewport.value,
  estimateSize: () => props.wrap ? 96 : 54,
  overscan: 8,
  getItemKey: (index: number) => props.entries[index]?.id ?? index,
})));
const virtualRows = computed(() => virtualizer.value.getVirtualItems());
const totalSize = computed(() => virtualizer.value.getTotalSize());

const dateFormatter = new Intl.DateTimeFormat("zh-CN", {
  dateStyle: "medium",
  timeStyle: "medium",
});

function formatTime(value: string): string {
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : dateFormatter.format(parsed);
}

function levelLabel(level?: string): string {
  return level?.toUpperCase() || "INFO";
}

function measureRow(element: Element | ComponentPublicInstance | null) {
  const resolved = element instanceof Element ? element : element?.$el;
  if (resolved instanceof HTMLElement) virtualizer.value.measureElement(resolved);
}

function fallbackCopy(value: string) {
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.append(textarea);
  textarea.select();
  document.execCommand("copy");
  textarea.remove();
}

async function copyEntry(entry: LogEntry) {
  const value = logRawValue(entry.message);
  try {
    await navigator.clipboard.writeText(value);
  } catch {
    fallbackCopy(value);
  }
  copiedID.value = entry.id;
  emit("copied", entry.id);
  window.clearTimeout(copyTimer);
  copyTimer = window.setTimeout(() => { copiedID.value = ""; }, 1200);
}

function inspectEntry(event: MouseEvent, entry: LogEntry) {
  const trigger = event.currentTarget;
  if (trigger instanceof HTMLElement) emit("inspect", entry, trigger);
}

watch(() => props.entries, async () => {
  await nextTick();
  virtualizer.value.scrollToOffset(0);
  virtualizer.value.measure();
});
watch(() => props.wrap, () => void nextTick(() => virtualizer.value.measure()));
</script>

<template>
  <div
    ref="viewport"
    class="virtual-log-list"
    :class="{ 'is-wrapped': wrap }"
    role="list"
    :aria-label="`${entries.length} 条虚拟化日志`"
    :aria-setsize="entries.length"
    data-testid="virtual-log-list"
    :data-rendered-count="virtualRows.length"
  >
    <div
      class="virtual-log-list__spacer"
      :style="{ height: `${totalSize}px` }"
    >
      <article
        v-for="virtualRow in virtualRows"
        :key="String(virtualRow.key)"
        :ref="measureRow"
        class="virtual-log-row"
        :class="{ 'is-inspected': inspectedID === entries[virtualRow.index]?.id }"
        role="listitem"
        :aria-posinset="virtualRow.index + 1"
        :aria-setsize="entries.length"
        :data-index="virtualRow.index"
        :style="{ transform: `translateY(${virtualRow.start}px)` }"
      >
        <UCheckbox
          :model-value="selectedIDs.has(entries[virtualRow.index].id)"
          :aria-label="`选择 ${formatTime(entries[virtualRow.index].timestamp)} 的日志作为 Evidence`"
          @update:model-value="emit('toggle', entries[virtualRow.index].id)"
        />
        <UButton
          class="virtual-log-row__inspect"
          color="neutral"
          variant="ghost"
          :data-log-entry-id="entries[virtualRow.index].id"
          :aria-label="`检查 ${formatTime(entries[virtualRow.index].timestamp)} 的日志`"
          @click="inspectEntry($event, entries[virtualRow.index])"
        >
          <time :datetime="entries[virtualRow.index].timestamp">{{ formatTime(entries[virtualRow.index].timestamp) }}</time>
          <span
            class="virtual-log-row__level"
            :data-level="entries[virtualRow.index].level || 'info'"
          >{{ levelLabel(entries[virtualRow.index].level) }}</span>
          <code>{{ entries[virtualRow.index].message }}</code>
        </UButton>
        <UTooltip text="复制完整原文">
          <UButton
            color="neutral"
            variant="ghost"
            :icon="copiedID === entries[virtualRow.index].id ? 'i-lucide-check' : 'i-lucide-copy'"
            square
            :aria-label="`复制 ${formatTime(entries[virtualRow.index].timestamp)} 的完整日志原文`"
            @click="copyEntry(entries[virtualRow.index])"
          />
        </UTooltip>
        <UTooltip
          v-if="entries[virtualRow.index].trace_id"
          text="打开关联 Trace"
        >
          <UButton
            color="primary"
            variant="ghost"
            icon="i-lucide-git-branch"
            square
            :aria-label="`打开 Trace ${entries[virtualRow.index].trace_id}`"
            @click="emit('openTrace', entries[virtualRow.index])"
          />
        </UTooltip>
        <span
          v-else
          aria-hidden="true"
        />
      </article>
    </div>
  </div>
</template>

<style scoped>
.virtual-log-list {
  width: 100%;
  height: min(56vh, 560px);
  min-height: 340px;
  overflow: auto;
  overscroll-behavior: contain;
  border-block: 1px solid var(--co-border-default);
  background: var(--co-bg-canvas);
  contain: layout paint;
}
.virtual-log-list__spacer { position: relative; min-width: 100%; }
.virtual-log-row {
  position: absolute;
  inset: 0 0 auto;
  display: grid;
  width: max-content;
  min-width: 100%;
  grid-template-columns: 30px minmax(780px, 1fr) 34px 34px;
  align-items: center;
  gap: var(--co-space-1);
  min-height: 54px;
  padding: var(--co-space-1) var(--co-space-2);
  border-bottom: 1px solid var(--co-border-subtle);
  background: var(--co-bg-canvas);
}
.virtual-log-row:hover,
.virtual-log-row.is-inspected { background: var(--co-bg-hover); }
.virtual-log-row.is-inspected { box-shadow: inset var(--co-severity-marker-width) 0 0 var(--co-action-primary); }
.virtual-log-row__inspect {
  display: grid;
  min-width: 780px;
  grid-template-columns: 190px 64px minmax(520px, max-content);
  justify-content: stretch;
  gap: var(--co-space-2);
  padding-inline: var(--co-space-2);
  text-align: left;
}
.virtual-log-row__inspect time {
  color: var(--co-text-muted);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}
.virtual-log-row__inspect code {
  min-width: 0;
  color: var(--co-text-primary);
  font-family: var(--co-font-mono);
  font-size: 11px;
  white-space: pre;
}
.virtual-log-row__level { font-family: var(--co-font-mono); font-size: 10px; font-weight: 800; }
.virtual-log-row__level[data-level="error"],
.virtual-log-row__level[data-level="fatal"] { color: var(--co-status-critical-fg); }
.virtual-log-row__level[data-level="warn"],
.virtual-log-row__level[data-level="warning"] { color: var(--co-status-warning-fg); }
.virtual-log-row__level[data-level="info"] { color: var(--co-status-info-fg); }
.is-wrapped .virtual-log-row { width: 100%; grid-template-columns: 30px minmax(0, 1fr) 34px 34px; }
.is-wrapped .virtual-log-row__inspect { min-width: 0; grid-template-columns: 190px 64px minmax(260px, 1fr); }
.is-wrapped .virtual-log-row__inspect code { overflow-wrap: anywhere; white-space: pre-wrap; }

@media (max-width: 1024px) {
  .virtual-log-list { height: 480px; }
}
</style>
