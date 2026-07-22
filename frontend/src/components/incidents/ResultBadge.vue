<script setup lang="ts">
import { computed, markRaw, type Component } from "vue";
import {
  CircleCheckFilled,
  CircleCloseFilled,
  Clock,
  InfoFilled,
  QuestionFilled,
  RemoveFilled,
  WarningFilled,
} from "@element-plus/icons-vue";

import { humanizeCode, statusTone, type StatusTone } from "../../models/incidents";

const props = defineProps<{
  result: string;
  label?: string;
}>();

const tone = computed(() => statusTone(props.result));
const text = computed(() => props.label ?? (props.result.trim() ? humanizeCode(props.result) : "Unknown"));
const icons: Record<StatusTone, Component> = {
  success: markRaw(CircleCheckFilled),
  warning: markRaw(Clock),
  danger: markRaw(CircleCloseFilled),
  info: markRaw(InfoFilled),
  primary: markRaw(InfoFilled),
  inconclusive: markRaw(QuestionFilled),
  neutral: markRaw(RemoveFilled),
};
const icon = computed(() => {
  if (props.result.trim().toLowerCase() === "unavailable") return WarningFilled;
  return icons[tone.value];
});
</script>

<template>
  <span
    class="result-badge"
    :class="`result-badge--${tone}`"
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
.result-badge {
  display: inline-flex;
  min-height: 24px;
  align-items: center;
  gap: 6px;
  padding: 2px 8px;
  border: 1px solid var(--co-status-neutral-border);
  border-radius: var(--co-radius-pill);
  color: var(--co-status-neutral-fg);
  background: var(--co-status-neutral-bg);
  font-size: 12px;
  font-weight: 650;
}

.result-badge--success { border-color: var(--co-status-success-border); color: var(--co-status-success-fg); background: var(--co-status-success-bg); }
.result-badge--warning { border-color: var(--co-status-warning-border); color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }
.result-badge--danger { border-color: var(--co-status-critical-border); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.result-badge--primary { border-color: var(--co-status-info-border); color: var(--co-status-info-fg); background: var(--co-status-info-bg); }
.result-badge--info,
.result-badge--neutral { border-color: var(--co-status-neutral-border); color: var(--co-status-neutral-fg); background: var(--co-status-neutral-bg); }
.result-badge--inconclusive { border-color: var(--co-status-inconclusive-border); color: var(--co-status-inconclusive-fg); background: var(--co-status-inconclusive-bg); }
</style>
