<script setup lang="ts">
import { useVirtualizer } from "@tanstack/vue-virtual";
import type { ComponentPublicInstance } from "vue";
import { computed, nextTick, ref, watch } from "vue";

import { createScaleRows, type ScaleRowFixture } from "../fixtures";

type ScaleMode = "logs" | "traces" | "timeline" | "table";

const cache = new Map<ScaleMode, ScaleRowFixture[]>();
function dataset(mode: ScaleMode) {
  const existing = cache.get(mode);
  if (existing) return existing;
  const created = createScaleRows(mode);
  cache.set(mode, created);
  return created;
}

const mode = ref<ScaleMode>("logs");
const scrollElement = ref<HTMLDivElement | null>(null);
const visibleRows = ref(dataset("logs"));
const wrap = ref(false);
const filterQuery = ref("");
const filtering = ref(false);
const canceledRequests = ref(0);
const filterLatency = ref(0);
const selectedRow = ref<ScaleRowFixture | null>(null);
const copiedId = ref("");
let filterController: AbortController | null = null;

const modeItems = [
  { label: "Logs 10k", value: "logs", icon: "i-lucide-scroll-text" },
  { label: "Trace 2.5k", value: "traces", icon: "i-lucide-git-branch" },
  { label: "Timeline 5k", value: "timeline", icon: "i-lucide-history" },
  { label: "Table 20k", value: "table", icon: "i-lucide-table-properties" },
];

const virtualizer = useVirtualizer<HTMLDivElement, HTMLDivElement>(computed(() => ({
  count: visibleRows.value.length,
  getScrollElement: () => scrollElement.value,
  estimateSize: () => wrap.value ? 58 : 34,
  overscan: 10,
  getItemKey: (index: number) => visibleRows.value[index]?.id ?? index,
})));

const virtualItems = computed(() => virtualizer.value.getVirtualItems());
const totalSize = computed(() => virtualizer.value.getTotalSize());
const selectedOpen = computed({
  get: () => Boolean(selectedRow.value),
  set: (value: boolean) => { if (!value) selectedRow.value = null; },
});

function levelClass(level: ScaleRowFixture["level"]) {
  return level === "ERROR" ? "is-error" : level === "WARN" ? "is-warning" : "is-info";
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

async function copyFull(row: ScaleRowFixture) {
  try {
    await navigator.clipboard.writeText(row.fullValue);
  } catch {
    fallbackCopy(row.fullValue);
  }
  copiedId.value = row.id;
  window.setTimeout(() => { if (copiedId.value === row.id) copiedId.value = ""; }, 1200);
}

async function applyFilter(query = filterQuery.value) {
  filterController?.abort();
  const controller = new AbortController();
  filterController = controller;
  filtering.value = true;
  const started = performance.now();
  await new Promise<void>((resolve) => window.setTimeout(resolve, 120));
  if (controller.signal.aborted) {
    canceledRequests.value += 1;
    return;
  }
  const normalized = query.trim().toLocaleLowerCase();
  visibleRows.value = normalized
    ? dataset(mode.value).filter((row) => `${row.id} ${row.source} ${row.level} ${row.message}`.toLocaleLowerCase().includes(normalized))
    : dataset(mode.value);
  filtering.value = false;
  filterLatency.value = Number((performance.now() - started).toFixed(1));
  await nextTick();
  virtualizer.value.scrollToIndex(0);
  virtualizer.value.measure();
}

function simulateStaleCancellation() {
  filterQuery.value = "cloudops-api";
  void applyFilter("cloudops-api");
  window.setTimeout(() => {
    filterQuery.value = "worker";
    void applyFilter("worker");
  }, 20);
}

function scrollTo(position: "start" | "middle" | "end") {
  const index = position === "start" ? 0 : position === "middle" ? Math.floor(visibleRows.value.length / 2) : Math.max(0, visibleRows.value.length - 1);
  virtualizer.value.scrollToIndex(index, { align: position === "start" ? "start" : position === "end" ? "end" : "center" });
}

function measureRow(element: Element | ComponentPublicInstance | null) {
  const resolved = element instanceof Element ? element : element?.$el;
  if (resolved instanceof HTMLDivElement) virtualizer.value.measureElement(resolved);
}

watch(mode, async (value) => {
  filterController?.abort();
  filterQuery.value = "";
  visibleRows.value = dataset(value);
  selectedRow.value = null;
  filtering.value = false;
  await nextTick();
  virtualizer.value.scrollToIndex(0);
  virtualizer.value.measure();
});
watch(wrap, () => void nextTick(() => virtualizer.value.measure()));
</script>

<template>
  <section class="workspace scale-lab" aria-labelledby="scale-title">
    <header class="workspace-header">
      <div class="workspace-title">
        <h1 id="scale-title" tabindex="-1">大数据渲染边界</h1>
        <p>Logs、Trace Span、Incident timeline 与大型 Table 使用同一虚拟化边界。</p>
      </div>
      <div class="workspace-actions">
        <UBadge color="neutral" variant="subtle" icon="i-lucide-database" :label="`${visibleRows.length.toLocaleString()} rows`" />
        <UBadge color="info" variant="subtle" icon="i-lucide-gauge" :label="`${virtualItems.length} rendered`" data-testid="virtual-render-count" />
      </div>
    </header>

    <section class="toolbar-band scale-toolbar" aria-label="大数据工具栏">
      <UTabs v-model="mode" :items="modeItems" value-key="value" size="sm" data-testid="scale-mode" />
      <div class="toolbar-group is-search"><span class="toolbar-label">Filter</span><UInput v-model="filterQuery" icon="i-lucide-search" placeholder="ID、Source、Level 或内容" data-testid="scale-filter" @keyup.enter="applyFilter()" /></div>
      <UButton color="primary" variant="outline" icon="i-lucide-filter" label="筛选" :loading="filtering" data-testid="apply-scale-filter" @click="applyFilter()" />
      <USwitch v-model="wrap" label="换行" data-testid="wrap-toggle" />
      <UTooltip text="顶部"><UButton color="neutral" variant="ghost" square icon="i-lucide-arrow-up-to-line" aria-label="滚动到顶部" @click="scrollTo('start')" /></UTooltip>
      <UTooltip text="中部"><UButton color="neutral" variant="ghost" square icon="i-lucide-move-vertical" aria-label="滚动到中部" @click="scrollTo('middle')" /></UTooltip>
      <UTooltip text="底部"><UButton color="neutral" variant="ghost" square icon="i-lucide-arrow-down-to-line" aria-label="滚动到底部" data-testid="scroll-scale-end" @click="scrollTo('end')" /></UTooltip>
    </section>

    <section class="request-band" aria-live="polite">
      <span><UIcon name="i-lucide-timer-reset" aria-hidden="true" />filter {{ filterLatency }} ms</span>
      <span><UIcon name="i-lucide-ban" aria-hidden="true" />stale requests canceled {{ canceledRequests }}</span>
      <UButton color="neutral" variant="ghost" icon="i-lucide-layers-2" label="注入竞态" data-testid="simulate-stale-filter" @click="simulateStaleCancellation" />
    </section>

    <section class="virtual-table" :class="{ 'is-wrapped': wrap }" aria-label="虚拟化数据列表">
      <div class="virtual-header" role="row"><span>ID</span><span>UTC / Source</span><span>Level</span><span>内容</span><span>操作</span></div>
      <div ref="scrollElement" class="virtual-viewport" role="rowgroup" data-testid="virtual-viewport">
        <div class="virtual-spacer" :style="{ height: `${totalSize}px` }">
          <div
            v-for="virtualRow in virtualItems"
            :key="String(virtualRow.key)"
            :ref="measureRow"
            class="virtual-row"
            role="row"
            :data-index="virtualRow.index"
            :data-testid="`virtual-row-${virtualRow.index}`"
            :style="{ transform: `translateY(${virtualRow.start}px)` }"
            @click="selectedRow = visibleRows[virtualRow.index]"
          >
            <code role="cell">{{ visibleRows[virtualRow.index].id }}</code>
            <span role="cell"><time :datetime="visibleRows[virtualRow.index].time">{{ visibleRows[virtualRow.index].time.slice(11, 23) }}</time><small>{{ visibleRows[virtualRow.index].source }}</small></span>
            <strong role="cell" :class="levelClass(visibleRows[virtualRow.index].level)">{{ visibleRows[virtualRow.index].level }}</strong>
            <span class="virtual-message" role="cell" :title="visibleRows[virtualRow.index].message">{{ visibleRows[virtualRow.index].message }}</span>
            <span class="row-actions" role="cell" @click.stop>
              <UTooltip :text="copiedId === visibleRows[virtualRow.index].id ? '已复制完整值' : '复制完整值'">
                <UButton color="neutral" variant="ghost" square :icon="copiedId === visibleRows[virtualRow.index].id ? 'i-lucide-copy-check' : 'i-lucide-copy'" :aria-label="`复制 ${visibleRows[virtualRow.index].id} 完整值`" :data-testid="virtualRow.index === 0 ? 'copy-first-full-value' : undefined" @click="copyFull(visibleRows[virtualRow.index])" />
              </UTooltip>
            </span>
          </div>
        </div>
      </div>
    </section>

    <p class="copy-status" aria-live="polite" data-testid="copy-status">{{ copiedId ? `${copiedId} 完整值已复制` : "" }}</p>

    <USlideover v-model:open="selectedOpen" :title="selectedRow?.id" :description="selectedRow ? `${selectedRow.source} · ${selectedRow.time}` : ''" :ui="{ content: 'w-[min(620px,52vw)] max-w-none' }" data-testid="scale-inspector">
      <template #body>
        <div v-if="selectedRow" class="scale-inspector-body"><code class="full-value">{{ selectedRow.fullValue }}</code><UButton color="neutral" variant="outline" icon="i-lucide-copy" label="复制完整值" @click="copyFull(selectedRow)" /><UButton color="primary" variant="outline" icon="i-lucide-file-check-2" label="交给 Evidence" /></div>
      </template>
    </USlideover>
  </section>
</template>

<style scoped>
.scale-lab { max-width: 1760px; margin: 0 auto; }
.scale-toolbar { align-items: center; }.scale-toolbar .is-search { min-width: 220px; }
.request-band { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; gap: var(--co-space-4); padding: 7px var(--co-space-3); border-bottom: 1px solid var(--co-border); color: var(--co-text-muted); background: var(--co-surface); font-family: var(--co-font-mono); font-size: 9px; }.request-band span { display: inline-flex; align-items: center; gap: 5px; }.request-band > :last-child { margin-left: auto; }
.virtual-table { min-width: 0; border-bottom: 1px solid var(--co-border); background: var(--co-code-surface); }.virtual-header, .virtual-row { display: grid; min-width: 0; grid-template-columns: 116px 142px 64px minmax(260px, 1fr) 42px; align-items: center; column-gap: 8px; }.virtual-header { height: 34px; padding: 0 8px; color: var(--co-text-secondary); background: var(--co-surface-muted); font-size: 9px; font-weight: 700; text-transform: uppercase; }.virtual-viewport { position: relative; height: min(570px, calc(100dvh - 330px)); min-height: 380px; overflow: auto; contain: strict; overscroll-behavior: contain; }.virtual-spacer { position: relative; width: 100%; }.virtual-row { position: absolute; top: 0; left: 0; width: 100%; min-height: 34px; padding: 3px 8px; border-bottom: 1px solid color-mix(in srgb, var(--co-code-text) 12%, transparent); color: var(--co-code-text); cursor: pointer; font-family: var(--co-font-mono); font-size: 9px; }.virtual-row:hover, .virtual-row:focus-within { background: color-mix(in srgb, var(--co-action) 18%, var(--co-code-surface)); }.virtual-row code { color: #9fd3f5; }.virtual-row span { min-width: 0; }.virtual-row time, .virtual-row small { display: block; }.virtual-row small { margin-top: 1px; color: #8fa1ad; }.virtual-row strong { font-size: 9px; }.is-error { color: #ff9b91; }.is-warning { color: #f2c15d; }.is-info { color: #9fd3f5; }.virtual-message { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.is-wrapped .virtual-message { overflow: visible; white-space: normal; overflow-wrap: anywhere; }.row-actions { display: flex; justify-content: center; }.copy-status { min-height: 20px; margin: 0; padding: 3px 8px; color: var(--co-success-fg); background: var(--co-surface); font-size: 9px; }
.scale-inspector-body { display: grid; gap: var(--co-space-3); }.full-value { padding: var(--co-space-3); overflow-wrap: anywhere; color: var(--co-code-text); background: var(--co-code-surface); font-size: 10px; line-height: 1.55; }
@media (max-width: 1180px) {
  .virtual-header, .virtual-row { grid-template-columns: 100px 116px 54px minmax(220px, 1fr) 38px; }.request-band > :last-child { margin-left: 0; }
}
</style>
