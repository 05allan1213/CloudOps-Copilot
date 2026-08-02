<script setup lang="ts">
import { computed, ref } from "vue";
import type { RouteLocationRaw } from "vue-router";

import type {
  Consultation,
  TelemetryEvidence,
  TraceDetail,
  TraceSpan,
} from "../../api/telemetry";
import { traceSpanRawValue } from "../../models/telemetry";
import { safeExternalURL } from "../../models/workbench";
import TraceWaterfall from "./TraceWaterfall.vue";

const props = defineProps<{
  detail: TraceDetail;
  selectedIDs: Set<string>;
  inspectedSpan: TraceSpan | null;
  retainedEvidence: TelemetryEvidence[];
  consultation: Consultation | null;
  savingEvidence: boolean;
  freezing: boolean;
  canFreeze: boolean;
  searchStale: boolean;
  workloadLocation: RouteLocationRaw;
}>();

const emit = defineEmits<{
  back: [];
  toggle: [spanID: string];
  inspect: [span: TraceSpan];
  saveEvidence: [];
  openAgent: [];
  freeze: [];
}>();

const copiedValue = ref("");

const tempoLink = computed(() => externalLink(
  props.detail.links.find((item) => (
    item.provider === "tempo"
    && item.target === "external"
    && item.availability === "available"
  )),
));
const spanProviderLink = computed(() => externalLink(
  props.inspectedSpan?.links.find((item) => (
    item.target === "external" && item.availability === "available"
  )),
));
const detailStats = computed(() => ({
  services: new Set(props.detail.spans.map((span) => span.service).filter(Boolean)).size,
  errors: props.detail.spans.filter((span) => span.status === "error").length,
  critical: props.detail.spans.filter((span) => span.critical_path).length,
}));
const logsLocation = computed<RouteLocationRaw>(() => {
  const span = props.inspectedSpan;
  return {
    path: "/logs",
    query: {
      cluster: props.detail.scope.cluster_id,
      namespace: span?.resource.namespace || props.detail.resource.namespace,
      resource: span?.resource.id || props.detail.resource.id,
      trace_id: props.detail.trace_id,
      from: props.detail.time_range.from,
      to: props.detail.time_range.to,
    },
  };
});

function externalLink(link?: TraceDetail["links"][number]) {
  const href = safeExternalURL(link?.href);
  return link && href ? { label: link.label || link.provider || "Provider", href } : null;
}

function formatDuration(value: number): string {
  if (!Number.isFinite(value)) return "无";
  if (value < 1) return `${value.toFixed(3)} ms`;
  if (value < 1000) return `${value.toFixed(value < 10 ? 2 : 1)} ms`;
  return `${(value / 1000).toFixed(2)} s`;
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value < 1) return "0 B";
  if (value < 1024) return `${value} B`;
  return `${(value / 1024).toFixed(value < 10240 ? 1 : 0)} KiB`;
}

function exactUTC(value?: string): string {
  if (!value) return "无";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : parsed.toISOString();
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

async function copyValue(value: string, identity: string) {
  try {
    await navigator.clipboard.writeText(value);
  } catch {
    fallbackCopy(value);
  }
  copiedValue.value = identity;
  window.setTimeout(() => {
    if (copiedValue.value === identity) copiedValue.value = "";
  }, 1200);
}

function copyTraceID() {
  return copyValue(props.detail.trace_id, "trace");
}

function copySpan() {
  if (!props.inspectedSpan) return Promise.resolve();
  return copyValue(traceSpanRawValue(props.inspectedSpan), props.inspectedSpan.span_id);
}
</script>

<template>
  <section
    class="trace-detail"
    aria-labelledby="trace-detail-heading"
    data-testid="trace-detail"
  >
    <header class="trace-detail__heading">
      <div>
        <UButton
          color="neutral"
          variant="ghost"
          icon="i-lucide-arrow-left"
          label="返回"
          @click="emit('back')"
        />
        <span>Trace detail</span>
        <h2 id="trace-detail-heading">
          {{ detail.root_service }} · {{ detail.root_operation }}
        </h2>
        <div class="trace-detail__identity">
          <code>{{ detail.trace_id }}</code>
          <UTooltip text="复制 Trace ID">
            <UButton
              color="neutral"
              variant="ghost"
              :icon="copiedValue === 'trace' ? 'i-lucide-check' : 'i-lucide-copy'"
              square
              aria-label="复制 Trace ID"
              @click="copyTraceID"
            />
          </UTooltip>
        </div>
      </div>
      <div class="trace-detail__actions">
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-save"
          label="保存 Evidence"
          :disabled="selectedIDs.size === 0 || savingEvidence"
          :loading="savingEvidence"
          @click="emit('saveEvidence')"
        />
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-bot"
          label="关联 Agent"
          :disabled="!canFreeze"
          @click="emit('openAgent')"
        />
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-box"
          label="Workload"
          :to="workloadLocation"
        />
        <UButton
          v-if="tempoLink"
          color="neutral"
          variant="outline"
          icon="i-lucide-external-link"
          :label="tempoLink.label"
          :to="tempoLink.href"
          target="_blank"
          rel="noopener noreferrer"
          external
        />
      </div>
    </header>

    <section class="trace-detail__overview" aria-label="Trace 摘要">
      <div class="trace-detail__latency">
        <span aria-hidden="true">
          <UIcon name="i-lucide-gauge" />
        </span>
        <div>
          <small>端到端总耗时</small>
          <strong>{{ formatDuration(detail.duration_ms) }}</strong>
          <p>{{ detail.root_service }} → {{ detail.root_operation }}</p>
        </div>
      </div>
      <dl class="trace-detail__meta">
        <div>
          <dt><UIcon name="i-lucide-route" aria-hidden="true" />关键路径</dt>
          <dd>{{ detailStats.critical }}</dd>
          <small>{{ detail.spans.length }} 个 Span</small>
        </div>
        <div>
          <dt><UIcon name="i-lucide-circle-alert" aria-hidden="true" />错误 Span</dt>
          <dd :class="{ 'is-error': detailStats.errors > 0 }">{{ detailStats.errors }}</dd>
          <small>真实 Span status</small>
        </div>
        <div>
          <dt><UIcon name="i-lucide-boxes" aria-hidden="true" />参与服务</dt>
          <dd>{{ detailStats.services }}</dd>
          <small>{{ formatBytes(detail.response_bytes) }} response</small>
        </div>
      </dl>
      <div class="trace-detail__flags">
        <span>开始 {{ exactUTC(detail.start_time) }}</span>
        <UBadge v-if="detail.partial" color="warning" variant="soft" label="部分结果" />
        <UBadge v-if="detail.truncated" color="warning" variant="soft" label="已截断" />
        <UBadge v-if="searchStale" color="neutral" variant="soft" label="搜索已陈旧" />
      </div>
    </section>

    <WorkspaceState
      v-if="detail.partial"
      kind="partial"
      title="Tempo 仅返回部分 Trace"
      description="可用 Span 继续显示；Evidence 会保留当前 partial 与 truncated 事实。"
    />
    <WorkspaceState
      v-if="searchStale"
      kind="stale"
      title="Trace 搜索结果已陈旧"
      description="当前 Trace 仍可检查，但不声明它代表最新 Provider 状态。"
    />

    <div class="trace-detail__grid">
      <TraceWaterfall
        :detail="detail"
        :selected-i-ds="selectedIDs"
        :inspected-i-d="inspectedSpan?.span_id ?? ''"
        @toggle="emit('toggle', $event)"
        @inspect="emit('inspect', $event)"
      />

      <aside
        class="span-inspector"
        aria-labelledby="span-inspector-heading"
      >
        <header>
          <div>
            <span>Inspector</span>
            <h3 id="span-inspector-heading">
              Span 详情
            </h3>
          </div>
          <UBadge
            v-if="inspectedSpan?.critical_path"
            color="warning"
            variant="soft"
            label="Critical path"
          />
        </header>
        <WorkspaceState
          v-if="!inspectedSpan"
          kind="empty"
          title="尚未选择 Span"
          description="从瀑布中选择一个 Span 查看完整属性、Events 与上下文。"
        />
        <template v-else>
          <section class="span-inspector__summary">
            <span>{{ inspectedSpan.service }}</span>
            <strong>{{ inspectedSpan.name }}</strong>
            <dl>
              <div><dt>状态</dt><dd :class="{ 'is-error': inspectedSpan.status === 'error' }">{{ inspectedSpan.status }}</dd></div>
              <div><dt>耗时</dt><dd>{{ formatDuration(inspectedSpan.duration_ms) }}</dd></div>
              <div><dt>Events</dt><dd>{{ inspectedSpan.events.length }}</dd></div>
            </dl>
          </section>
          <div class="span-inspector__actions">
            <UButton
              color="neutral"
              :variant="selectedIDs.has(inspectedSpan.span_id) ? 'soft' : 'outline'"
              :icon="selectedIDs.has(inspectedSpan.span_id) ? 'i-lucide-check' : 'i-lucide-file-plus-2'"
              :label="selectedIDs.has(inspectedSpan.span_id) ? '已选为 Evidence' : '选为 Evidence'"
              @click="emit('toggle', inspectedSpan.span_id)"
            />
            <UButton
              color="neutral"
              variant="outline"
              :icon="copiedValue === inspectedSpan.span_id ? 'i-lucide-check' : 'i-lucide-copy'"
              label="复制完整 JSON"
              @click="copySpan"
            />
            <UButton
              color="neutral"
              variant="outline"
              icon="i-lucide-scroll-text"
              label="相关日志"
              :to="logsLocation"
            />
            <UButton
              v-if="spanProviderLink"
              color="neutral"
              variant="outline"
              icon="i-lucide-external-link"
              :label="spanProviderLink.label"
              :to="spanProviderLink.href"
              target="_blank"
              rel="noopener noreferrer"
              external
            />
          </div>

          <UCollapsible class="span-inspector__technical">
            <template #default="{ open }">
              <UButton
                color="neutral"
                variant="ghost"
                block
                icon="i-lucide-braces"
                label="完整 Span 属性"
                :trailing-icon="open ? 'i-lucide-chevron-up' : 'i-lucide-chevron-down'"
              />
            </template>
            <template #content>
              <div class="span-inspector__technical-content">
          <section aria-labelledby="span-identity-heading">
            <h4 id="span-identity-heading">
              Identity
            </h4>
            <dl class="span-inspector__fields">
              <div><dt>span_id</dt><dd>{{ inspectedSpan.span_id }}</dd></div>
              <div><dt>parent_span_id</dt><dd>{{ inspectedSpan.parent_span_id || "root" }}</dd></div>
              <div><dt>service</dt><dd>{{ inspectedSpan.service }}</dd></div>
              <div><dt>operation</dt><dd>{{ inspectedSpan.name }}</dd></div>
              <div><dt>kind</dt><dd>{{ inspectedSpan.kind || "unspecified" }}</dd></div>
              <div><dt>status</dt><dd>{{ inspectedSpan.status }}</dd></div>
              <div><dt>start_time UTC</dt><dd>{{ exactUTC(inspectedSpan.start_time) }}</dd></div>
              <div><dt>duration</dt><dd>{{ formatDuration(inspectedSpan.duration_ms) }}</dd></div>
              <div><dt>resource</dt><dd>{{ inspectedSpan.resource.id }}</dd></div>
            </dl>
          </section>

          <section aria-labelledby="span-tags-heading">
            <h4 id="span-tags-heading">
              Tags / attributes
            </h4>
            <WorkspaceState
              v-if="!Object.keys(inspectedSpan.attributes).length"
              kind="empty"
              title="无 Span attributes"
            />
            <dl
              v-else
              class="span-inspector__fields"
            >
              <div
                v-for="(value, key) in inspectedSpan.attributes"
                :key="key"
              >
                <dt>{{ key }}</dt><dd>{{ value }}</dd>
              </div>
            </dl>
          </section>

          <section aria-labelledby="span-events-heading">
            <h4 id="span-events-heading">
              Events / logs context
            </h4>
            <WorkspaceState
              v-if="!inspectedSpan.events.length"
              kind="empty"
              title="无 Span Events"
              description="可使用 Trace ID 打开同一时间范围的日志上下文。"
            />
            <ol
              v-else
              class="span-inspector__events"
            >
              <li
                v-for="event in inspectedSpan.events"
                :key="`${event.timestamp}:${event.name}`"
              >
                <time :datetime="event.timestamp">{{ exactUTC(event.timestamp) }}</time>
                <strong>{{ event.name }}</strong>
                <dl
                  v-if="Object.keys(event.attributes).length"
                  class="span-inspector__fields"
                >
                  <div
                    v-for="(value, key) in event.attributes"
                    :key="key"
                  >
                    <dt>{{ key }}</dt><dd>{{ value }}</dd>
                  </div>
                </dl>
              </li>
            </ol>
          </section>
              </div>
            </template>
          </UCollapsible>
        </template>
      </aside>
    </div>

    <section
      class="trace-snapshot"
      aria-labelledby="trace-context-heading"
    >
      <div>
        <UIcon
          name="i-lucide-archive"
          aria-hidden="true"
        />
        <div>
          <h3 id="trace-context-heading">
            冻结上下文
          </h3>
          <span>{{ retainedEvidence.length }} 条 Evidence · Trace execution {{ detail.query_id }}</span>
          <ul
            v-if="retainedEvidence.length"
            class="trace-evidence-list"
            aria-label="Trace Evidence identities"
          >
            <li
              v-for="evidence in retainedEvidence"
              :key="evidence.id"
            >
              <code>{{ evidence.id }}</code>
            </li>
          </ul>
        </div>
      </div>
      <UButton
        color="primary"
        icon="i-lucide-archive"
        label="创建 Snapshot"
        :disabled="!canFreeze || freezing"
        :loading="freezing"
        @click="emit('freeze')"
      />
    </section>
    <dl
      v-if="consultation"
      class="trace-snapshot__proof"
      data-testid="context-snapshot"
    >
      <div><dt>Consultation</dt><dd>{{ consultation.id }}</dd></div>
      <div><dt>Snapshot</dt><dd>{{ consultation.context_snapshot.id }}</dd></div>
      <div><dt>Content hash</dt><dd>{{ consultation.context_snapshot.content_hash }}</dd></div>
    </dl>
  </section>
</template>

<style scoped>
.trace-detail { min-width: 0; }
.trace-detail__heading { display: flex; min-width: 0; align-items: flex-start; justify-content: space-between; gap: var(--co-space-4); padding-bottom: var(--co-space-3); }
.trace-detail__heading > div:first-child { min-width: 0; }
.trace-detail__heading > div:first-child > span { display: block; margin-top: var(--co-space-2); color: var(--co-text-muted); font-size: 10px; font-weight: 750; text-transform: uppercase; }
.trace-detail__heading h2 { margin: 2px 0 0; overflow-wrap: anywhere; font-size: 18px; }
.trace-detail__identity { display: flex; min-width: 0; align-items: center; gap: var(--co-space-1); }
.trace-detail__identity code { min-width: 0; overflow-wrap: anywhere; color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 10px; }
.trace-detail__actions,
.span-inspector__actions { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: var(--co-space-2); }
.trace-detail__overview { display: grid; overflow: hidden; grid-template-columns: minmax(280px, 0.85fr) minmax(0, 1.15fr); gap: var(--co-space-3); padding: var(--co-space-4); border: 1px solid var(--co-border-subtle); border-radius: var(--co-radius-frame); background: color-mix(in srgb, var(--co-bg-surface) 72%, var(--co-bg-canvas)); box-shadow: var(--co-shadow-row); }
.trace-detail__latency { display: flex; min-width: 0; align-items: center; gap: var(--co-space-3); padding: var(--co-space-4); border-radius: var(--co-radius-panel); background: var(--co-bg-floating); }
.trace-detail__latency > span { display: grid; width: 52px; height: 52px; flex: 0 0 auto; place-items: center; border: 1px solid var(--co-status-success-border); border-radius: var(--co-radius-panel); color: var(--co-status-success-fg); background: var(--co-status-success-bg); font-size: 20px; }
.trace-detail__latency > div { display: grid; min-width: 0; gap: 2px; }
.trace-detail__latency small { color: var(--co-text-muted); font-size: 9px; font-weight: 750; }
.trace-detail__latency strong { font-family: var(--co-font-mono); font-size: 26px; line-height: 1.1; font-variant-numeric: tabular-nums; }
.trace-detail__latency p { margin: 0; overflow: hidden; color: var(--co-text-secondary); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.trace-detail__meta { display: grid; min-width: 0; grid-template-columns: repeat(3, minmax(0, 1fr)); margin: 0; align-items: stretch; gap: var(--co-space-2); }
.trace-detail__meta > div { display: grid; min-width: 0; align-content: center; padding: var(--co-space-3); border-radius: var(--co-radius-panel); background: var(--co-bg-floating); }
.trace-detail__meta dt { display: flex; align-items: center; gap: var(--co-space-1); color: var(--co-text-muted); font-size: 9px; font-weight: 750; }
.trace-detail__meta dd { margin: 3px 0 0; color: var(--co-text-primary); font-family: var(--co-font-mono); font-size: 20px; font-weight: 750; font-variant-numeric: tabular-nums; }
.trace-detail__meta dd.is-error { color: var(--co-status-critical-fg); }
.trace-detail__meta small { display: block; margin-top: 2px; overflow: hidden; color: var(--co-text-muted); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.trace-detail__flags { display: flex; min-width: 0; grid-column: 1 / -1; flex-wrap: wrap; align-items: center; gap: var(--co-space-2); padding: var(--co-space-2) var(--co-space-3); border-radius: var(--co-radius-control); color: var(--co-text-muted); background: color-mix(in srgb, var(--co-bg-canvas) 68%, transparent); font-family: var(--co-font-mono); font-size: 9px; }
.trace-detail__grid { display: grid; min-width: 0; grid-template-columns: minmax(0, 1.85fr) minmax(300px, 1fr); align-items: start; gap: var(--co-space-5); margin-top: var(--co-space-3); }
.span-inspector { min-width: 0; max-height: min(62vh, 640px); overflow-y: auto; padding: var(--co-space-2) var(--co-space-3) var(--co-space-3); border: 1px solid var(--co-border-subtle); border-radius: var(--co-radius-frame); background: color-mix(in srgb, var(--co-bg-surface) 84%, var(--co-bg-canvas)); box-shadow: var(--co-shadow-row); }
.span-inspector > header { display: flex; min-height: 46px; align-items: center; justify-content: space-between; gap: var(--co-space-2); }
.span-inspector header span { color: var(--co-text-muted); font-size: 10px; font-weight: 750; text-transform: uppercase; }
.span-inspector h3,
.span-inspector h4 { margin: 0; font-size: 13px; }
.span-inspector h4 { padding: var(--co-space-3) 0 var(--co-space-2); border-bottom: 1px solid var(--co-border-default); }
.span-inspector__summary { display: grid; gap: 2px; padding: var(--co-space-3); border-radius: var(--co-radius-panel); background: var(--co-bg-floating); }
.span-inspector__summary > span { color: var(--co-text-muted); font-size: 10px; }
.span-inspector__summary > strong { overflow-wrap: anywhere; font-size: 14px; }
.span-inspector__summary dl { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); margin: var(--co-space-2) 0 0; gap: var(--co-space-2); }
.span-inspector__summary dl div { min-width: 0; }
.span-inspector__summary dt { color: var(--co-text-muted); font-size: 9px; }
.span-inspector__summary dd { margin: 2px 0 0; font-family: var(--co-font-mono); font-size: 11px; }
.span-inspector__summary dd.is-error { color: var(--co-status-critical-fg); }
.span-inspector__actions { justify-content: flex-start; padding: var(--co-space-2) 0; }
.span-inspector__technical { overflow: hidden; border-radius: var(--co-radius-panel); background: color-mix(in srgb, var(--co-bg-canvas) 64%, transparent); }
.span-inspector__technical > :deep(button) { justify-content: flex-start; border-radius: var(--co-radius-panel); }
.span-inspector__technical-content { padding: 0 var(--co-space-3) var(--co-space-3); }
.span-inspector__fields { margin: 0; }
.span-inspector__fields div { display: grid; min-width: 0; grid-template-columns: minmax(110px, 0.7fr) minmax(0, 1fr); gap: var(--co-space-2); padding: var(--co-space-2) 0; border-bottom: 1px solid var(--co-border-subtle); }
.span-inspector__fields dt,
.span-inspector__fields dd { min-width: 0; overflow-wrap: anywhere; font-family: var(--co-font-mono); font-size: 10px; }
.span-inspector__fields dt { color: var(--co-text-muted); }
.span-inspector__fields dd { margin: 0; }
.span-inspector__events { display: grid; margin: 0; padding: 0; gap: var(--co-space-3); list-style: none; }
.span-inspector__events > li { min-width: 0; padding-bottom: var(--co-space-2); border-bottom: 1px solid var(--co-border-default); }
.span-inspector__events time,
.span-inspector__events strong { display: block; min-width: 0; overflow-wrap: anywhere; }
.span-inspector__events time { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 10px; }
.span-inspector__events strong { margin-top: var(--co-space-1); font-size: 11px; }
.trace-snapshot { display: flex; min-height: 72px; align-items: center; justify-content: space-between; gap: var(--co-space-4); margin-top: var(--co-space-4); padding: var(--co-space-3) var(--co-space-4); border: 1px solid var(--co-border-subtle); border-radius: var(--co-radius-frame); background: color-mix(in srgb, var(--co-bg-surface) 78%, var(--co-bg-canvas)); }
.trace-snapshot > div { display: flex; min-width: 0; align-items: center; gap: var(--co-space-2); }
.trace-snapshot h3 { margin: 0; font-size: 14px; }
.trace-snapshot span { color: var(--co-text-muted); font-size: 11px; }
.trace-evidence-list { display: grid; min-width: 0; margin: var(--co-space-1) 0 0; padding: 0; gap: 2px; list-style: none; }
.trace-evidence-list code { overflow-wrap: anywhere; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 10px; }
.trace-snapshot__proof { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); margin: var(--co-space-2) 0 0; overflow: hidden; border: 1px solid var(--co-border-subtle); border-radius: var(--co-radius-frame); background: var(--co-bg-surface); }
.trace-snapshot__proof div { min-width: 0; padding: var(--co-space-2); border-right: 1px solid var(--co-border-default); }
.trace-snapshot__proof div:last-child { border-right: 0; }
.trace-snapshot__proof dt { color: var(--co-text-muted); font-size: 10px; }
.trace-snapshot__proof dd { margin: var(--co-space-1) 0 0; overflow-wrap: anywhere; font-family: var(--co-font-mono); font-size: 10px; }

@media (max-width: 1180px) {
  .trace-detail__grid { grid-template-columns: minmax(0, 1fr) minmax(280px, 0.7fr); }
}

@media (max-width: 1024px) {
  .trace-detail__heading { flex-direction: column; }
  .trace-detail__actions { justify-content: flex-start; }
  .trace-detail__grid { grid-template-columns: minmax(0, 1fr); }
  .span-inspector { max-height: none; }
  .trace-detail__overview { grid-template-columns: minmax(0, 1fr); }
  .trace-detail__meta { grid-template-columns: repeat(3, minmax(0, 1fr)); padding: 0 var(--co-space-4) var(--co-space-3); }
}
</style>
