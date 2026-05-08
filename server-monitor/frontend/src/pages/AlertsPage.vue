<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";

import AlertEventsPanel from "../components/AlertEventsPanel.vue";
import AlertHistoriesPage from "./AlertHistoriesPage.vue";
import AlertsPanel from "../components/AlertsPanel.vue";
import { createDiagnosis } from "../api/diagnosis";
import { useMonitorStore } from "../stores/monitor";
import type { AlertRecord } from "../types";

const monitor = useMonitorStore();
const router = useRouter();
const activeTab = ref<"current" | "history">("current");

async function diagnoseActiveAlert(alert: AlertRecord) {
  try {
    const report = await createDiagnosis({
      fingerprint: alert.fingerprint,
      trigger_type: "manual",
    });
    await router.push(`/diagnosis/${report.id}`);
  } catch (err) {
    monitor.alertsError = err instanceof Error ? err.message : "生成诊断失败";
  }
}
</script>

<template>
  <div class="tab-bar">
    <button :class="{ active: activeTab === 'current' }" @click="activeTab = 'current'">当前告警</button>
    <button :class="{ active: activeTab === 'history' }" @click="activeTab = 'history'">历史</button>
  </div>

  <template v-if="activeTab === 'current'">
    <AlertsPanel
      :alerts="monitor.alerts"
      :selected-severity="monitor.selectedSeverity"
      :refreshing="monitor.refreshing"
      :error="monitor.alertsError"
      @severity-change="monitor.setSeverityFilter"
      @refresh="monitor.refreshAll"
      @diagnose="diagnoseActiveAlert"
    />

    <AlertEventsPanel
      :events="monitor.latestAlertEvents"
      :selected-status="monitor.selectedEventStatus"
      :selected-severity="monitor.selectedEventSeverity"
      :error="monitor.alertEventsError"
      @status-change="monitor.setEventStatusFilter"
      @severity-change="monitor.setEventSeverityFilter"
    />
  </template>

  <AlertHistoriesPage v-else />
</template>

<style scoped>
.tab-bar {
  display: flex;
  gap: 0;
  border-bottom: 1px solid var(--border-color);
  margin-bottom: 1rem;
}

.tab-bar button {
  background: none;
  border: none;
  border-bottom: 2px solid transparent;
  color: var(--text-secondary);
  font-size: 0.88rem;
  font-weight: 700;
  padding: 0.6rem 1.2rem;
  cursor: pointer;
  transition: color 0.15s, border-color 0.15s;
}

.tab-bar button:hover {
  color: var(--text-primary);
}

.tab-bar button.active {
  color: var(--accent);
  border-bottom-color: var(--accent);
}
</style>
