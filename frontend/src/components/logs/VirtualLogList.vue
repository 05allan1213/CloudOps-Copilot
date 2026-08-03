<script setup lang="ts">
import { useVirtualizer } from "@tanstack/vue-virtual";
import type { ComponentPublicInstance } from "vue";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";

import type { LogEntry } from "../../api/telemetry";
import { logRawValue } from "../../models/telemetry";
import {
  appendedLogCount,
  readLogReadingPosition,
  rememberLogReadingPosition,
} from "./logsReadingPosition";

const props = defineProps<{
  entries: LogEntry[];
  wrap: boolean;
  selectedIDs: Set<string>;
  inspectedID: string;
  queryIdentity: string;
  follow: boolean;
  highlight?: string;
}>();

const emit = defineEmits<{
  inspect: [entry: LogEntry, trigger: HTMLElement];
  toggle: [entryID: string];
  openTrace: [entry: LogEntry];
  copied: [entryID: string];
}>();

const viewport = ref<HTMLDivElement | null>(null);
const copiedID = ref("");
const pendingNewEntries = ref(0);
let copyTimer: number | undefined;

const virtualizer = useVirtualizer<HTMLDivElement, HTMLElement>(computed(() => ({
  count: props.entries.length,
  getScrollElement: () => viewport.value,
  estimateSize: () => props.wrap ? 96 : 64,
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

function messageParts(message: string): { value: string; highlighted: boolean }[] {
  const needle = props.highlight?.trim();
  if (!needle) return [{ value: message, highlighted: false }];
  const lowerMessage = message.toLocaleLowerCase();
  const lowerNeedle = needle.toLocaleLowerCase();
  const parts: { value: string; highlighted: boolean }[] = [];
  let cursor = 0;
  while (cursor < message.length) {
    const match = lowerMessage.indexOf(lowerNeedle, cursor);
    if (match < 0) {
      parts.push({ value: message.slice(cursor), highlighted: false });
      break;
    }
    if (match > cursor) parts.push({ value: message.slice(cursor, match), highlighted: false });
    parts.push({ value: message.slice(match, match + needle.length), highlighted: true });
    cursor = match + needle.length;
  }
  return parts.length ? parts : [{ value: message, highlighted: false }];
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

function isAtBottom(): boolean {
  const element = viewport.value;
  return Boolean(element && element.scrollHeight - element.scrollTop - element.clientHeight <= 48);
}

function updateReadingPosition() {
  const element = viewport.value;
  if (!element) return;
  rememberLogReadingPosition(props.queryIdentity, element.scrollTop);
  if (isAtBottom()) pendingNewEntries.value = 0;
}

async function scrollToLatest() {
  await nextTick();
  if (props.entries.length) virtualizer.value.scrollToIndex(props.entries.length - 1, { align: "end" });
  pendingNewEntries.value = 0;
  updateReadingPosition();
}

watch(() => props.entries, async (entries, previousEntries) => {
  const appended = appendedLogCount(previousEntries ?? [], entries);
  const followLatest = appended > 0 && props.follow && isAtBottom();
  const currentOffset = viewport.value?.scrollTop ?? 0;
  await nextTick();
  virtualizer.value.measure();
  if (followLatest) await scrollToLatest();
  else {
    virtualizer.value.scrollToOffset(currentOffset);
    pendingNewEntries.value += appended;
  }
});
watch(() => props.queryIdentity, async (identity, previousIdentity) => {
  if (previousIdentity && viewport.value) rememberLogReadingPosition(previousIdentity, viewport.value.scrollTop);
  pendingNewEntries.value = 0;
  await nextTick();
  virtualizer.value.measure();
  virtualizer.value.scrollToOffset(readLogReadingPosition(identity));
});
watch(() => props.wrap, () => void nextTick(() => virtualizer.value.measure()));

onMounted(() => void nextTick(() => virtualizer.value.scrollToOffset(readLogReadingPosition(props.queryIdentity))));
onBeforeUnmount(() => {
  if (copyTimer !== undefined) window.clearTimeout(copyTimer);
  if (viewport.value) rememberLogReadingPosition(props.queryIdentity, viewport.value.scrollTop);
});
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
    @scroll.passive="updateReadingPosition"
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
        :data-level="entries[virtualRow.index].level || 'info'"
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
          <span class="virtual-log-row__meta">
            <time :datetime="entries[virtualRow.index].timestamp">{{ formatTime(entries[virtualRow.index].timestamp) }}</time>
            <span
              class="virtual-log-row__level"
              :data-level="entries[virtualRow.index].level || 'info'"
            >{{ levelLabel(entries[virtualRow.index].level) }}</span>
            <span class="virtual-log-row__source">{{ entries[virtualRow.index].service || entries[virtualRow.index].resource.name }}</span>
          </span>
          <code><template v-for="(part, partIndex) in messageParts(entries[virtualRow.index].message)" :key="partIndex"><mark v-if="part.highlighted">{{ part.value }}</mark><template v-else>{{ part.value }}</template></template></code>
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
    <UButton
      v-if="pendingNewEntries"
      class="virtual-log-list__new-content"
      color="primary"
      icon="i-lucide-arrow-down"
      :label="`${pendingNewEntries} 条新日志`"
      @click="scrollToLatest"
    />
  </div>
</template>

<style scoped>
.virtual-log-list {
  position: relative;
  width: 100%;
  height: min(60vh, 620px);
  min-height: 400px;
  overflow: auto;
  overscroll-behavior: contain;
  border: 1px solid color-mix(in srgb, var(--co-border-strong) 34%, transparent);
  border-radius: var(--co-radius-frame);
  background: var(--co-code-bg);
  color: var(--co-code-text);
  contain: layout paint;
}
.virtual-log-list__new-content { position: sticky; z-index: 4; bottom: var(--co-space-3); display: flex; width: fit-content; margin: 0 auto; box-shadow: var(--co-shadow-overlay); }
.virtual-log-list__spacer { position: relative; min-width: 920px; }
.virtual-log-row {
  position: absolute;
  inset: 0 0 auto;
  display: grid;
  width: 100%;
  min-width: 100%;
  min-height: 64px;
  grid-template-columns: 30px minmax(780px, 1fr) 34px 34px;
  align-items: center;
  gap: var(--co-space-1);
  padding: 0 var(--co-space-2);
  border-bottom: 1px solid color-mix(in srgb, var(--co-code-text) 9%, transparent);
  background: transparent;
  transition: background var(--co-motion-fast) var(--co-ease-out);
}
.virtual-log-row:hover,
.virtual-log-row.is-inspected { background: color-mix(in srgb, var(--co-code-text) 7%, transparent); }
.virtual-log-row::after {
  position: absolute;
  z-index: 1;
  top: 50%;
  left: 3px;
  width: 3px;
  height: 30px;
  border-radius: var(--co-radius-pill);
  background: color-mix(in srgb, var(--co-code-text) 24%, transparent);
  content: "";
  transform: translateY(-50%);
}
.virtual-log-row[data-level="error"]::after,
.virtual-log-row[data-level="fatal"]::after { background: var(--co-status-critical-fg); }
.virtual-log-row[data-level="warn"]::after,
.virtual-log-row[data-level="warning"]::after { background: var(--co-status-warning-fg); }
.virtual-log-row[data-level="info"]::after { background: var(--co-status-success-fg); }
.virtual-log-row.is-inspected::after { box-shadow: 0 0 0 3px color-mix(in srgb, currentColor 18%, transparent); }
.virtual-log-row > * { position: relative; z-index: 1; }
.virtual-log-row__inspect {
  display: grid;
  min-width: 780px;
  grid-template-rows: auto auto;
  justify-content: stretch;
  gap: 5px;
  padding: var(--co-space-2) var(--co-space-3);
  text-align: left;
}
.virtual-log-row :deep(button) { color: var(--co-code-text); }
.virtual-log-row__inspect:hover { background: transparent; }
.virtual-log-row__meta { display: flex; min-width: 0; align-items: center; gap: var(--co-space-2); }
.virtual-log-row__inspect time {
  color: color-mix(in srgb, var(--co-code-text) 66%, transparent);
  font-family: var(--co-font-mono);
  font-size: 10px;
  font-variant-numeric: tabular-nums;
}
.virtual-log-row__inspect code {
  min-width: 0;
  overflow: hidden;
  color: var(--co-code-text);
  font-family: var(--co-font-mono);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: pre;
}
.virtual-log-row__source { min-width: 0; overflow: hidden; color: var(--co-status-success-fg); font-family: var(--co-font-mono); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.virtual-log-row__inspect mark { border-radius: 3px; background: var(--co-status-warning-bg); color: var(--co-status-warning-fg); }
.virtual-log-row__level { width: fit-content; padding: 2px 6px; border-radius: var(--co-radius-pill); background: color-mix(in srgb, var(--co-code-text) 9%, transparent); font-family: var(--co-font-mono); font-size: 10px; font-weight: 800; }
.virtual-log-row__level[data-level="error"],
.virtual-log-row__level[data-level="fatal"] { color: var(--co-status-critical-fg); }
.virtual-log-row__level[data-level="warn"],
.virtual-log-row__level[data-level="warning"] { color: var(--co-status-warning-fg); }
.virtual-log-row__level[data-level="info"] { color: var(--co-status-info-fg); }
.is-wrapped .virtual-log-list__spacer { min-width: 100%; }
.is-wrapped .virtual-log-row { width: 100%; grid-template-columns: 30px minmax(0, 1fr) 34px 34px; }
.is-wrapped .virtual-log-row__inspect { min-width: 0; }
.is-wrapped .virtual-log-row__inspect code { overflow: visible; overflow-wrap: anywhere; text-overflow: clip; white-space: pre-wrap; }

@media (max-width: 1024px) {
  .virtual-log-list { height: 480px; }
  .virtual-log-list__spacer { min-width: 760px; }
  .virtual-log-row { grid-template-columns: 30px minmax(620px, 1fr) 34px 34px; }
  .virtual-log-row__inspect { min-width: 620px; }
  .is-wrapped .virtual-log-list__spacer { min-width: 100%; }
  .is-wrapped .virtual-log-row__inspect { min-width: 0; }
}

@media (prefers-reduced-motion: reduce) {
  .virtual-log-row { transition: none; }
}
</style>
