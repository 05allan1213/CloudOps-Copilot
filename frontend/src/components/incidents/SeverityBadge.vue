<script setup lang="ts">
import { computed, markRaw, type Component } from "vue";
import {
	CircleHelp as QuestionFilled,
	CircleX as CircleCloseFilled,
	Info as InfoFilled,
	TriangleAlert as WarningFilled,
} from "lucide-vue-next";

import { severityLabel } from "../../models/incidents";
import type { IncidentSeverity } from "../../types/incidents";

const props = withDefaults(defineProps<{
  severity: IncidentSeverity;
  showIcon?: boolean;
}>(), {
  showIcon: true,
});

const definitions: Record<IncidentSeverity, { icon: Component; tone: string }> = {
  critical: { icon: markRaw(CircleCloseFilled), tone: "critical" },
  warning: { icon: markRaw(WarningFilled), tone: "warning" },
  info: { icon: markRaw(InfoFilled), tone: "info" },
  unknown: { icon: markRaw(QuestionFilled), tone: "neutral" },
};

const definition = computed(() => definitions[props.severity] ?? definitions.unknown);
</script>

<template>
  <span
    class="severity-badge"
    :class="`severity-badge--${definition.tone}`"
  >
    <component
      :is="definition.icon"
      v-if="showIcon"
      :size="14"
      aria-hidden="true"
    />
    {{ severityLabel(severity) }}
  </span>
</template>

<style scoped>
.severity-badge {
  display: inline-flex;
  min-height: 24px;
  align-items: center;
  gap: 5px;
  padding: 2px 8px;
  border: 1px solid;
  border-radius: var(--co-radius-pill);
  font-size: 12px;
  font-weight: 700;
  line-height: 1.2;
  white-space: nowrap;
}

.severity-badge--critical {
  border-color: var(--co-status-critical-border);
  color: var(--co-status-critical-fg);
  background: var(--co-status-critical-bg);
}

.severity-badge--warning {
  border-color: var(--co-status-warning-border);
  color: var(--co-status-warning-fg);
  background: var(--co-status-warning-bg);
}

.severity-badge--info {
  border-color: var(--co-status-info-border);
  color: var(--co-status-info-fg);
  background: var(--co-status-info-bg);
}

.severity-badge--neutral {
  border-color: var(--co-status-neutral-border);
  color: var(--co-status-neutral-fg);
  background: var(--co-status-neutral-bg);
}
</style>
