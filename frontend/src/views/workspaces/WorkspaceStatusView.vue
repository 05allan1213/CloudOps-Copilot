<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { ArrowRight, CircleSlash2, RefreshCw } from "lucide-vue-next";
import { useRoute } from "vue-router";

import { isApiError } from "../../api/client";
import { getBootstrap, type BootstrapSnapshot, type ProviderHealth, type ProviderIdentity } from "../../api/platform";

const route = useRoute();
const bootstrap = ref<BootstrapSnapshot | null>(null);
const loading = ref(true);
const error = ref("");
let controller: AbortController | undefined;

const title = computed(() => (typeof route.meta.title === "string" ? route.meta.title : "工作区"));
const provider = computed(() => (typeof route.meta.provider === "string" ? route.meta.provider as ProviderIdentity : undefined));
const providerHealth = computed<ProviderHealth | undefined>(() => bootstrap.value?.provider_health.find((item) => item.provider === provider.value));
const capabilityAvailable = computed(() => {
  const workspace = typeof route.meta.workspace === "string" ? route.meta.workspace : "";
  return bootstrap.value?.capabilities.includes(workspace) ?? false;
});

async function loadStatus() {
  controller?.abort();
  controller = new AbortController();
  loading.value = true;
  error.value = "";
  try {
    bootstrap.value = await getBootstrap(controller.signal);
  } catch (reason) {
    if (controller.signal.aborted) return;
    error.value = isApiError(reason)
      ? `工作区状态读取失败（${reason.code || "REQUEST_FAILED"}）：${reason.message}`
      : "工作区状态读取失败，请检查本地 API。";
  } finally {
    if (!controller.signal.aborted) loading.value = false;
  }
}

onMounted(loadStatus);
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <article class="workspace-status">
    <header>
      <p class="eyebrow">CloudOps Workspace</p>
      <h1>{{ title }}</h1>
    </header>

    <section v-if="loading && !bootstrap" class="status-panel" role="status" aria-live="polite">
      正在读取数据面状态…
    </section>

    <section v-else-if="error" class="status-panel is-error" role="alert">
      <CircleSlash2 :size="28" aria-hidden="true" />
      <h2>工作区暂不可用</h2>
      <p>{{ error }}</p>
      <button type="button" @click="loadStatus"><RefreshCw :size="17" aria-hidden="true" />重试</button>
    </section>

    <template v-else-if="bootstrap">
      <section class="status-panel" :class="{ 'is-ready': capabilityAvailable }" aria-labelledby="workspace-state-title">
        <CircleSlash2 :size="28" aria-hidden="true" />
        <h2 id="workspace-state-title">{{ capabilityAvailable ? "数据面可用" : "当前没有可用查询" }}</h2>
        <p v-if="providerHealth">{{ providerHealth.detail }}</p>
        <p v-else>当前 Configuration Revision 未返回对应 Provider 状态。</p>
        <dl>
          <div><dt>Provider</dt><dd class="mono-text">{{ provider || "internal" }}</dd></div>
          <div><dt>状态</dt><dd>{{ providerHealth?.state || "not_configured" }}</dd></div>
          <div><dt>Cluster</dt><dd class="mono-text">{{ bootstrap.active_scope.cluster_id }}</dd></div>
          <div><dt>Environment</dt><dd class="mono-text">{{ bootstrap.active_scope.environment }}</dd></div>
          <div><dt>Namespace</dt><dd class="mono-text">{{ bootstrap.active_scope.namespaces.join(", ") }}</dd></div>
          <div><dt>Configuration Revision</dt><dd class="mono-text">#{{ bootstrap.active_revision.number }}</dd></div>
        </dl>
        <RouterLink to="/settings#providers">检查 Provider 配置<ArrowRight :size="17" aria-hidden="true" /></RouterLink>
      </section>
    </template>
  </article>
</template>

<style scoped>
.workspace-status { display: grid; gap: var(--co-space-6); width: min(100%, 1120px); margin: 0 auto; }
.workspace-status header { padding-block: var(--co-space-2); }
.eyebrow { margin: 0 0 var(--co-space-2); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 11px; font-weight: 750; text-transform: uppercase; }
.workspace-status h1 { margin: 0; font-size: 30px; }
.status-panel { display: grid; min-height: 420px; place-content: center; justify-items: center; gap: var(--co-space-3); padding: clamp(24px, 6vw, 72px); border-block: 1px solid var(--co-border-default); color: var(--co-text-muted); text-align: center; background: var(--co-bg-surface); }
.status-panel > svg { color: var(--co-status-neutral-fg); }
.status-panel h2, .status-panel p { margin: 0; }
.status-panel h2 { color: var(--co-text-primary); font-size: 20px; }
.status-panel p { max-width: 680px; overflow-wrap: anywhere; }
.status-panel dl { display: grid; width: min(100%, 780px); grid-template-columns: repeat(3, minmax(0, 1fr)); margin: var(--co-space-5) 0 0; border: 1px solid var(--co-border-default); text-align: left; }
.status-panel dl div { min-width: 0; padding: var(--co-space-3); border-right: 1px solid var(--co-border-default); border-bottom: 1px solid var(--co-border-default); }
.status-panel dt { font-size: 11px; }
.status-panel dd { margin: 4px 0 0; color: var(--co-text-primary); overflow-wrap: anywhere; font-size: 12px; font-weight: 700; }
.status-panel a, .status-panel button { display: inline-flex; min-height: 44px; align-items: center; gap: var(--co-space-2); margin-top: var(--co-space-3); padding: 0 var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-action-primary); background: var(--co-bg-surface); cursor: pointer; font-weight: 750; }
.status-panel a:hover, .status-panel button:hover { border-color: var(--co-border-strong); color: var(--co-action-hover); background: var(--co-bg-hover); }
.status-panel.is-error { border-color: var(--co-status-critical-border); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.status-panel.is-error > svg { color: var(--co-status-critical-fg); }
.status-panel.is-ready > svg { color: var(--co-status-success-fg); }
@media (max-width: 767px) { .workspace-status h1 { font-size: 25px; } .status-panel { min-height: 360px; } .status-panel dl { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 380px) { .status-panel dl { grid-template-columns: 1fr; } }
</style>
