<script setup lang="ts" generic="T extends Record<string, unknown>">
import type { DropdownMenuItem, TableColumn, TableRow } from "@nuxt/ui";
import { computed, h, onBeforeUnmount, onMounted, ref, watch } from "vue";

import { COPY_FEEDBACK_DURATION_MS } from "../../composables/useCopyFeedback";
import CopyFeedbackButton from "./CopyFeedbackButton.vue";

export type DenseRowSeverity = "critical" | "warning" | "info" | "neutral";

export type DenseTableColumn<T extends Record<string, unknown>> = TableColumn<T> & {
  id: string;
  label: string;
  size: number;
  optional?: boolean;
};

const props = withDefaults(defineProps<{
  rows: T[];
  columns: DenseTableColumn<T>[];
  rowKey: (row: T) => string;
  storageKey: string;
  caption: string;
  criticalColumnIds: string[];
  selectedId?: string;
  empty?: string;
  virtualized?: boolean;
  severity?: (row: T) => DenseRowSeverity;
  copyValue?: (row: T) => string;
}>(), {
  selectedId: "",
  empty: "当前条件下没有结果",
  virtualized: undefined,
  severity: undefined,
  copyValue: undefined,
});

const emit = defineEmits<{
  "update:selectedId": [value: string];
  select: [row: T, trigger: HTMLElement | null];
}>();

const tableRoot = ref<{ $el: HTMLElement } | null>(null);
const columnVisibility = ref<Record<string, boolean>>({});
const copiedRowID = ref("");
const preferenceLoaded = ref(false);
let copyStatusTimer: ReturnType<typeof setTimeout> | undefined;
const preferenceStorageKey = computed(() => `cloudops.table.columns.${props.storageKey}`);
const optionalColumns = computed(() => props.columns.filter((column) => column.optional));
const shouldVirtualize = computed(() => props.virtualized ?? props.rows.length > 250);
const selectedRows = computed<Record<string, boolean>>(() => (
  props.selectedId ? { [props.selectedId]: true } : {}
));
const columnPinning = computed(() => ({
  left: props.criticalColumnIds,
  right: props.copyValue ? ["__copy"] : [],
}));
function rowToken(rowID: string) {
  return encodeURIComponent(rowID).replace(/[^A-Za-z0-9_-]/g, (character) => (
    `_${character.charCodeAt(0).toString(16)}_`
  ));
}

const tableMeta = computed(() => ({
  class: {
    tr: (row: TableRow<T>) => [
      "dense-data-table-row",
      `dense-data-table-row--${props.severity?.(row.original) ?? "neutral"}`,
      `dense-data-table-row-token-${rowToken(props.rowKey(row.original))}`,
    ].join(" "),
  },
}));

type DenseColumnStyle = string | Record<string, string>;

function sizedColumnStyle(existing: unknown, size: number) {
  return (context: unknown): DenseColumnStyle => {
    const candidate = typeof existing === "function"
      ? (existing as (value: unknown) => unknown)(context)
      : existing;
    const dimensions = {
      width: `${size}px`,
      minWidth: `${size}px`,
      maxWidth: `${size}px`,
    };
    if (typeof candidate === "string") {
      return `${candidate};width:${size}px;min-width:${size}px;max-width:${size}px`;
    }
    return {
      ...(candidate && typeof candidate === "object" ? candidate as Record<string, string> : {}),
      ...dimensions,
    };
  };
}

function normalizeColumn(column: DenseTableColumn<T>): DenseTableColumn<T> {
  return {
    ...column,
    minSize: column.size,
    maxSize: column.size,
    meta: {
      ...column.meta,
      style: {
        ...column.meta?.style,
        th: sizedColumnStyle(column.meta?.style?.th, column.size),
        td: sizedColumnStyle(column.meta?.style?.td, column.size),
      },
    },
  } as DenseTableColumn<T>;
}

const tableColumns = computed<DenseTableColumn<T>[]>(() => {
  const source = props.copyValue ? [
    ...props.columns,
    {
      id: "__copy",
      label: "完整值",
      header: "完整值",
      size: 56,
      cell: ({ row }) => h(CopyFeedbackButton, {
        value: props.copyValue?.(row.original) ?? "",
        label: `复制 ${props.rowKey(row.original)} 完整值`,
        successLabel: `${props.rowKey(row.original)} 完整值已复制`,
        "data-copy-row": props.rowKey(row.original),
        onCopied: () => reportCopied(props.rowKey(row.original)),
      }),
      meta: { class: { th: "dense-data-table-copy-cell", td: "dense-data-table-copy-cell" } },
    } as DenseTableColumn<T>,
  ] : props.columns;
  return source.map(normalizeColumn);
});

const columnMenuItems = computed<DropdownMenuItem[][]>(() => [
  optionalColumns.value.map((column) => ({
    label: column.label,
    type: "checkbox" as const,
    checked: columnVisibility.value[column.id] !== false,
    onUpdateChecked: (checked: boolean) => setColumnVisible(column.id, checked),
  })),
]);

function readColumnPreference() {
  preferenceLoaded.value = false;
  const next: Record<string, boolean> = {};
  try {
    const stored = JSON.parse(window.localStorage.getItem(preferenceStorageKey.value) ?? "{}") as unknown;
    if (stored && typeof stored === "object" && !Array.isArray(stored)) {
      for (const column of optionalColumns.value) {
        const value = (stored as Record<string, unknown>)[column.id];
        if (typeof value === "boolean") next[column.id] = value;
      }
    }
  } catch {
    // Storage failure does not make the table unusable.
  }
  for (const id of props.criticalColumnIds) next[id] = true;
  columnVisibility.value = next;
  preferenceLoaded.value = true;
}

function persistColumnPreference() {
  if (!preferenceLoaded.value) return;
  const stored = Object.fromEntries(optionalColumns.value.map((column) => [
    column.id,
    columnVisibility.value[column.id] !== false,
  ]));
  try {
    window.localStorage.setItem(preferenceStorageKey.value, JSON.stringify(stored));
  } catch {
    // The current in-memory preference remains active.
  }
}

function setColumnVisible(columnID: string, visible: boolean) {
  if (props.criticalColumnIds.includes(columnID)) return;
  columnVisibility.value = { ...columnVisibility.value, [columnID]: visible };
}

function resetColumnPreference() {
  columnVisibility.value = Object.fromEntries(optionalColumns.value.map((column) => [column.id, true]));
  for (const id of props.criticalColumnIds) columnVisibility.value[id] = true;
}

function selectRow(event: Event, row: TableRow<T>) {
  const id = props.rowKey(row.original);
  emit("update:selectedId", id);
  emit("select", row.original, event.currentTarget instanceof HTMLElement ? event.currentTarget : null);
}

function activateFocusedRow(event: KeyboardEvent) {
  const target = event.target;
  if (!(target instanceof HTMLElement)) return;
  const row = target.closest<HTMLTableRowElement>("tbody tr[role='button']");
  if (!row || target !== row) return;
  event.preventDefault();
  row.click();
}

function reportCopied(id: string) {
  copiedRowID.value = id;
  if (copyStatusTimer !== undefined) clearTimeout(copyStatusTimer);
  copyStatusTimer = setTimeout(() => {
    if (copiedRowID.value === id) copiedRowID.value = "";
  }, COPY_FEEDBACK_DURATION_MS);
}

function getScrollElement() {
  return tableRoot.value?.$el instanceof HTMLElement ? tableRoot.value.$el : null;
}

function getRowElement(rowID: string) {
  return getScrollElement()?.querySelector<HTMLElement>(`.dense-data-table-row-token-${rowToken(rowID)}`) ?? null;
}

watch(columnVisibility, persistColumnPreference, { deep: true });
watch(preferenceStorageKey, readColumnPreference);
onMounted(readColumnPreference);
onBeforeUnmount(() => {
  if (copyStatusTimer !== undefined) clearTimeout(copyStatusTimer);
});

defineExpose({ getRowElement, getScrollElement });
</script>

<template>
  <section
    class="dense-data-table-composition"
    @keydown.enter="activateFocusedRow"
    @keydown.space="activateFocusedRow"
  >
    <header class="dense-data-table-toolbar">
      <span>{{ rows.length.toLocaleString("zh-CN") }} 项</span>
      <div class="dense-data-table-actions">
        <UDropdownMenu
          v-if="optionalColumns.length"
          :items="columnMenuItems"
          :content="{ align: 'end' }"
        >
          <UTooltip text="选择次要列">
            <UButton
              color="neutral"
              variant="ghost"
              icon="i-lucide-columns-3"
              label="列"
              aria-label="选择次要列"
            />
          </UTooltip>
        </UDropdownMenu>
        <UTooltip
          v-if="optionalColumns.length"
          text="恢复默认列"
        >
          <UButton
            color="neutral"
            variant="ghost"
            icon="i-lucide-rotate-ccw"
            square
            aria-label="恢复默认列"
            @click="resetColumnPreference"
          />
        </UTooltip>
      </div>
    </header>
    <UTable
      ref="tableRoot"
      :data="rows"
      :columns="tableColumns"
      :caption="caption"
      :empty="empty"
      :virtualize="shouldVirtualize ? { estimateSize: 48, overscan: 12 } : false"
      :column-visibility="columnVisibility"
      :column-pinning="columnPinning"
      :row-selection="selectedRows"
      :get-row-id="rowKey"
      :meta="tableMeta"
      :watch-options="{ deep: false }"
      sticky="header"
      class="dense-data-table-viewport"
      :ui="{
        base: 'dense-data-table-base',
        th: 'dense-data-table-header-cell',
        td: 'dense-data-table-cell',
        empty: 'dense-data-table-empty',
      }"
      @select="selectRow"
    />
    <p
      class="dense-data-table-copy-status"
      aria-live="polite"
    >
      {{ copiedRowID ? `${copiedRowID} 完整值已复制` : "" }}
    </p>
  </section>
</template>

<style>
.dense-data-table-composition {
  min-width: 0;
  border-bottom: 1px solid var(--co-border-default);
  background: var(--co-bg-surface);
}

.dense-data-table-toolbar {
  display: flex;
  min-height: var(--co-control-height);
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-3);
  padding: var(--co-space-1) var(--co-space-3);
  border-bottom: 1px solid var(--co-border-default);
  color: var(--co-text-muted);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}

.dense-data-table-actions { display: flex; align-items: center; gap: var(--co-space-1); }
.dense-data-table-viewport {
  min-height: min(var(--co-table-viewport-min-height), calc(100dvh - 360px));
  max-height: min(var(--co-table-viewport-max-height), calc(100dvh - 300px));
  overflow: auto;
  overscroll-behavior: contain;
}

.dense-data-table-base { width: max-content; min-width: 100%; table-layout: fixed; }
.dense-data-table-header-cell {
  height: var(--co-control-height);
  padding: var(--co-space-2) var(--co-space-3);
  color: var(--co-text-secondary);
  background: var(--co-bg-subtle);
  font-size: 11px;
  font-weight: 700;
  white-space: nowrap;
}

.dense-data-table-row { height: var(--co-table-row-height); background: var(--co-bg-surface); }
.dense-data-table-row:hover > .dense-data-table-cell { background: var(--co-bg-hover); }
.dense-data-table-row[data-selected="true"] > .dense-data-table-cell { background: var(--co-bg-active); }
.dense-data-table-row > td:first-child { border-left: var(--co-severity-marker-width) solid transparent; }
.dense-data-table-row--critical > td:first-child { border-left-color: var(--co-status-critical-fg); }
.dense-data-table-row--warning > td:first-child { border-left-color: var(--co-status-warning-fg); }
.dense-data-table-row--info > td:first-child { border-left-color: var(--co-status-info-fg); }
.dense-data-table-row:focus-visible { outline: 2px solid var(--co-focus-ring); outline-offset: -2px; }
.dense-data-table-cell {
  height: var(--co-table-row-height);
  max-width: var(--co-table-cell-max-width);
  overflow: hidden;
  padding: var(--co-space-2) var(--co-space-3);
  border-bottom: 1px solid var(--co-border-subtle);
  color: var(--co-text-primary);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dense-data-table-header-cell[data-pinned] { background: var(--co-bg-subtle); }
.dense-data-table-cell[data-pinned] { background: var(--co-bg-surface); }

.dense-data-table-copy-cell { width: 56px; text-align: center; }
.dense-data-table-empty { height: 180px; color: var(--co-text-muted); }
.dense-data-table-copy-status {
  min-height: 22px;
  margin: 0;
  padding: var(--co-space-1) var(--co-space-3);
  color: var(--co-status-success-fg);
  font-size: 10px;
}

@media (max-width: 1024px) {
  .dense-data-table-viewport { max-height: min(var(--co-table-viewport-compact-max-height), calc(100dvh - 300px)); }
}
</style>
