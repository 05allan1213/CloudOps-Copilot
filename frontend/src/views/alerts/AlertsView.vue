<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { BellRing, Check, ChevronRight, FilterX, Link2, RefreshCw, Search, VolumeX } from "lucide-vue-next";
import { useRoute, useRouter } from "vue-router";

import { isApiError } from "../../api/client";
import {
  listAlerts,
  type AlertListQuery,
  type AlertSeverity,
  type AlertStatus,
  type AlertView,
} from "../../api/alerts";
import AlertBadges from "../../components/alerts/AlertBadges.vue";

const route = useRoute();
const router = useRouter();
const items = ref<AlertView[]>([]);
const nextCursor = ref("");
const loading = ref(true);
const loadingMore = ref(false);
const error = ref("");
let controller: AbortController | null = null;

const filters = reactive({
  status: queryValue(route.query.status) as AlertStatus | "",
  severity: queryValue(route.query.severity) as AlertSeverity | "",
  namespace: queryValue(route.query.namespace),
  search: queryValue(route.query.search),
  incident: queryValue(route.query.incident),
});

const firingCount = computed(() => items.value.filter((item) => item.status === "firing").length);
const acknowledgedCount = computed(() => items.value.filter((item) => item.acknowledgement).length);
const silencedCount = computed(() => items.value.filter((item) => item.silence?.status === "active").length);

function queryValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function formatTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", { dateStyle: "short", timeStyle: "medium" }).format(date);
}

function describeError(reason: unknown): string {
  if (!isApiError(reason)) return "Alert 列表读取失败。";
  return `${reason.code || "REQUEST_FAILED"}：${reason.message}`;
}

function currentQuery(cursor = ""): AlertListQuery {
  return {
    limit: 50,
    cursor: cursor || undefined,
    status: filters.status || undefined,
    severity: filters.severity || undefined,
    namespace: filters.namespace.trim() || undefined,
    search: filters.search.trim() || undefined,
    incident: filters.incident || undefined,
  };
}

async function load(reset = true) {
  controller?.abort();
  const requestController = new AbortController();
  controller = requestController;
  if (reset) loading.value = true;
  else loadingMore.value = true;
  error.value = "";
  try {
    const page = await listAlerts(currentQuery(reset ? "" : nextCursor.value), requestController.signal);
    if (controller !== requestController) return;
    items.value = reset ? page.items : [...items.value, ...page.items];
    nextCursor.value = page.next_cursor || "";
  } catch (reason) {
    if (requestController.signal.aborted || controller !== requestController) return;
    error.value = describeError(reason);
  } finally {
    if (controller === requestController) {
      loading.value = false;
      loadingMore.value = false;
    }
  }
}

async function applyFilters() {
  await router.replace({
    name: "alerts",
    query: {
      status: filters.status || undefined,
      severity: filters.severity || undefined,
      namespace: filters.namespace.trim() || undefined,
      search: filters.search.trim() || undefined,
      incident: filters.incident || undefined,
    },
  });
  await load(true);
}

async function clearFilters() {
  filters.status = "";
  filters.severity = "";
  filters.namespace = "";
  filters.search = "";
  await applyFilters();
}

function detailLocation(item: AlertView) {
  return {
    name: "alert-detail",
    params: { alertId: item.id },
    query: { cluster_id: item.cluster, namespace: item.namespace, incident: filters.incident || undefined },
  };
}

onMounted(() => void load(true));
watch(() => route.query.incident, (value) => {
  const next = queryValue(value);
  if (next === filters.incident) return;
  filters.incident = next;
  void load(true);
});
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <article class="alerts-view">
    <header class="page-heading">
      <div>
        <p class="eyebrow">CloudOps Alerts</p>
        <h1>告警</h1>
        <p>独立 Alert lifecycle 与精确 Incident 关联。</p>
      </div>
      <button type="button" class="icon-action" :disabled="loading" aria-label="刷新告警" title="刷新告警" @click="load(true)">
        <RefreshCw :size="18" aria-hidden="true" />
      </button>
    </header>

    <div v-if="filters.incident" class="incident-filter-band" role="status">
      <Link2 :size="16" aria-hidden="true" />
      <span>当前仅显示 Incident <code translate="no">{{ filters.incident }}</code> 的关联 Alert</span>
      <RouterLink :to="{ name: 'incident-detail', params: { incidentId: filters.incident } }">返回 Incident</RouterLink>
    </div>

    <dl class="summary-strip">
      <div><dt>当前结果</dt><dd>{{ items.length }}</dd></div>
      <div><dt>Firing</dt><dd class="is-critical">{{ firingCount }}</dd></div>
      <div><dt>已 acknowledge</dt><dd>{{ acknowledgedCount }}</dd></div>
      <div><dt>Active silence</dt><dd>{{ silencedCount }}</dd></div>
    </dl>

    <form class="filter-bar" aria-label="Alert filters" @submit.prevent="applyFilters">
      <label><span>状态</span><select v-model="filters.status" name="status" autocomplete="off"><option value="">全部</option><option value="firing">Firing</option><option value="resolved">Resolved</option></select></label>
      <label><span>Severity</span><select v-model="filters.severity" name="severity" autocomplete="off"><option value="">全部</option><option value="critical">Critical</option><option value="warning">Warning</option><option value="info">Info</option><option value="unknown">Unknown</option></select></label>
      <label><span>Namespace</span><input v-model="filters.namespace" name="namespace" type="text" autocomplete="off" spellcheck="false" placeholder="例如：demo…"></label>
      <label class="search-field"><span>搜索</span><input v-model="filters.search" name="search" type="search" autocomplete="off" placeholder="摘要、目标或服务…"></label>
      <button type="submit" class="primary-action"><Search :size="17" aria-hidden="true" />查询</button>
      <button type="button" class="icon-action" aria-label="清除筛选" title="清除筛选" @click="clearFilters"><FilterX :size="18" aria-hidden="true" /></button>
    </form>

    <div v-if="error" class="message-band is-error" role="alert">
      <strong>Alert API 不可用</strong><span>{{ error }}</span>
      <button type="button" class="secondary-action" @click="load(true)"><RefreshCw :size="17" aria-hidden="true" />重试</button>
    </div>
    <div v-else-if="loading" class="message-band" role="status" aria-live="polite">正在读取 Alert lifecycle…</div>
    <div v-else-if="items.length === 0" class="empty-state">
      <BellRing :size="28" aria-hidden="true" />
      <strong>没有匹配的 Alert</strong>
      <span>当前筛选条件下没有领域记录。</span>
    </div>

    <template v-else>
      <div class="alert-table" role="region" aria-label="Alert lifecycle 列表" tabindex="0">
        <table>
          <thead><tr><th>状态</th><th>Alert</th><th>Scope / Target</th><th>Facets</th><th>最近 Signal</th><th><span class="visually-hidden">打开</span></th></tr></thead>
          <tbody>
            <tr v-for="item in items" :key="item.id">
              <td><AlertBadges :status="item.status" :severity="item.severity" /></td>
              <td class="alert-copy"><RouterLink :to="detailLocation(item)">{{ item.summary }}</RouterLink><span>{{ item.category }} · {{ item.signal_count }} signals · recurrence {{ item.recurrence_count }}</span></td>
              <td><strong>{{ item.namespace }}/{{ item.target_name }}</strong><span>{{ item.cluster }} · {{ item.target_kind }} · {{ item.service_name }}</span></td>
              <td><span class="facets"><span v-if="item.acknowledgement" title="Acknowledged"><Check :size="15" aria-hidden="true" />Ack</span><span v-if="item.silence?.status === 'active'" title="Active silence"><VolumeX :size="15" aria-hidden="true" />Silenced</span><span v-if="item.incident_links.length"><Link2 :size="15" aria-hidden="true" />{{ item.incident_links.length }} Incident</span><span v-if="!item.acknowledgement && item.silence?.status !== 'active' && item.incident_links.length === 0">未处置</span></span></td>
              <td><time :datetime="item.last_seen_at">{{ formatTime(item.last_seen_at) }}</time><span class="mono-text">v{{ item.version }}</span></td>
              <td><RouterLink class="open-link" :to="detailLocation(item)" :aria-label="`打开 Alert ${item.summary}`"><ChevronRight :size="18" aria-hidden="true" /></RouterLink></td>
            </tr>
          </tbody>
        </table>
      </div>

      <ol class="alert-mobile-list">
        <li v-for="item in items" :key="`mobile-${item.id}`">
          <RouterLink :to="detailLocation(item)">
            <header><AlertBadges :status="item.status" :severity="item.severity" /><ChevronRight :size="18" aria-hidden="true" /></header>
            <strong>{{ item.summary }}</strong>
            <span>{{ item.namespace }}/{{ item.target_name }}</span>
            <footer><span>{{ item.signal_count }} signals</span><time :datetime="item.last_seen_at">{{ formatTime(item.last_seen_at) }}</time></footer>
          </RouterLink>
        </li>
      </ol>

      <button v-if="nextCursor" type="button" class="load-more" :disabled="loadingMore" @click="load(false)">{{ loadingMore ? "正在加载…" : "加载更多" }}</button>
    </template>
  </article>
</template>

<style scoped>
.alerts-view { display: grid; width: min(100%, var(--co-content-max-width)); min-width: 0; margin: 0 auto; gap: var(--co-space-5); }
.page-heading { display: flex; align-items: flex-end; justify-content: space-between; gap: var(--co-space-4); }
.page-heading h1 { margin: 0; font-size: 30px; }
.page-heading p:not(.eyebrow) { margin: var(--co-space-2) 0 0; color: var(--co-text-secondary); }
.incident-filter-band { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; gap: var(--co-space-2); padding: var(--co-space-3) var(--co-space-4); border-left: 3px solid var(--co-action-primary); color: var(--co-text-secondary); background: var(--co-bg-active); font-size: 12px; }
.incident-filter-band span { min-width: 0; overflow-wrap: anywhere; }
.incident-filter-band a { margin-left: auto; color: var(--co-action-primary); font-weight: 700; }
.incident-filter-band a:hover { text-decoration: underline; text-underline-offset: 3px; }
.incident-filter-band a:focus-visible { outline: 2px solid var(--co-action-primary); outline-offset: 2px; }
.eyebrow { margin: 0 0 var(--co-space-2); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 11px; font-weight: 750; text-transform: uppercase; }
.icon-action, .primary-action, .secondary-action, .load-more { display: inline-flex; min-height: 42px; align-items: center; justify-content: center; gap: var(--co-space-2); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); cursor: pointer; font-weight: 750; }
.icon-action { width: 42px; flex: 0 0 42px; padding: 0; color: var(--co-text-secondary); background: var(--co-bg-surface); }
.primary-action { padding: 0 var(--co-space-4); border-color: var(--co-action-primary); color: var(--co-text-on-action); background: var(--co-action-primary); }
.secondary-action, .load-more { padding: 0 var(--co-space-4); color: var(--co-text-primary); background: var(--co-bg-surface); }
button:hover { border-color: var(--co-border-strong); }
button:disabled { cursor: not-allowed; opacity: .55; }
.summary-strip { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); margin: 0; border-block: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.summary-strip div { padding: var(--co-space-3) var(--co-space-4); border-right: 1px solid var(--co-border-default); }
.summary-strip div:last-child { border-right: 0; }
.summary-strip dt { color: var(--co-text-muted); font-size: 11px; }
.summary-strip dd { margin: 2px 0 0; font-family: var(--co-font-mono); font-size: 18px; font-weight: 800; }
.summary-strip dd.is-critical { color: var(--co-status-critical-fg); }
.filter-bar { display: grid; grid-template-columns: 140px 150px minmax(150px, .8fr) minmax(220px, 1fr) auto auto; align-items: end; gap: var(--co-space-3); }
.filter-bar label { display: grid; min-width: 0; gap: 5px; color: var(--co-text-muted); font-size: 11px; font-weight: 700; }
.filter-bar input, .filter-bar select { width: 100%; min-width: 0; min-height: 42px; padding: 0 var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-primary); background: var(--co-bg-surface); }
.message-band { display: flex; min-width: 0; align-items: center; gap: var(--co-space-3); padding: var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); color: var(--co-text-secondary); background: var(--co-bg-surface); }
.message-band.is-error { flex-wrap: wrap; border-color: var(--co-status-critical-border); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.message-band.is-error .secondary-action { margin-left: auto; }
.empty-state { display: grid; min-height: 260px; place-content: center; justify-items: center; gap: var(--co-space-2); border-block: 1px solid var(--co-border-default); color: var(--co-text-muted); text-align: center; }
.empty-state strong { color: var(--co-text-primary); }
.alert-table { min-width: 0; overflow-x: auto; overscroll-behavior: contain; border-block: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
table { width: 100%; min-width: 1080px; border-collapse: collapse; font-size: 12px; }
th, td { padding: var(--co-space-3); border-bottom: 1px solid var(--co-border-default); text-align: left; vertical-align: middle; }
th { color: var(--co-text-muted); font-size: 10px; text-transform: uppercase; }
tbody tr:hover { background: var(--co-bg-hover); }
td > strong, td > span, td > time { display: block; }
td > span, td > time { margin-top: 3px; color: var(--co-text-muted); font-size: 11px; }
.alert-copy { max-width: 400px; }
.alert-copy a { display: block; color: var(--co-text-primary); font-weight: 750; }
.alert-copy a:hover { color: var(--co-action-primary); }
.facets { display: flex; max-width: 250px; flex-wrap: wrap; gap: var(--co-space-1); }
.facets > span { display: inline-flex; min-height: 25px; align-items: center; gap: 4px; padding: 0 6px; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-pill); color: var(--co-text-secondary); background: var(--co-bg-subtle); white-space: nowrap; }
.open-link { display: grid; width: 36px; height: 36px; place-items: center; border-radius: var(--co-radius-control); color: var(--co-action-primary); }
.open-link:hover { background: var(--co-bg-active); }
.load-more { justify-self: center; }
.alert-mobile-list { display: none; margin: 0; padding: 0; list-style: none; }
@media (max-width: 1100px) { .filter-bar { grid-template-columns: repeat(2, minmax(0, 1fr)); } .filter-bar .primary-action { min-width: 0; } }
@media (max-width: 767px) {
  .page-heading h1 { font-size: 25px; }
  .summary-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .summary-strip div:nth-child(2) { border-right: 0; }
  .summary-strip div:nth-child(-n+2) { border-bottom: 1px solid var(--co-border-default); }
  .filter-bar { grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); }
  .filter-bar label { font-size: 12px; }
  .filter-bar input, .filter-bar select { font-size: 16px; }
  .filter-bar .search-field { grid-column: 1 / -1; }
  .alert-table { display: none; }
  .alert-mobile-list { display: grid; gap: var(--co-space-2); }
  .alert-mobile-list li { border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); background: var(--co-bg-surface); }
  .alert-mobile-list a { display: grid; min-width: 0; gap: var(--co-space-2); padding: var(--co-space-4); }
  .alert-mobile-list header, .alert-mobile-list footer { display: flex; align-items: center; justify-content: space-between; gap: var(--co-space-2); }
  .alert-mobile-list strong { overflow-wrap: anywhere; }
  .alert-mobile-list > li > a > span, .alert-mobile-list footer { color: var(--co-text-muted); font-size: 11px; }
}
</style>
