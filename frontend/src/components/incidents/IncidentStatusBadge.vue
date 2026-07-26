<script setup lang="ts">
import { computed, markRaw, type Component } from "vue";
import {
	ChartNoAxesCombined as DataLine,
	CircleCheck as CircleCheckFilled,
	CircleHelp as QuestionFilled,
	CircleMinus as RemoveFilled,
	CircleX as CircleCloseFilled,
	Clock,
	Info as InfoFilled,
	LoaderCircle as Loading,
	Rocket as Promotion,
	Search,
	TriangleAlert as WarningFilled,
} from "lucide-vue-next";

import { incidentStatusLabel, statusTone } from "../../models/incidents";

const props = defineProps<{ status: string; label?: string }>();

const statusIcons: Record<string, Component> = {
  detected: markRaw(WarningFilled),
  investigating: markRaw(Search),
  awaiting_approval: markRaw(Clock),
  delivering: markRaw(Promotion),
  verifying: markRaw(DataLine),
  resolved: markRaw(CircleCheckFilled),
  closed: markRaw(RemoveFilled),
  passed: markRaw(CircleCheckFilled),
  completed: markRaw(CircleCheckFilled),
  approved: markRaw(CircleCheckFilled),
  failed: markRaw(CircleCloseFilled),
  rejected: markRaw(CircleCloseFilled),
  invalid: markRaw(CircleCloseFilled),
  timed_out: markRaw(Clock),
  inconclusive: markRaw(QuestionFilled),
  unavailable: markRaw(WarningFilled),
  running: markRaw(Loading),
  pending: markRaw(Clock),
  unknown: markRaw(InfoFilled),
};

const normalizedStatus = computed(() => props.status.trim().toLowerCase() || "unknown");
const text = computed(() => props.label ?? incidentStatusLabel(normalizedStatus.value));
const icon = computed(() => statusIcons[normalizedStatus.value] ?? QuestionFilled);
const tone = computed(() => statusTone(normalizedStatus.value));
</script>

<template>
  <span
    class="status-badge"
    :class="`status-badge--${tone}`"
  >
    <el-icon
      :size="14"
      aria-hidden="true"
    >
      <component :is="icon" />
    </el-icon>
    {{ text }}
  </span>
</template>

<style scoped>
.status-badge {
  display: inline-flex;
  min-height: 24px;
  align-items: center;
  gap: 5px;
  padding: 2px 8px;
  border: 1px solid;
  border-radius: var(--co-radius-pill);
  font-size: 12px;
  font-weight: 650;
  line-height: 1.2;
  white-space: nowrap;
}

.status-badge--success {
  border-color: var(--co-status-success-border);
  color: var(--co-status-success-fg);
  background: var(--co-status-success-bg);
}

.status-badge--warning {
  border-color: var(--co-status-warning-border);
  color: var(--co-status-warning-fg);
  background: var(--co-status-warning-bg);
}

.status-badge--danger {
  border-color: var(--co-status-critical-border);
  color: var(--co-status-critical-fg);
  background: var(--co-status-critical-bg);
}

.status-badge--info {
  border-color: var(--co-status-neutral-border);
  color: var(--co-status-neutral-fg);
  background: var(--co-status-neutral-bg);
}

.status-badge--neutral {
  border-color: var(--co-status-neutral-border);
  color: var(--co-status-neutral-fg);
  background: var(--co-status-neutral-bg);
}

.status-badge--inconclusive {
  border-color: var(--co-status-inconclusive-border);
  color: var(--co-status-inconclusive-fg);
  background: var(--co-status-inconclusive-bg);
}

.status-badge--primary {
  border-color: var(--co-status-info-border);
  color: var(--co-status-info-fg);
  background: var(--co-status-info-bg);
}
</style>
