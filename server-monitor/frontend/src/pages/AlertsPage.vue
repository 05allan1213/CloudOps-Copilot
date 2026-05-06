<script setup lang="ts">
import { ref } from "vue";

import AlertEventsPanel from "../components/AlertEventsPanel.vue";
import AlertHistoriesPage from "./AlertHistoriesPage.vue";
import AlertsPanel from "../components/AlertsPanel.vue";
import { useMonitorStore } from "../stores/monitor";

const monitor = useMonitorStore();
const activeTab = ref<"current" | "history">("current");
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
