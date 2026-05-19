<script setup lang="ts">
defineProps<{
  status: string;
  type?: "node" | "pod" | "event";
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
  return "info";
}
</script>

<template>
  <el-tag :type="tagType(status, type)" size="small">{{ status }}</el-tag>
</template>
