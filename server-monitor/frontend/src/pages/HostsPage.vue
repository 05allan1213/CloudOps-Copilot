<script setup lang="ts">
import { onMounted, ref } from "vue";

import { fetchHostGroups } from "../api/hostGroups";
import HostsPanel from "../components/HostsPanel.vue";
import { useMonitorStore } from "../stores/monitor";
import type { HostGroup } from "../types";

const monitor = useMonitorStore();
const hostGroups = ref<HostGroup[]>([]);
const groupsError = ref("");

async function loadHostGroups() {
  try {
    groupsError.value = "";
    hostGroups.value = await fetchHostGroups();
  } catch (err) {
    groupsError.value = err instanceof Error ? err.message : "加载主机分组失败";
  }
}

function onGroupChange(event: Event) {
  const value = Number((event.target as HTMLSelectElement).value);
  monitor.setHostGroup(Number.isFinite(value) ? value : 0);
}

onMounted(loadHostGroups);
</script>

<template>
  <div v-if="monitor.hostsError" class="hosts-error">
    {{ monitor.hostsError }}
  </div>
  <div v-if="groupsError" class="hosts-error">
    {{ groupsError }}
  </div>
  <div class="group-filter">
    <label>
      <span>主机分组</span>
      <select :value="monitor.selectedHostGroup" @change="onGroupChange">
        <option :value="0">全部分组</option>
        <option v-for="group in hostGroups" :key="group.id" :value="group.id">
          {{ group.name }} ({{ group.member_count }})
        </option>
      </select>
    </label>
  </div>
  <HostsPanel
    :hosts="monitor.hosts"
    :loading="monitor.loading"
    :host-search-input="monitor.hostSearchInput"
    :applied-host-query="monitor.appliedHostQuery"
    :selected-host-status="monitor.selectedHostStatus"
    :selected-host-sort="monitor.selectedHostSort"
    :selected-host-risk="monitor.selectedHostRisk"
    :host-view-summary="monitor.hostViewSummary"
    :host-filter-summary="monitor.hostFilterSummary"
    :has-active-host-filters="monitor.hasActiveHostFilters"
    @update:host-search-input="monitor.hostSearchInput = $event"
    @apply-search="monitor.applyHostSearch"
    @status-change="monitor.setHostStatusFilter"
    @sort-change="monitor.setHostSort"
    @risk-change="monitor.setHostRisk"
    @reset-filters="monitor.resetHostFilters"
  />
</template>

<style scoped>
.group-filter {
  margin-bottom: 1rem;
  display: flex;
  justify-content: flex-end;
}

.group-filter label {
  display: flex;
  align-items: center;
  gap: 0.55rem;
  color: var(--text-secondary);
  font-size: 0.82rem;
  font-weight: 700;
}

.group-filter select {
  min-width: 180px;
  cursor: pointer;
  color: var(--text-primary);
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 0.48rem 0.7rem;
}

.hosts-error {
  margin-bottom: 1rem;
  color: var(--danger);
  background: var(--danger-soft);
  border: 1px solid rgba(239, 68, 68, 0.24);
  border-radius: var(--radius-md);
  padding: 0.75rem 1rem;
  font-size: 0.82rem;
}
</style>
