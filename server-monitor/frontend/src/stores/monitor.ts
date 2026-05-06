import { computed, ref } from "vue";
import { defineStore } from "pinia";

import { fetchActiveAlerts, fetchAlertEvents } from "../api/alerts";
import { fetchHosts } from "../api/hosts";
import type { AlertEvent, AlertRecord, Host } from "../types";

type SeverityFilter = "all" | "critical" | "warning" | "info";
type HostStatusFilter = "all" | "up" | "down";
type HostSort = "instance" | "cpu_desc" | "memory_desc";
type HostRiskFilter = "all" | "high_cpu" | "high_memory";
type EventStatusFilter = "all" | "firing" | "resolved";

export const useMonitorStore = defineStore("monitor", () => {
  const alerts = ref<AlertRecord[]>([]);
  const alertEvents = ref<AlertEvent[]>([]);
  const hosts = ref<Host[]>([]);
  const loading = ref(true);
  const refreshing = ref(false);
  const alertsError = ref("");
  const alertEventsError = ref("");
  const hostsError = ref("");
  const hostSearchInput = ref("");
  const appliedHostQuery = ref("");
  const selectedSeverity = ref<SeverityFilter>("all");
  const selectedHostStatus = ref<HostStatusFilter>("all");
  const selectedHostSort = ref<HostSort>("instance");
  const selectedHostRisk = ref<HostRiskFilter>("all");
  const selectedHostGroup = ref(0);
  const selectedEventStatus = ref<EventStatusFilter>("all");
  const selectedEventSeverity = ref<SeverityFilter>("all");
  const lastUpdateTime = ref(Date.now());
  const updateAgo = ref("");
  const toasts = ref<{ id: number; message: string; severity: string }[]>([]);
  const toastTimers: number[] = [];
  let toastId = 0;

  const alertEventsLimit = 8;

  const criticalCount = computed(
    () =>
      alerts.value.filter((a) => (a.labels.severity ?? "info") === "critical")
        .length,
  );
  const warningCount = computed(
    () =>
      alerts.value.filter((a) => (a.labels.severity ?? "info") === "warning")
        .length,
  );
  const infoCount = computed(
    () =>
      alerts.value.filter((a) => (a.labels.severity ?? "info") === "info")
        .length,
  );
  const latestAlertEvents = computed(() =>
    alertEvents.value.slice(0, alertEventsLimit),
  );
  const hostCountLabel = computed(() => {
    switch (selectedHostStatus.value) {
      case "up":
        return "在线主机";
      case "down":
        return "离线主机";
      default:
        return "当前主机";
    }
  });
  const highCPUHostCount = computed(
    () => hosts.value.filter((host) => isHighCPU(host)).length,
  );
  const highMemoryHostCount = computed(
    () => hosts.value.filter((host) => isHighMemory(host)).length,
  );
  const bothRiskHostCount = computed(
    () => hosts.value.filter((host) => hostRiskVariant(host) === "both").length,
  );
  const hostFilterSummary = computed(() => {
    const parts: string[] = [];

    if (selectedHostStatus.value === "up") {
      parts.push("在线");
    } else if (selectedHostStatus.value === "down") {
      parts.push("离线");
    }

    if (appliedHostQuery.value) {
      parts.push(`搜索: ${appliedHostQuery.value}`);
    }

    switch (selectedHostSort.value) {
      case "cpu_desc":
        parts.push("按 CPU 排序");
        break;
      case "memory_desc":
        parts.push("按内存排序");
        break;
    }

    switch (selectedHostRisk.value) {
      case "high_cpu":
        parts.push("高 CPU");
        break;
      case "high_memory":
        parts.push("高内存");
        break;
    }

    if (selectedHostGroup.value !== 0) {
      parts.push(`分组 #${selectedHostGroup.value}`);
    }

    return parts;
  });
  const hasActiveHostFilters = computed(() => hostFilterSummary.value.length > 0);
  const hostViewSummary = computed(() => {
    if (loading.value) {
      return "主机视图加载中";
    }

    return hasActiveHostFilters.value
      ? `当前视图匹配 ${hosts.value.length} 台主机`
      : `当前展示 ${hosts.value.length} 台主机`;
  });

  function severityClass(severity: string | undefined): string {
    switch (severity ?? "info") {
      case "critical":
        return "severity-critical";
      case "warning":
        return "severity-warning";
      default:
        return "severity-info";
    }
  }

  function severityLabel(severity: string | undefined): string {
    switch (severity ?? "info") {
      case "critical":
        return "严重";
      case "warning":
        return "警告";
      default:
        return "提示";
    }
  }

  function setSeverityFilter(value: SeverityFilter) {
    selectedSeverity.value = value;
    loadAlerts();
  }

  function setHostStatusFilter(value: HostStatusFilter) {
    selectedHostStatus.value = value;
    loadHosts();
  }

  function setHostSort(value: HostSort) {
    selectedHostSort.value = value;
    loadHosts();
  }

  function setHostRisk(value: HostRiskFilter) {
    selectedHostRisk.value = value;
    loadHosts();
  }

  function setHostGroup(value: number) {
    selectedHostGroup.value = value;
    loadHosts();
  }

  function applyHostSearch() {
    appliedHostQuery.value = hostSearchInput.value.trim().toLowerCase();
    loadHosts();
  }

  function resetHostFilters() {
    hostSearchInput.value = "";
    appliedHostQuery.value = "";
    selectedHostStatus.value = "all";
    selectedHostSort.value = "instance";
    selectedHostRisk.value = "all";
    selectedHostGroup.value = 0;
    loadHosts();
  }

  function setEventStatusFilter(value: EventStatusFilter) {
    selectedEventStatus.value = value;
    loadAlertEvents();
  }

  function setEventSeverityFilter(value: SeverityFilter) {
    selectedEventSeverity.value = value;
    loadAlertEvents();
  }

  async function loadAlerts() {
    try {
      alertsError.value = "";
      const data = await fetchActiveAlerts({
        severity: selectedSeverity.value,
      });
      alerts.value = data;
    } catch (err) {
      alertsError.value = err instanceof Error ? err.message : "加载告警失败";
    }
  }

  async function loadAlertEvents() {
    try {
      alertEventsError.value = "";
      const data = await fetchAlertEvents({
        limit: alertEventsLimit,
        status: selectedEventStatus.value,
        severity: selectedEventSeverity.value,
      });
      alertEvents.value = data;
    } catch (err) {
      alertEventsError.value = err instanceof Error ? err.message : "加载事件失败";
    }
  }

  async function loadHosts() {
    try {
      hostsError.value = "";
      const data = await fetchHosts({
        status: selectedHostStatus.value,
        q: appliedHostQuery.value,
        sort: selectedHostSort.value,
        risk: selectedHostRisk.value,
        group: selectedHostGroup.value,
      });
      hosts.value = sortHosts(data);
    } catch (err) {
      hostsError.value = err instanceof Error ? err.message : "加载主机失败";
    }
  }

  let refreshInProgress = false;

  async function refreshAll() {
    if (refreshInProgress) {
      return;
    }
    refreshInProgress = true;
    refreshing.value = true;
    try {
      await Promise.all([loadAlerts(), loadAlertEvents(), loadHosts()]);
      lastUpdateTime.value = Date.now();
    } finally {
      if (loading.value) {
        loading.value = false;
      }
      refreshing.value = false;
      refreshInProgress = false;
    }
  }

  function pushAlertEvent(event: AlertEvent) {
    alertEvents.value.unshift(event);
    if (alertEvents.value.length > 200) {
      alertEvents.value = alertEvents.value.slice(0, 200);
    }
  }

  function applyIncomingAlert(event: AlertEvent) {
    pushAlertEvent(event);

    const alert: AlertRecord = {
      status: event.status,
      fingerprint: event.fingerprint,
      labels: event.labels,
      annotations: event.annotations,
      startsAt: event.startsAt,
      endsAt: event.endsAt,
      generatorURL: event.generatorURL,
    };

    const idx = alerts.value.findIndex(
      (a) => a.fingerprint === alert.fingerprint,
    );
    if (alert.status === "resolved") {
      if (idx !== -1) alerts.value.splice(idx, 1);
      return;
    }
    if (idx !== -1) {
      alerts.value[idx] = alert;
    } else {
      alerts.value.unshift(alert);
      const sev = alert.labels.severity ?? "info";
      const sevLabel = severityLabel(sev);
      const name = alert.labels.alertname || "未知告警";
      showToast(`新${sevLabel}告警: ${name}`, sev);
    }
  }

  function applyIncomingHosts(newHosts: Host[]) {
    if (selectedHostGroup.value !== 0) {
      loadHosts();
      return;
    }

    hosts.value = sortHosts(
      newHosts.filter((host) => {
        const statusMatched =
          selectedHostStatus.value === "all" ||
          isHostUp(host.status) === (selectedHostStatus.value === "up");

        return (
          statusMatched &&
          matchesHostQuery(host, appliedHostQuery.value) &&
          matchesHostRisk(host)
        );
      }),
    );
    lastUpdateTime.value = Date.now();
  }

  function updateAgoText() {
    const diff = Math.floor((Date.now() - lastUpdateTime.value) / 1000);
    if (diff < 5) {
      updateAgo.value = "刚刚更新";
    } else if (diff < 60) {
      updateAgo.value = `${diff}秒前更新`;
    } else if (diff < 3600) {
      updateAgo.value = `${Math.floor(diff / 60)}分钟前更新`;
    } else {
      updateAgo.value = `${Math.floor(diff / 3600)}小时前更新`;
    }
  }

  function clearToastTimers() {
    toastTimers.forEach((id) => clearTimeout(id));
    toastTimers.length = 0;
  }

  function showToast(message: string, severity: string) {
    const id = ++toastId;
    toasts.value.push({ id, message, severity });
    if (toasts.value.length > 10) {
      toasts.value.splice(0, toasts.value.length - 10);
    }
    const timerId = window.setTimeout(() => {
      toasts.value = toasts.value.filter((t) => t.id !== id);
      const idx = toastTimers.indexOf(timerId);
      if (idx !== -1) {
        toastTimers.splice(idx, 1);
      }
    }, 4000);
    toastTimers.push(timerId);
  }

  function isHighCPU(host: Host): boolean {
    return host.cpu >= 80;
  }

  function isHighMemory(host: Host): boolean {
    return host.memory >= 85;
  }

  function hostRiskVariant(host: Host): "normal" | "cpu" | "memory" | "both" {
    const highCPU = isHighCPU(host);
    const highMemory = isHighMemory(host);

    if (highCPU && highMemory) {
      return "both";
    }
    if (highCPU) {
      return "cpu";
    }
    if (highMemory) {
      return "memory";
    }

    return "normal";
  }

  function matchesHostQuery(host: Host, query: string): boolean {
    if (!query) {
      return true;
    }

    return host.instance.toLowerCase().includes(query);
  }

  function isHostUp(status: string): boolean {
    return status === "up";
  }

  function matchesHostRisk(host: Host): boolean {
    switch (selectedHostRisk.value) {
      case "high_cpu":
        return host.cpu >= 80;
      case "high_memory":
        return host.memory >= 85;
      default:
        return true;
    }
  }

  function sortHosts(hostList: Host[]): Host[] {
    const sorted = [...hostList];

    switch (selectedHostSort.value) {
      case "cpu_desc":
        sorted.sort((a, b) => {
          if (b.cpu === a.cpu) {
            return a.instance.localeCompare(b.instance);
          }
          return b.cpu - a.cpu;
        });
        break;
      case "memory_desc":
        sorted.sort((a, b) => {
          if (b.memory === a.memory) {
            return a.instance.localeCompare(b.instance);
          }
          return b.memory - a.memory;
        });
        break;
      default:
        sorted.sort((a, b) => a.instance.localeCompare(b.instance));
    }

    return sorted;
  }

  return {
    alerts,
    alertEvents,
    hosts,
    loading,
    refreshing,
    alertsError,
    alertEventsError,
    hostsError,
    hostSearchInput,
    appliedHostQuery,
    selectedSeverity,
    selectedHostStatus,
    selectedHostSort,
    selectedHostRisk,
    selectedHostGroup,
    selectedEventStatus,
    selectedEventSeverity,
    lastUpdateTime,
    updateAgo,
    toasts,
    criticalCount,
    warningCount,
    infoCount,
    latestAlertEvents,
    hostCountLabel,
    highCPUHostCount,
    highMemoryHostCount,
    bothRiskHostCount,
    hostFilterSummary,
    hasActiveHostFilters,
    hostViewSummary,
    severityClass,
    severityLabel,
    setSeverityFilter,
    setHostStatusFilter,
    setHostSort,
    setHostRisk,
    setHostGroup,
    applyHostSearch,
    resetHostFilters,
    setEventStatusFilter,
    setEventSeverityFilter,
    loadAlerts,
    loadAlertEvents,
    loadHosts,
    refreshAll,
    applyIncomingAlert,
    applyIncomingHosts,
    updateAgoText,
    clearToastTimers,
  };
});
