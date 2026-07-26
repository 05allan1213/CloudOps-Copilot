<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { ArrowRight, Database, RefreshCw, Server, ShieldCheck } from "lucide-vue-next";

import { isApiError } from "../../api/client";
import { getOverview, type OverviewSnapshot, type ProviderState } from "../../api/platform";

const snapshot = ref<OverviewSnapshot | null>(null);
const loading = ref(true);
const error = ref("");
let controller: AbortController | undefined;

const dateFormatter = new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium", timeStyle: "medium" });
const numberFormatter = new Intl.NumberFormat("zh-CN");
const providerLabels: Record<string, string> = {
  mysql: "MySQL",
  kubernetes: "Kubernetes",
  prometheus: "Prometheus",
  alertmanager: "Alertmanager",
  elasticsearch: "Elasticsearch",
  tempo: "Tempo",
  llm: "LLM",
  github: "GitHub",
  argocd: "Argo CD",
};

const availableProviders = computed(() => snapshot.value?.bootstrap.provider_health.filter((item) => item.state === "available").length ?? 0);
const degradedProviders = computed(() => snapshot.value?.bootstrap.provider_health.filter((item) => item.state === "partial" || item.state === "unavailable").length ?? 0);
const kubernetesState = computed(() => snapshot.value?.bootstrap.provider_health.find((item) => item.provider === "kubernetes")?.state ?? "not_configured");

function stateLabel(state: ProviderState): string {
  return ({
    available: "可用",
    partial: "部分可用",
    unavailable: "不可用",
    disabled: "已停用",
    not_configured: "未配置",
  } satisfies Record<ProviderState, string>)[state];
}

function formatTime(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : dateFormatter.format(date);
}

async function loadOverview() {
  controller?.abort();
  controller = new AbortController();
  loading.value = true;
  error.value = "";
  try {
    snapshot.value = await getOverview(controller.signal);
  } catch (reason) {
    if (controller.signal.aborted) return;
    error.value = isApiError(reason)
      ? `总览读取失败（${reason.code || "REQUEST_FAILED"}）：${reason.message}`
      : "总览读取失败，请检查本地 API。";
  } finally {
    if (!controller.signal.aborted) loading.value = false;
  }
}

onMounted(loadOverview);
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <article class="overview-view">
    <header class="page-heading">
      <div>
        <p class="eyebrow">Observe · Detect · Investigate · Decide · Act · Verify</p>
        <h1>运行总览</h1>
        <p>当前活动 Scope、配置边界与 Provider 状态。</p>
      </div>
      <button type="button" :disabled="loading" @click="loadOverview">
        <RefreshCw :size="18" aria-hidden="true" />
        {{ loading ? "刷新中…" : "刷新" }}
      </button>
    </header>

    <div v-if="error" class="state-band is-error" role="alert">
      <strong>总览暂不可用</strong>
      <span>{{ error }}</span>
      <button type="button" @click="loadOverview">重试</button>
    </div>
    <div v-else-if="loading && !snapshot" class="state-band" role="status" aria-live="polite">正在读取运行状态…</div>

    <template v-else-if="snapshot">
      <section class="summary-band" aria-labelledby="scope-title">
        <div class="scope-identity">
          <span class="section-icon" aria-hidden="true"><Server :size="21" /></span>
          <div>
            <p id="scope-title">活动 Operational Scope</p>
            <strong>{{ snapshot.bootstrap.active_scope.name }}</strong>
            <span class="mono-text">{{ snapshot.bootstrap.active_scope.cluster_id }} / {{ snapshot.bootstrap.active_scope.environment }}</span>
          </div>
        </div>
        <dl>
          <div><dt>Namespace</dt><dd>{{ snapshot.bootstrap.active_scope.namespaces.join(", ") }}</dd></div>
          <div><dt>配置 Revision</dt><dd class="mono-text">#{{ snapshot.bootstrap.active_revision.number }}</dd></div>
          <div><dt>Provider 可用</dt><dd>{{ numberFormatter.format(availableProviders) }} / {{ numberFormatter.format(snapshot.bootstrap.provider_health.length) }}</dd></div>
          <div><dt>需关注</dt><dd>{{ numberFormatter.format(degradedProviders) }}</dd></div>
          <div><dt>未读通知</dt><dd>{{ numberFormatter.format(snapshot.unread_notifications) }}</dd></div>
          <div><dt>采集时间</dt><dd><time :datetime="snapshot.bootstrap.collected_at">{{ formatTime(snapshot.bootstrap.collected_at) }}</time></dd></div>
        </dl>
      </section>

      <section class="atlas-band" aria-labelledby="atlas-title">
        <div class="atlas-copy">
          <p class="eyebrow">Operations Atlas</p>
          <h2 id="atlas-title">集群结构状态</h2>
          <p v-if="kubernetesState === 'available'">Kubernetes Provider 已连接，结构化资源投影可从基础设施工作区继续。</p>
          <p v-else>当前没有可用的 Kubernetes 结构投影；界面不会使用 fixture 填充 Live Mode。</p>
          <RouterLink to="/infrastructure">打开基础设施<ArrowRight :size="17" aria-hidden="true" /></RouterLink>
        </div>
        <div class="atlas-status" :class="`is-${kubernetesState}`">
          <Server :size="34" aria-hidden="true" />
          <strong>{{ stateLabel(kubernetesState) }}</strong>
          <span>Kubernetes</span>
        </div>
      </section>

      <section class="provider-section" aria-labelledby="provider-title">
        <header>
          <div>
            <p class="eyebrow">Provider Health</p>
            <h2 id="provider-title">数据源状态</h2>
          </div>
          <RouterLink to="/settings#providers">管理 Provider<ArrowRight :size="16" aria-hidden="true" /></RouterLink>
        </header>
        <ul class="provider-grid">
          <li v-for="provider in snapshot.bootstrap.provider_health" :key="provider.provider">
            <div class="provider-name">
              <Database v-if="provider.provider === 'mysql'" :size="18" aria-hidden="true" />
              <ShieldCheck v-else :size="18" aria-hidden="true" />
              <strong>{{ providerLabels[provider.provider] || provider.provider }}</strong>
            </div>
            <span class="state-chip" :class="`is-${provider.state}`">{{ stateLabel(provider.state) }}</span>
            <p>{{ provider.detail }}</p>
            <time v-if="provider.checked_at" :datetime="provider.checked_at">{{ formatTime(provider.checked_at) }}</time>
          </li>
        </ul>
      </section>
    </template>
  </article>
</template>

<style scoped>
.overview-view { display: grid; gap: var(--co-space-6); width: min(100%, var(--co-content-max-width)); margin: 0 auto; }
.page-heading, .provider-section > header { display: flex; align-items: flex-end; justify-content: space-between; gap: var(--co-space-4); }
.page-heading h1, .provider-section h2, .atlas-copy h2 { margin: 0; }
.page-heading h1 { font-size: 30px; }
.page-heading p:not(.eyebrow), .atlas-copy p:not(.eyebrow) { max-width: 720px; margin: var(--co-space-2) 0 0; color: var(--co-text-secondary); }
.eyebrow { margin: 0 0 var(--co-space-2); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 11px; font-weight: 750; text-transform: uppercase; }
.page-heading button, .state-band button { display: inline-flex; min-height: 42px; align-items: center; gap: var(--co-space-2); padding: 0 var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-primary); background: var(--co-bg-surface); cursor: pointer; font-weight: 700; }
.page-heading button:hover, .state-band button:hover { border-color: var(--co-border-strong); background: var(--co-bg-hover); }
button:disabled { cursor: wait; opacity: 0.6; }
.state-band { display: flex; min-height: 120px; align-items: center; justify-content: center; gap: var(--co-space-3); padding: var(--co-space-5); border-block: 1px solid var(--co-border-default); color: var(--co-text-muted); }
.state-band.is-error { align-items: flex-start; flex-direction: column; border: 1px solid var(--co-status-critical-border); border-radius: var(--co-radius-panel); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.summary-band { display: grid; grid-template-columns: minmax(260px, 0.8fr) minmax(0, 2fr); border-block: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.scope-identity { display: flex; min-width: 0; align-items: flex-start; gap: var(--co-space-3); padding: var(--co-space-5); border-right: 1px solid var(--co-border-default); }
.section-icon { display: grid; width: 38px; height: 38px; flex: 0 0 38px; place-items: center; border: 1px solid var(--co-status-info-border); border-radius: var(--co-radius-control); color: var(--co-status-info-fg); background: var(--co-status-info-bg); }
.scope-identity div { display: grid; min-width: 0; gap: 2px; }
.scope-identity p, .scope-identity span { margin: 0; color: var(--co-text-muted); font-size: 12px; }
.scope-identity strong, .scope-identity span { overflow-wrap: anywhere; }
.summary-band dl { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); margin: 0; }
.summary-band dl div { min-width: 0; padding: var(--co-space-4); border-right: 1px solid var(--co-border-default); border-bottom: 1px solid var(--co-border-default); }
.summary-band dt { color: var(--co-text-muted); font-size: 11px; }
.summary-band dd { margin: var(--co-space-1) 0 0; color: var(--co-text-primary); overflow-wrap: anywhere; font-size: 13px; font-weight: 750; }
.atlas-band { display: grid; min-height: 280px; grid-template-columns: minmax(0, 1fr) minmax(220px, 34%); overflow: hidden; border-block: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.atlas-copy { align-self: center; padding: clamp(24px, 5vw, 64px); }
.atlas-copy h2 { font-size: 26px; }
.atlas-copy a, .provider-section header a { display: inline-flex; min-height: 44px; align-items: center; gap: var(--co-space-2); margin-top: var(--co-space-4); color: var(--co-action-primary); font-weight: 750; }
.atlas-copy a:hover, .provider-section header a:hover { color: var(--co-action-hover); text-decoration: underline; }
.atlas-status { display: grid; place-content: center; justify-items: center; gap: var(--co-space-2); border-left: 1px solid var(--co-border-default); color: var(--co-status-neutral-fg); background: var(--co-status-neutral-bg); }
.atlas-status strong { font-size: 18px; }
.atlas-status span { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 12px; }
.atlas-status.is-available { color: var(--co-status-success-fg); background: var(--co-status-success-bg); }
.atlas-status.is-partial, .atlas-status.is-unavailable { color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }
.provider-section { display: grid; gap: var(--co-space-4); }
.provider-section h2 { font-size: 20px; }
.provider-section header a { margin: 0; font-size: 13px; }
.provider-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--co-space-3); margin: 0; padding: 0; list-style: none; }
.provider-grid li { display: grid; min-width: 0; grid-template-columns: minmax(0, 1fr) auto; gap: var(--co-space-2); padding: var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); background: var(--co-bg-surface); content-visibility: auto; contain-intrinsic-size: 150px; }
.provider-name { display: flex; min-width: 0; align-items: center; gap: var(--co-space-2); }
.provider-name strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.provider-grid p { grid-column: 1 / -1; min-height: 42px; margin: 0; color: var(--co-text-secondary); overflow-wrap: anywhere; font-size: 12px; }
.provider-grid time { grid-column: 1 / -1; color: var(--co-text-muted); font-size: 11px; font-variant-numeric: tabular-nums; }
.state-chip { padding: 2px 7px; border: 1px solid var(--co-status-neutral-border); border-radius: var(--co-radius-pill); color: var(--co-status-neutral-fg); background: var(--co-status-neutral-bg); font-size: 10px; font-weight: 800; white-space: nowrap; }
.state-chip.is-available { border-color: var(--co-status-success-border); color: var(--co-status-success-fg); background: var(--co-status-success-bg); }
.state-chip.is-partial, .state-chip.is-unavailable { border-color: var(--co-status-warning-border); color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }
@media (max-width: 1050px) { .provider-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } .summary-band { grid-template-columns: 1fr; } .scope-identity { border-right: 0; border-bottom: 1px solid var(--co-border-default); } }
@media (max-width: 767px) { .page-heading { align-items: flex-start; flex-direction: column; } .page-heading h1 { font-size: 25px; } .summary-band dl { grid-template-columns: repeat(2, minmax(0, 1fr)); } .atlas-band { grid-template-columns: 1fr; } .atlas-status { min-height: 150px; border-top: 1px solid var(--co-border-default); border-left: 0; } .provider-grid { grid-template-columns: 1fr; } }
@media (max-width: 380px) { .summary-band dl { grid-template-columns: 1fr; } }
</style>
