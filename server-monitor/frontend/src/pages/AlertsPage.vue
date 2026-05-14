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
const activeTab = ref("current");

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
  <el-tabs v-model="activeTab">
    <el-tab-pane label="当前告警" name="current">
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
    </el-tab-pane>
    <el-tab-pane label="历史" name="history">
      <AlertHistoriesPage />
    </el-tab-pane>
  </el-tabs>
</template>
