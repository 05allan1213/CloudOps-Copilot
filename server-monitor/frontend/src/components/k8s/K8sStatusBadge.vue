<script setup lang="ts">
import { computed } from "vue";
import {
  k8sPhaseLabel,
  k8sNodeStatusLabel,
  k8sEventTypeLabel,
  k8sJobStatusLabel,
  k8sPVStatusLabel,
  k8sPVCStatusLabel,
} from "../../utils/k8sLabels";

const props = defineProps<{
  status: string;
  type?: "node" | "pod" | "event" | "job" | "pv" | "pvc";
  showLabel?: boolean;
}>();

function tagType(status: string, type?: string): "" | "success" | "warning" | "danger" | "info" {
  if (type === "node") {
    return status === "Ready" ? "success" : "danger";
  }
  if (type === "pod") {
    switch (status) {
      case "Running": return "success";
      case "Pending": return "warning";
      case "Failed": return "danger";
      case "Succeeded": return "info";
      default: return "info";
    }
  }
  if (type === "event") {
    return status === "Warning" ? "danger" : "info";
  }
  if (type === "job") {
    switch (status) {
      case "Completed": return "success";
      case "Failed": return "danger";
      case "Running": return "";
      case "Suspended": return "warning";
      default: return "info";
    }
  }
  if (type === "pv") {
    switch (status) {
      case "Bound": return "success";
      case "Available": return "";
      case "Released": return "warning";
      case "Failed": return "danger";
      default: return "info";
    }
  }
  if (type === "pvc") {
    switch (status) {
      case "Bound": return "success";
      case "Pending": return "warning";
      case "Lost": return "danger";
      default: return "info";
    }
  }
  return "info";
}

const displayLabel = computed(() => {
  if (props.showLabel === false) return props.status;
  switch (props.type) {
    case "node": return k8sNodeStatusLabel(props.status);
    case "pod": return k8sPhaseLabel(props.status);
    case "event": return k8sEventTypeLabel(props.status);
    case "job": return k8sJobStatusLabel(props.status);
    case "pv": return k8sPVStatusLabel(props.status);
    case "pvc": return k8sPVCStatusLabel(props.status);
    default: return props.status;
  }
});
</script>

<template>
  <el-tag
    :type="tagType(status, type)"
    size="small"
    class="k8s-status-badge"
  >
    <span class="badge-label">{{ displayLabel }}</span>
    <span
      v-if="displayLabel !== status"
      class="badge-original"
    >{{ status }}</span>
  </el-tag>
</template>

<style scoped>
.k8s-status-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.badge-label {
  font-weight: 500;
}

.badge-original {
  font-size: 11px;
  opacity: 0.6;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}
</style>
