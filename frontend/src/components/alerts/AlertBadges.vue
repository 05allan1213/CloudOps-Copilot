<script setup lang="ts">
import type { AlertSeverity, AlertStatus } from "../../api/alerts";

defineProps<{ status: AlertStatus; severity: AlertSeverity }>();
</script>

<template>
  <span class="alert-badges">
    <span class="status" :class="`is-${status}`">{{ status === "firing" ? "Firing" : "Resolved" }}</span>
    <span class="severity" :class="`is-${severity}`">{{ severity }}</span>
  </span>
</template>

<style scoped>
.alert-badges { display: inline-flex; flex-wrap: wrap; gap: var(--co-space-1); }
.status, .severity { display: inline-flex; min-height: 24px; align-items: center; padding: 0 7px; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-pill); font-family: var(--co-font-mono); font-size: 10px; font-weight: 800; text-transform: uppercase; white-space: nowrap; }
.status.is-firing, .severity.is-critical { border-color: var(--co-status-critical-border); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.status.is-resolved { border-color: var(--co-status-success-border); color: var(--co-status-success-fg); background: var(--co-status-success-bg); }
.severity.is-warning { border-color: var(--co-status-warning-border); color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }
.severity.is-info { border-color: var(--co-status-info-border); color: var(--co-status-info-fg); background: var(--co-status-info-bg); }
.severity.is-unknown { color: var(--co-status-neutral-fg); background: var(--co-status-neutral-bg); }
</style>
