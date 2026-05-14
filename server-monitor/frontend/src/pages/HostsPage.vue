<script setup lang="ts">
import { onMounted, ref } from "vue";

import { fetchHostGroups } from "../api/hostGroups";
import FilterPanel from "../components/common/FilterPanel.vue";
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

function onGroupChange(value: number) {
  monitor.setHostGroup(value);
}

onMounted(loadHostGroups);
</script>

<template>
  <el-alert
    v-if="monitor.hostsError || groupsError"
    :title="monitor.hostsError || groupsError"
    type="error"
    show-icon
    closable
    style="margin-bottom: 16px"
  />

  <FilterPanel @search="monitor.applyHostSearch" @reset="monitor.resetHostFilters">
    <el-form-item label="主机分组">
      <el-select
        :model-value="monitor.selectedHostGroup"
        placeholder="全部分组"
        clearable
        style="width: 200px"
        @change="onGroupChange"
      >
        <el-option :value="0" label="全部分组" />
        <el-option
          v-for="group in hostGroups"
          :key="group.id"
          :value="group.id"
          :label="`${group.name} (${group.member_count})`"
        />
      </el-select>
    </el-form-item>
  </FilterPanel>

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
