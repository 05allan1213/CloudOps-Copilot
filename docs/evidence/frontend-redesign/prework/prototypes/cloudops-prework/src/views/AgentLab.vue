<script setup lang="ts">
import { ref } from "vue";

const evidenceOpen = ref(false);
const contextOpen = ref(false);
const streamState = ref<"live" | "reconnecting" | "stale">("live");
const activeStep = ref(3);

const contextItems = [
  { label: "Incident", value: "inc-2026-07-01", icon: "i-lucide-siren" },
  { label: "Alert", value: "alt-error-rate-17", icon: "i-lucide-bell-ring" },
  { label: "Service", value: "cloudops-api", icon: "i-lucide-box" },
  { label: "Scope", value: "cloudops-local / demo", icon: "i-lucide-scan-search" },
  { label: "Time", value: "07:00–08:00 UTC", icon: "i-lucide-clock-3" },
  { label: "Evidence", value: "12 current", icon: "i-lucide-file-check-2" },
];

const steps = [
  { title: "约束调查范围", state: "complete", detail: "锁定 Incident、Service、Scope 与精确时间范围。", icon: "i-lucide-crosshair" },
  { title: "关联信号", state: "complete", detail: "Prometheus、Tempo 与 Kubernetes typed reader 形成同一时间窗。", icon: "i-lucide-link-2" },
  { title: "核对变更", state: "complete", detail: "Revision 13 在错误率上升前 4m32s 被 observed。", icon: "i-lucide-git-compare-arrows" },
  { title: "验证假设", state: "active", detail: "重试放大与连接池耗尽相关；等待当前 Trace 样本闭合。", icon: "i-lucide-flask-conical" },
  { title: "形成只读结论", state: "waiting", detail: "不创建 Approval 或执行入口。", icon: "i-lucide-file-clock" },
];

const evidence = [
  { id: "ev-043", source: "Prometheus", observed: "2026-07-30T07:44:10Z", summary: "5xx ratio 5.24%，连续 9m" },
  { id: "ev-044", source: "Tempo", observed: "2026-07-30T07:45:02Z", summary: "checkout -> mysql P95 481ms" },
  { id: "ev-045", source: "Kubernetes", observed: "2026-07-30T07:45:19Z", summary: "Revision 13 current，3/3 replicas" },
  { id: "ev-046", source: "Logs", observed: "2026-07-30T07:45:23Z", summary: "retry budget exhausted request_id=req-a1f03" },
];

function setStreamState(value: "live" | "reconnecting" | "stale") {
  streamState.value = value;
}
</script>

<template>
  <section class="workspace agent-lab" aria-labelledby="agent-title">
    <header class="workspace-header">
      <div class="workspace-title">
        <h1 id="agent-title" tabindex="-1">Agent 调查工作区</h1>
        <p>主调查优先，Scope 与 Evidence 上下文在所有支持桌面宽度保持可见。</p>
      </div>
      <div class="workspace-actions">
        <UBadge :color="streamState === 'live' ? 'success' : streamState === 'reconnecting' ? 'warning' : 'neutral'" :icon="streamState === 'live' ? 'i-lucide-radio' : streamState === 'reconnecting' ? 'i-lucide-refresh-cw' : 'i-lucide-cloud-off'" :label="streamState" data-testid="agent-stream-state" />
        <UTooltip text="调查上下文"><UButton class="compact-context-trigger" color="neutral" variant="outline" square icon="i-lucide-panel-left-open" aria-label="打开调查上下文" @click="contextOpen = true" /></UTooltip>
        <UTooltip text="Evidence"><UButton class="secondary-trigger" color="neutral" variant="outline" square icon="i-lucide-panel-right-open" aria-label="打开 Evidence" @click="evidenceOpen = true" /></UTooltip>
      </div>
    </header>

    <section class="agent-context-strip" aria-label="固定调查上下文" data-testid="agent-context-strip">
      <div v-for="item in contextItems" :key="item.label" class="context-item">
        <UIcon :name="item.icon" aria-hidden="true" /><span>{{ item.label }}</span><strong>{{ item.value }}</strong>
      </div>
    </section>

    <section class="agent-workspace" data-testid="agent-workspace">
      <aside class="agent-context-rail" aria-labelledby="context-rail-title" data-testid="agent-context-rail">
        <div class="rail-heading"><h2 id="context-rail-title">调查上下文</h2><UBadge color="info" variant="subtle" label="只读" /></div>
        <dl>
          <div v-for="item in contextItems" :key="item.label" class="data-pair"><dt>{{ item.label }}</dt><dd class="mono">{{ item.value }}</dd></div>
        </dl>
        <UAlert color="neutral" variant="subtle" icon="i-lucide-shield-check" title="Authority" description="当前调查无执行授权，不推断可操作状态。" />
      </aside>

      <section class="agent-investigation" aria-labelledby="investigation-title" data-testid="agent-main">
        <div class="investigation-heading">
          <div><span>Investigation run · inv-20260730-014</span><h2 id="investigation-title">API 错误率与数据库重试放大</h2></div>
          <div class="stream-controls" role="group" aria-label="Agent Stream 故障注入">
            <UTooltip text="Live"><UButton color="neutral" variant="ghost" square icon="i-lucide-radio" aria-label="设置 Agent Stream live" @click="setStreamState('live')" /></UTooltip>
            <UTooltip text="Reconnecting"><UButton color="neutral" variant="ghost" square icon="i-lucide-refresh-cw" aria-label="设置 Agent Stream reconnecting" @click="setStreamState('reconnecting')" /></UTooltip>
            <UTooltip text="Stale"><UButton color="neutral" variant="ghost" square icon="i-lucide-cloud-off" aria-label="设置 Agent Stream stale" @click="setStreamState('stale')" /></UTooltip>
          </div>
        </div>

        <UAlert v-if="streamState === 'reconnecting'" color="warning" variant="soft" icon="i-lucide-refresh-cw" title="正在重连" description="现有调查内容保持原位；新事件尚未并入。" />
        <UAlert v-else-if="streamState === 'stale'" color="neutral" variant="soft" icon="i-lucide-cloud-off" title="调查已 stale" description="无法保证游标连续性，停止声明 live；当前 Evidence 仍可检查。" />

        <ol class="investigation-steps" aria-label="调查进程">
          <li v-for="(step, index) in steps" :key="step.title" :class="[`is-${step.state}`, { 'is-selected': index === activeStep }]">
            <UButton color="neutral" variant="ghost" :aria-current="index === activeStep ? 'step' : undefined" @click="activeStep = index">
              <span class="step-marker"><UIcon :name="step.icon" aria-hidden="true" /></span>
              <span class="step-copy"><strong>{{ step.title }}</strong><small>{{ step.detail }}</small></span>
              <UBadge :color="step.state === 'complete' ? 'success' : step.state === 'active' ? 'info' : 'neutral'" variant="subtle" :label="step.state" />
            </UButton>
          </li>
        </ol>

        <section class="agent-finding" aria-labelledby="finding-title">
          <div class="section-heading"><h2 id="finding-title">当前只读发现</h2><span class="mono">2026-07-30T07:47:12Z</span></div>
          <p>错误率上升与连接池等待相关，但 Kubernetes Revision 当前一致；尚无 Verification 支持恢复结论。</p>
          <div class="finding-links"><UBadge color="warning" icon="i-lucide-eye" label="Observed, not verified" /><UBadge color="neutral" icon="i-lucide-file-check-2" label="12 Evidence" /></div>
        </section>
      </section>

      <aside class="agent-evidence-rail" aria-labelledby="evidence-rail-title" data-testid="agent-evidence-rail">
        <div class="rail-heading"><h2 id="evidence-rail-title">Evidence</h2><span>{{ evidence.length }}</span></div>
        <ol class="evidence-list">
          <li v-for="item in evidence" :key="item.id">
            <div><strong>{{ item.source }}</strong><code>{{ item.id }}</code></div>
            <p>{{ item.summary }}</p><time :datetime="item.observed">{{ item.observed }}</time>
          </li>
        </ol>
      </aside>
    </section>

    <USlideover v-model:open="evidenceOpen" title="Evidence" description="当前调查的 Provider-backed 证据" :ui="{ content: 'w-[min(520px,48vw)] max-w-none' }" data-testid="agent-evidence-slideover">
      <template #body><ol class="evidence-list"><li v-for="item in evidence" :key="item.id"><div><strong>{{ item.source }}</strong><code>{{ item.id }}</code></div><p>{{ item.summary }}</p><time :datetime="item.observed">{{ item.observed }}</time></li></ol></template>
    </USlideover>
    <USlideover v-model:open="contextOpen" side="left" title="调查上下文" description="Incident、Alert、Service、Scope、Time 与 Evidence" :ui="{ content: 'w-[min(440px,46vw)] max-w-none' }" data-testid="agent-context-slideover">
      <template #body><dl><div v-for="item in contextItems" :key="item.label" class="data-pair"><dt>{{ item.label }}</dt><dd class="mono">{{ item.value }}</dd></div></dl></template>
    </USlideover>
  </section>
</template>

<style scoped>
.agent-lab { max-width: 1760px; margin: 0 auto; }
.agent-context-strip { display: grid; min-width: 0; grid-template-columns: repeat(6, minmax(0, 1fr)); border-block: 1px solid var(--co-border); background: var(--co-surface); }
.context-item { display: grid; min-width: 0; grid-template-columns: auto minmax(0, 1fr); gap: 1px 6px; padding: 8px 10px; border-right: 1px solid var(--co-border); }
.context-item:last-child { border-right: 0; }.context-item > svg { grid-row: 1 / 3; align-self: center; color: var(--co-text-muted); }.context-item span { color: var(--co-text-muted); font-size: 9px; text-transform: uppercase; }.context-item strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--co-font-mono); font-size: 10px; }
.agent-workspace { display: grid; min-width: 0; min-height: 570px; grid-template-columns: minmax(210px, 0.72fr) minmax(480px, 2fr) minmax(260px, 0.9fr); border-bottom: 1px solid var(--co-border); background: var(--co-surface); }
.agent-context-rail, .agent-evidence-rail { min-width: 0; padding: var(--co-space-3); overflow: auto; background: var(--co-surface-muted); }.agent-context-rail { border-right: 1px solid var(--co-border); }.agent-evidence-rail { border-left: 1px solid var(--co-border); }
.rail-heading, .investigation-heading { display: flex; min-width: 0; align-items: flex-start; justify-content: space-between; gap: var(--co-space-2); margin-bottom: var(--co-space-3); }.rail-heading h2, .investigation-heading h2 { margin-bottom: 0; }.rail-heading span, .investigation-heading span { color: var(--co-text-muted); font-size: 10px; }
.agent-context-rail .data-pair { grid-template-columns: 72px minmax(0, 1fr); font-size: 10px; }
.agent-investigation { min-width: 0; padding: var(--co-space-4); overflow: hidden; }.stream-controls { display: flex; gap: 2px; }
.investigation-steps { display: grid; margin: var(--co-space-4) 0; padding: 0; list-style: none; }.investigation-steps li { position: relative; min-width: 0; }.investigation-steps li::before { position: absolute; z-index: 0; top: 34px; bottom: -8px; left: 17px; width: 1px; background: var(--co-border-strong); content: ""; }.investigation-steps li:last-child::before { display: none; }.investigation-steps button { position: relative; z-index: 1; display: grid; width: 100%; min-width: 0; grid-template-columns: 34px minmax(0, 1fr) auto; align-items: center; gap: var(--co-space-3); padding: 8px; border: 0; border-radius: var(--co-radius-control); color: inherit; background: transparent; text-align: left; }.investigation-steps button:hover, .investigation-steps .is-selected button { background: var(--co-selected); }.step-marker { display: grid; width: 26px; height: 26px; place-items: center; border: 1px solid var(--co-border-strong); border-radius: 50%; color: var(--co-text-muted); background: var(--co-surface); }.is-complete .step-marker { color: var(--co-success-fg); }.is-active .step-marker { color: var(--co-action); border-color: var(--co-action); }.step-copy { display: grid; min-width: 0; gap: 2px; }.step-copy strong { font-size: 12px; }.step-copy small { color: var(--co-text-secondary); overflow-wrap: anywhere; font-size: 10px; }
.agent-finding { padding: var(--co-space-3); border-left: 3px solid var(--co-warning-fg); background: var(--co-surface-muted); }.agent-finding p { margin-bottom: var(--co-space-3); color: var(--co-text-secondary); font-size: 12px; line-height: 1.6; }.finding-links { display: flex; flex-wrap: wrap; gap: 6px; }
.evidence-list { display: grid; margin: 0; padding: 0; list-style: none; }.evidence-list li { min-width: 0; padding: 10px 0; border-bottom: 1px solid var(--co-border); }.evidence-list li > div { display: flex; justify-content: space-between; gap: 8px; }.evidence-list strong { font-size: 11px; }.evidence-list code, .evidence-list time { color: var(--co-text-muted); font-size: 9px; }.evidence-list p { margin: 5px 0; color: var(--co-text-secondary); font-size: 10px; line-height: 1.45; }
.secondary-trigger, .compact-context-trigger { display: none; }
@media (max-width: 1500px) {
  .agent-workspace { grid-template-columns: minmax(210px, 0.72fr) minmax(0, 2fr); }
  .agent-evidence-rail { display: none; }.secondary-trigger { display: inline-flex; }
  .agent-context-strip { grid-template-columns: repeat(3, minmax(0, 1fr)); }.context-item:nth-child(3) { border-right: 0; }.context-item:nth-child(-n + 3) { border-bottom: 1px solid var(--co-border); }
}
@media (max-width: 1180px) {
  .agent-workspace { grid-template-columns: minmax(0, 1fr); }.agent-context-rail { display: none; }.compact-context-trigger { display: inline-flex; }
  .agent-investigation { padding: var(--co-space-3); }
}
@media (max-width: 1060px) {
  .agent-context-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); }.context-item { border-bottom: 1px solid var(--co-border); }.context-item:nth-child(even) { border-right: 0; }.context-item:nth-last-child(-n + 2) { border-bottom: 0; }
  .investigation-steps button { grid-template-columns: 30px minmax(0, 1fr); }.investigation-steps button > :last-child { grid-column: 2; justify-self: start; }
}
</style>
