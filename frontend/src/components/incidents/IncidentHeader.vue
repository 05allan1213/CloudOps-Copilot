<script setup lang="ts">
import { severityLabel } from "../../models/incidents";
import type { IncidentView } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import IncidentStatusBadge from "./IncidentStatusBadge.vue";

defineProps<{ incident: IncidentView; realtimeState: string }>();
</script>

<template>
  <header class="incident-header">
    <div>
      <div class="eyebrow">
        Incident · {{ incident.id }}
      </div>
      <h1>{{ incident.summary || "Incident cycle in progress" }}</h1>
      <p>Cycle {{ incident.cycle }} · optimistic version {{ incident.version }}</p>
    </div>
    <div
      class="header-badges"
      aria-label="Incident status"
    >
      <el-tag
        :type="incident.severity === 'critical' ? 'danger' : incident.severity === 'warning' ? 'warning' : 'info'"
        effect="dark"
      >
        {{ severityLabel(incident.severity) }}
      </el-tag>
      <IncidentStatusBadge :status="incident.status" />
      <el-tag
        :type="realtimeState === 'connected' ? 'success' : realtimeState === 'reconnecting' ? 'warning' : 'info'"
        effect="plain"
      >
        SSE: {{ realtimeState }}
      </el-tag>
      <el-tag
        v-if="incident.needs_attention"
        type="danger"
        effect="dark"
      >
        Needs attention
      </el-tag>
    </div>
    <dl class="header-facts">
      <div><dt>Blocking reason</dt><dd>{{ incident.blocking_reason_code || "None" }}</dd></div>
      <div><dt>Created</dt><dd>{{ formatIncidentTime(incident.created_at) }}</dd></div>
      <div><dt>Updated</dt><dd>{{ formatIncidentTime(incident.updated_at) }}</dd></div>
    </dl>
  </header>
</template>

<style scoped>
.incident-header { display: grid; gap: 18px; padding: 24px; border-radius: 12px; color: white; background: linear-gradient(125deg, #172554, #312e81 55%, #0f766e); }
.eyebrow { font-size: 12px; opacity: .72; overflow-wrap: anywhere; }
h1 { margin: 6px 0; font-size: clamp(24px, 4vw, 36px); }
p { margin: 0; opacity: .82; }
.header-badges { display: flex; flex-wrap: wrap; gap: 8px; }
.header-facts { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 12px; margin: 0; }
.header-facts div { padding: 10px 12px; border: 1px solid rgba(255,255,255,.18); border-radius: 8px; background: rgba(255,255,255,.06); }
dt { font-size: 11px; text-transform: uppercase; opacity: .65; }
dd { margin: 4px 0 0; font-size: 13px; overflow-wrap: anywhere; }
</style>
