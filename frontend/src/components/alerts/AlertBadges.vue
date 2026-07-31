<script setup lang="ts">
import { computed } from "vue";

import type { AlertSeverity, AlertStatus } from "../../api/alerts";

const props = defineProps<{ status: AlertStatus; severity: AlertSeverity }>();

const statusDefinition = computed(() => props.status === "firing"
  ? { label: "触发中", color: "error" as const, icon: "i-lucide-radio-tower" }
  : { label: "已恢复", color: "success" as const, icon: "i-lucide-circle-check" });

const severityDefinition = computed(() => ({
  critical: { label: "严重", color: "error" as const, icon: "i-lucide-octagon-alert" },
  warning: { label: "警告", color: "warning" as const, icon: "i-lucide-triangle-alert" },
  info: { label: "信息", color: "info" as const, icon: "i-lucide-info" },
  unknown: { label: "未知", color: "neutral" as const, icon: "i-lucide-circle-help" },
})[props.severity]);
</script>

<template>
  <span class="alert-badges">
    <UBadge
      :data-alert-status="status"
      :color="statusDefinition.color"
      variant="soft"
      size="sm"
      :icon="statusDefinition.icon"
      :label="statusDefinition.label"
    />
    <UBadge
      :data-alert-severity="severity"
      :color="severityDefinition.color"
      variant="soft"
      size="sm"
      :icon="severityDefinition.icon"
      :label="severityDefinition.label"
    />
  </span>
</template>

<style scoped>
.alert-badges {
  display: inline-flex;
  min-width: 0;
  flex-wrap: wrap;
  gap: var(--co-space-1);
}
</style>
