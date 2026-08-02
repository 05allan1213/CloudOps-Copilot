<script setup lang="ts">
import { computed, markRaw, type Component } from "vue";
import {
	CircleCheck as CircleCheckFilled,
	CircleX as CircleCloseFilled,
	LoaderCircle as Loading,
	Network as Connection,
} from "lucide-vue-next";

import { humanizeCode } from "../../models/incidents";
import type { IncidentView } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import AttentionFlag from "./AttentionFlag.vue";
import IncidentStatusBadge from "./IncidentStatusBadge.vue";
import SeverityBadge from "./SeverityBadge.vue";

const props = defineProps<{
  incident: IncidentView;
  realtimeState: string;
  realtimeNotice?: string;
  refreshing?: boolean;
  lastUpdatedAt?: string;
}>();

const realtimeDefinition = computed<{ label: string; tone: string; icon: Component }>(() => {
  if (props.realtimeState === "connected") return { label: "实时", tone: "success", icon: markRaw(CircleCheckFilled) };
  if (props.realtimeState === "connecting" || props.realtimeState === "reconnecting") {
    return { label: props.realtimeState === "connecting" ? "连接中" : "重连中", tone: "warning", icon: markRaw(Loading) };
  }
  if (props.realtimeState === "disconnected") return { label: "已断开", tone: "neutral", icon: markRaw(CircleCloseFilled) };
  return { label: "实时状态", tone: "neutral", icon: markRaw(Connection) };
});

const projectionUpdatedAt = computed(() => props.lastUpdatedAt || props.incident.updated_at);
</script>

<template>
  <header class="incident-header">
    <div class="identity-row">
      <div class="identity-copy">
        <div class="identity-badges">
          <SeverityBadge :severity="incident.severity" />
          <IncidentStatusBadge :status="incident.status" />
          <AttentionFlag :active="incident.attention.required" />
        </div>
        <p class="incident-id">
          Incident <code translate="no">{{ incident.id }}</code>
        </p>
        <h1>{{ incident.summary || "Incident Cycle 进行中" }}</h1>
        <p class="identity-context">
          Cycle {{ incident.cycle }} · 版本 {{ incident.version }}
          <span v-if="incident.migrated_legacy_context"> · 迁移的 legacy context 仅用于审计</span>
        </p>
      </div>

      <div class="realtime-status">
        <span
          class="realtime-chip"
          :class="`realtime-chip--${realtimeDefinition.tone}`"
        >
          <component
            :is="realtimeDefinition.icon"
            :size="15"
            class="realtime-icon"
            aria-hidden="true"
          />
          {{ realtimeDefinition.label }}
        </span>
        <span
          v-if="refreshing"
          class="projection-refresh"
          role="status"
          aria-live="polite"
        >
          正在更新投影…
        </span>
      </div>
    </div>

    <dl class="header-facts">
      <div>
        <dt>当前阶段</dt>
        <dd>
          {{ incident.attention.stage }}<template v-if="incident.attention.required">
            · {{ humanizeCode(incident.attention.reason_code) }}
          </template>
        </dd>
      </div>
      <div>
        <dt>关联 Alert</dt>
        <dd>{{ incident.related_alert_count }}</dd>
      </div>
      <div>
        <dt>恢复状态</dt>
        <dd>{{ incident.recovery.state }} · {{ incident.recovery.verification_attempts }} 次尝试</dd>
      </div>
      <div>
        <dt>投影刷新</dt>
        <dd><time :datetime="projectionUpdatedAt">{{ formatIncidentTime(projectionUpdatedAt) }}</time></dd>
      </div>
    </dl>

    <p
      class="realtime-announcement visually-hidden"
      role="status"
      aria-live="polite"
    >
      {{ realtimeNotice }}
    </p>
  </header>
</template>

<style scoped>
.incident-header {
  display: grid;
  min-width: 0;
  gap: var(--co-space-5);
  padding: var(--co-space-5) 0;
  border-bottom: 1px solid var(--co-border-default);
}

.identity-row,
.identity-badges,
.realtime-status {
  display: flex;
  min-width: 0;
}

.identity-row {
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--co-space-6);
}

.identity-copy { min-width: 0; }

.identity-badges {
  flex-wrap: wrap;
  align-items: center;
  gap: var(--co-space-2);
  margin-bottom: var(--co-space-3);
}

.incident-id,
.identity-context,
.incident-header h1 {
  margin: 0;
}

.incident-id {
  color: var(--co-text-muted);
  font-size: 12px;
  overflow-wrap: anywhere;
}

.incident-header h1 {
  max-width: 42ch;
  margin-top: var(--co-space-1);
  color: var(--co-text-primary);
  font-size: 24px;
  line-height: 1.25;
  overflow-wrap: anywhere;
}

.identity-context {
  margin-top: var(--co-space-2);
  color: var(--co-text-secondary);
  font-size: 13px;
}

.realtime-status {
  flex: 0 0 auto;
  align-items: flex-end;
  flex-direction: column;
  gap: var(--co-space-2);
}

.realtime-chip {
  display: inline-flex;
  min-height: 28px;
  align-items: center;
  gap: 6px;
  padding: 3px 9px;
  border: 1px solid var(--co-status-neutral-border);
  border-radius: var(--co-radius-pill);
  color: var(--co-status-neutral-fg);
  background: var(--co-status-neutral-bg);
  font-size: 12px;
  font-weight: 700;
}

.realtime-chip--success { border-color: var(--co-status-success-border); color: var(--co-status-success-fg); background: var(--co-status-success-bg); }
.realtime-chip--warning { border-color: var(--co-status-warning-border); color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }

.realtime-chip--warning .realtime-icon {
  animation: realtime-rotation var(--co-spinner-duration) linear infinite;
}

.projection-refresh {
  color: var(--co-action-primary);
  font-size: 12px;
}

.header-facts {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  margin: 0;
  overflow: hidden;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-frame);
  background: var(--co-bg-surface);
}

.header-facts div {
  min-width: 0;
  padding: var(--co-space-3) var(--co-space-4);
}

.header-facts div + div { border-left: 1px solid var(--co-border-default); }

.header-facts dt {
  color: var(--co-text-muted);
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
}

.header-facts dd {
  margin: 3px 0 0;
  color: var(--co-text-primary);
  font-size: 13px;
  font-variant-numeric: tabular-nums;
  overflow-wrap: anywhere;
}

@keyframes realtime-rotation {
  to { transform: rotate(360deg); }
}

@media (prefers-reduced-motion: reduce) {
  .realtime-chip--warning .realtime-icon { animation: none; }
}

@media (max-width: 900px) {
  .header-facts { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .header-facts div:nth-child(3) { border-left: 0; }
  .header-facts div:nth-child(n + 3) { border-top: 1px solid var(--co-border-default); }
}

@media (max-width: 640px) {
  .identity-row { flex-direction: column; gap: var(--co-space-4); }
  .incident-header h1 { font-size: 22px; }
  .realtime-status { align-items: flex-start; }
  .header-facts { grid-template-columns: minmax(0, 1fr); }
  .header-facts div + div,
  .header-facts div:nth-child(3) { border-left: 0; border-top: 1px solid var(--co-border-default); }
}
</style>
