<script setup lang="ts">
import { computed, ref } from "vue";

type StatusColor = "error" | "warning" | "info" | "neutral" | "success";
type StreamState = "connecting" | "live" | "reconnecting" | "disconnected" | "stale" | "cursor-expired" | "resyncing" | "resync-failed" | "torn-down";

interface ExceptionalState {
  id: string;
  label: string;
  title: string;
  summary: string;
  next: string;
  icon: string;
  color: StatusColor;
  code: string;
}

const states: ExceptionalState[] = [
  { id: "permission-denied", label: "Permission Denied", title: "无权读取当前 Evidence", summary: "身份可访问 Incident，但缺少 evidence.read；现有 Incident 上下文保持可见。", next: "请求 Scope 限定的只读权限，或返回 Incident。", icon: "i-lucide-shield-x", color: "error", code: "EVIDENCE_PERMISSION_DENIED" },
  { id: "partial", label: "Partial", title: "Provider 返回部分结果", summary: "Prometheus 成功，Tempo 超时，Kubernetes 当前投影可用；不合并成成功。", next: "保留可用结果并单独重试 Tempo。", icon: "i-lucide-split", color: "warning", code: "PROVIDER_PARTIAL" },
  { id: "stale", label: "Stale", title: "数据完整性已过期", summary: "最后连续游标早于 2026-07-30T07:48:12Z，页面停止显示 live。", next: "执行有界 resync；失败时继续标记 stale。", icon: "i-lucide-cloud-off", color: "neutral", code: "CURSOR_STALE" },
  { id: "disconnected", label: "Disconnected", title: "实时连接已断开", summary: "已加载内容保持原位，新事件不会移动当前阅读位置。", next: "使用退避策略重连，不清空筛选和选择。", icon: "i-lucide-unplug", color: "warning", code: "SSE_DISCONNECTED" },
  { id: "expired-authority", label: "Expired Authority", title: "执行授权已过期", summary: "授权 hash 与当前时间边界不再有效，所有危险操作 fail closed。", next: "重新获取当前 authority；旧 hash 不可复用。", icon: "i-lucide-key-round", color: "error", code: "AUTHORITY_EXPIRED" },
  { id: "hash-changed", label: "Hash Changed", title: "Exact hash 已变化", summary: "Owner 接受的是 43c5…e6ab8，当前候选为 7ea2…9d410。", next: "重新审查差异并明确接受新 hash。", icon: "i-lucide-git-compare-arrows", color: "error", code: "EXACT_HASH_CHANGED" },
  { id: "provider-disagreement", label: "Provider Disagreement", title: "Provider 观察不一致", summary: "Kubernetes 显示 3/3 current，Prometheus 仍显示错误率高于恢复阈值。", next: "保留两侧事实；不能推断恢复。", icon: "i-lucide-scale", color: "warning", code: "PROVIDER_DISAGREEMENT" },
  { id: "accepted-not-observed", label: "Accepted, not observed", title: "已接受但尚未观察", summary: "Owner 已接受精确 hash，Provider 尚未报告派发后的当前状态。", next: "等待 current Evidence；不得显示 verified success。", icon: "i-lucide-file-clock", color: "info", code: "ACCEPTED_PENDING_OBSERVATION" },
  { id: "observed-not-verified", label: "Observed, not verified", title: "已观察但尚未验证", summary: "Provider 观察到 Revision 13，恢复指标的验证窗口尚未闭合。", next: "收集当前 Verification 与 Evidence。", icon: "i-lucide-scan-eye", color: "warning", code: "OBSERVED_PENDING_VERIFICATION" },
  { id: "verification-failed", label: "Verification Failed", title: "恢复验证失败", summary: "错误率在验证窗口再次超过 5%，当前 Evidence 不支持恢复成功。", next: "保持 Incident open，返回调查并保留失败证据。", icon: "i-lucide-badge-x", color: "error", code: "VERIFICATION_FAILED" },
];

const selectedId = ref(states[0].id);
const selected = computed(() => states.find((state) => state.id === selectedId.value) ?? states[0]);
const streamState = ref<StreamState>("connecting");
const cursor = ref(1042);
const acceptedEvents = ref(18);
const duplicateEvents = ref(0);
const streamLog = ref<string[]>(["2026-07-30T07:49:00Z connecting cursor=1042"]);
const streamSequence = ref(0);

const streamMeta: Record<StreamState, { label: string; icon: string; color: StatusColor; claim: string }> = {
  connecting: { label: "Connecting", icon: "i-lucide-loader-circle", color: "info", claim: "正在建立连接" },
  live: { label: "Live", icon: "i-lucide-radio", color: "success", claim: "游标连续，当前可声明 live" },
  reconnecting: { label: "Reconnecting", icon: "i-lucide-refresh-cw", color: "warning", claim: "现有内容可读，新事件暂缓" },
  disconnected: { label: "Disconnected", icon: "i-lucide-unplug", color: "warning", claim: "不声明 live" },
  stale: { label: "Stale", icon: "i-lucide-cloud-off", color: "neutral", claim: "连续性不可信" },
  "cursor-expired": { label: "Cursor expired", icon: "i-lucide-history", color: "error", claim: "旧游标不可继续" },
  resyncing: { label: "Resyncing", icon: "i-lucide-list-restart", color: "info", claim: "后台核对完整窗口" },
  "resync-failed": { label: "Resync failed", icon: "i-lucide-circle-x", color: "error", claim: "保持 stale，不伪造连续" },
  "torn-down": { label: "Torn down", icon: "i-lucide-power", color: "neutral", claim: "连接与监听器已清理" },
};

function transition(next: StreamState, message: string) {
  streamSequence.value += 1;
  streamState.value = next;
  const observedAt = new Date(Date.parse("2026-07-30T07:49:00Z") + streamSequence.value * 1_000).toISOString();
  streamLog.value = [`${observedAt} ${message}`, ...streamLog.value].slice(0, 6);
}

function connectLive() {
  cursor.value += 1;
  acceptedEvents.value += 1;
  transition("live", `live cursor=${cursor.value}`);
}

function injectDuplicate() {
  duplicateEvents.value += 1;
  transition(streamState.value, `duplicate cursor=${cursor.value} ignored`);
}

function expireCursor() {
  transition("cursor-expired", `cursor=${cursor.value} expired`);
}

function resync(success: boolean) {
  transition("resyncing", "bounded resync started");
  window.setTimeout(() => {
    if (success) {
      cursor.value += 64;
      transition("live", `resync success cursor=${cursor.value}`);
    } else transition("resync-failed", "resync failed; live claim withheld");
  }, 120);
}
</script>

<template>
  <section class="workspace state-lab" aria-labelledby="state-title">
    <header class="workspace-header">
      <div class="workspace-title">
        <h1 id="state-title" tabindex="-1">异常状态与实时生命周期</h1>
        <p>领域状态保持各自语义；展示层不把失败、等待或观察伪装成 Verified。</p>
      </div>
      <div class="workspace-actions"><UBadge color="neutral" variant="subtle" icon="i-lucide-shapes" label="10 domain states" /></div>
    </header>

    <section class="state-catalog" aria-labelledby="domain-state-title">
      <nav class="state-index" aria-label="异常状态">
        <UButton
          v-for="item in states"
          :key="item.id"
          :color="selectedId === item.id ? item.color : 'neutral'"
          :variant="selectedId === item.id ? 'soft' : 'ghost'"
          :icon="item.icon"
          :label="item.label"
          block
          :aria-current="selectedId === item.id ? 'true' : undefined"
          @click="selectedId = item.id"
        />
      </nav>
      <article class="state-detail" :data-testid="`state-${selected.id}`">
        <div class="state-detail-heading">
          <div class="state-icon" :class="`is-${selected.color}`"><UIcon :name="selected.icon" aria-hidden="true" /></div>
          <div><span>{{ selected.code }}</span><h2 id="domain-state-title">{{ selected.title }}</h2></div>
        </div>
        <p>{{ selected.summary }}</p>
        <dl>
          <div class="data-pair"><dt>Request ID</dt><dd class="mono">req-8d9112caa410</dd></div>
          <div class="data-pair"><dt>Trace ID</dt><dd class="mono">4d5f196cf4c548b78acc66e80b915c91</dd></div>
          <div class="data-pair"><dt>Observed UTC</dt><dd class="mono">2026-07-30T07:49:12Z</dd></div>
          <div class="data-pair"><dt>恢复路径</dt><dd>{{ selected.next }}</dd></div>
        </dl>
        <div class="state-grid" aria-label="领域执行状态">
          <div class="state-step"><strong><UIcon name="i-lucide-file-check-2" />Accepted</strong><small>已锁定输入</small></div>
          <div class="state-step"><strong><UIcon name="i-lucide-send" />Dispatched</strong><small>请求已派发</small></div>
          <div class="state-step is-current"><strong><UIcon name="i-lucide-scan-eye" />Observed</strong><small>当前事实</small></div>
          <div class="state-step"><strong><UIcon name="i-lucide-badge-check" />Verified</strong><small>独立验证</small></div>
        </div>
      </article>
    </section>

    <section class="stream-lab" aria-labelledby="stream-title">
      <div class="stream-heading">
        <div><span>SSE lifecycle fixture</span><h2 id="stream-title">Incident 实时连接</h2></div>
        <UBadge :color="streamMeta[streamState].color" :icon="streamMeta[streamState].icon" :label="streamMeta[streamState].label" data-testid="sse-state" />
      </div>
      <div class="stream-status" aria-live="polite">
        <UIcon :name="streamMeta[streamState].icon" :class="{ spinning: streamState === 'connecting' || streamState === 'reconnecting' || streamState === 'resyncing' }" aria-hidden="true" />
        <div><strong>{{ streamMeta[streamState].claim }}</strong><span>cursor {{ cursor }} · accepted {{ acceptedEvents }} · duplicates ignored {{ duplicateEvents }}</span></div>
      </div>
      <div class="stream-actions" role="group" aria-label="SSE 故障注入">
        <UButton color="success" variant="outline" icon="i-lucide-radio" label="Live" data-testid="sse-live" @click="connectLive" />
        <UButton color="warning" variant="outline" icon="i-lucide-refresh-cw" label="Reconnecting" @click="transition('reconnecting', 'connection retry scheduled')" />
        <UButton color="warning" variant="outline" icon="i-lucide-unplug" label="Disconnect" @click="transition('disconnected', 'transport disconnected')" />
        <UButton color="neutral" variant="outline" icon="i-lucide-cloud-off" label="Stale" @click="transition('stale', 'cursor continuity is no longer trustworthy')" />
        <UButton color="neutral" variant="outline" icon="i-lucide-copy-x" label="Duplicate" data-testid="sse-duplicate" @click="injectDuplicate" />
        <UButton color="error" variant="outline" icon="i-lucide-history" label="Expire cursor" data-testid="sse-expire" @click="expireCursor" />
        <UButton color="info" variant="outline" icon="i-lucide-list-restart" label="Resync PASS" data-testid="sse-resync-pass" @click="resync(true)" />
        <UButton color="error" variant="outline" icon="i-lucide-circle-x" label="Resync FAIL" data-testid="sse-resync-fail" @click="resync(false)" />
        <UButton color="neutral" variant="ghost" icon="i-lucide-power" label="Teardown" data-testid="sse-teardown" @click="transition('torn-down', 'event source and listeners disposed')" />
      </div>
      <ol class="stream-log" aria-label="SSE 事件日志"><li v-for="line in streamLog" :key="line" class="mono">{{ line }}</li></ol>
    </section>
  </section>
</template>

<style scoped>
.state-lab { max-width: 1560px; margin: 0 auto; }
.state-catalog { display: grid; min-width: 0; grid-template-columns: 250px minmax(0, 1fr); border-block: 1px solid var(--co-border); background: var(--co-surface); }
.state-index { display: flex; min-width: 0; flex-direction: column; gap: 2px; padding: var(--co-space-3); border-right: 1px solid var(--co-border); }
.state-detail { min-width: 0; min-height: 410px; padding: var(--co-space-5); }.state-detail-heading { display: flex; min-width: 0; align-items: center; gap: var(--co-space-3); margin-bottom: var(--co-space-4); }.state-detail-heading span { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 9px; }.state-detail-heading h2 { margin: 2px 0 0; }.state-detail > p { max-width: 76ch; color: var(--co-text-secondary); line-height: 1.6; }
.state-icon { display: grid; width: 38px; height: 38px; flex: 0 0 38px; place-items: center; border: 1px solid currentColor; border-radius: 50%; }.state-icon.is-error { color: var(--co-critical-fg); background: var(--co-critical-bg); }.state-icon.is-warning { color: var(--co-warning-fg); background: var(--co-warning-bg); }.state-icon.is-info { color: var(--co-info-fg); background: var(--co-info-bg); }.state-icon.is-success { color: var(--co-success-fg); background: var(--co-success-bg); }.state-icon.is-neutral { color: var(--co-text-secondary); background: var(--co-surface-muted); }
.stream-lab { min-width: 0; margin-top: var(--co-space-5); border-block: 1px solid var(--co-border); background: var(--co-surface); }.stream-heading { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: var(--co-space-3); padding: var(--co-space-3); border-bottom: 1px solid var(--co-border); }.stream-heading span { color: var(--co-text-muted); font-size: 9px; text-transform: uppercase; }.stream-heading h2 { margin: 2px 0 0; }
.stream-status { display: grid; min-height: 78px; grid-template-columns: auto minmax(0, 1fr); align-items: center; gap: var(--co-space-3); padding: var(--co-space-4); }.stream-status > svg { width: 24px; height: 24px; color: var(--co-action); }.stream-status strong, .stream-status span { display: block; }.stream-status span { margin-top: 3px; color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 10px; }
.stream-actions { display: flex; min-width: 0; flex-wrap: wrap; gap: 6px; padding: var(--co-space-3); border-block: 1px solid var(--co-border); background: var(--co-surface-muted); }.stream-log { display: grid; min-height: 126px; margin: 0; padding: var(--co-space-3); list-style: none; }.stream-log li { padding: 3px 0; color: var(--co-text-secondary); font-size: 10px; }.spinning { animation: spin 0.8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 1100px) {
  .state-catalog { grid-template-columns: 1fr; }.state-index { flex-direction: row; overflow-x: auto; border-right: 0; border-bottom: 1px solid var(--co-border); }.state-index > * { flex: 0 0 auto; }.state-detail { min-height: 430px; padding: var(--co-space-4); }
}
</style>
