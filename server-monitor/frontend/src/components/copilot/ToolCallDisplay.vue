<script setup lang="ts">
import type { CopilotToolCall } from "../../types";

defineProps<{
  toolCalls: CopilotToolCall[];
}>();

function isK8sTool(tool: CopilotToolCall): boolean {
  return tool.name.startsWith("k8s.");
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function k8sRecords(value: unknown): Record<string, unknown>[] {
  if (Array.isArray(value)) return value.filter(isRecord);
  return isRecord(value) ? [value] : [];
}

function k8sColumns(toolName: string): string[] {
  switch (toolName) {
    case "k8s.get_pods":
      return ["namespace", "name", "phase", "ready_containers", "total_containers", "restart_count", "node_name"];
    case "k8s.get_deployments":
      return ["namespace", "name", "replicas", "ready_replicas", "updated_replicas", "available_replicas"];
    case "k8s.get_services":
      return ["namespace", "name", "type", "cluster_ip", "ports"];
    case "k8s.get_nodes":
      return ["name", "ready", "kubelet_version", "capacity"];
    case "k8s.get_events":
      return ["namespace", "type", "reason", "involved_kind", "involved_name", "message"];
    default:
      return [];
  }
}

function k8sValue(row: Record<string, unknown>, key: string): string {
  const value = row[key];
  if (value === undefined || value === null || value === "") return "-";
  if (Array.isArray(value)) return value.map((item) => String(item)).join(", ");
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

function k8sLogLines(value: unknown): string[] {
  if (!isRecord(value) || !Array.isArray(value.lines)) return [];
  return value.lines.map((line) => String(line));
}

function toolResultPreview(value: unknown): string {
  if (value === undefined || value === null) return "";
  const text = JSON.stringify(value, null, 2);
  return text.length > 420 ? `${text.slice(0, 420)}...` : text;
}

function toolTagType(status: string): "success" | "danger" | "" {
  return status === "success" ? "success" : "danger";
}
</script>

<template>
  <div class="tool-list">
    <el-collapse>
      <el-collapse-item
        v-for="tool in toolCalls"
        :key="`${tool.name}-${tool.status}`"
        :name="tool.name"
      >
        <template #title>
          <div class="tool-title">
            <span class="tool-name">{{ tool.name }}</span>
            <el-tag :type="toolTagType(tool.status)" size="small">{{ tool.status }}</el-tag>
          </div>
        </template>

        <pre v-if="tool.error" class="tool-error">{{ tool.error }}</pre>

        <div v-else-if="isK8sTool(tool)" class="k8s-result">
          <div v-if="k8sLogLines(tool.result).length" class="k8s-log-lines">
            <code v-for="(line, lineIndex) in k8sLogLines(tool.result)" :key="lineIndex">{{ line }}</code>
          </div>
          <el-table
            v-else-if="k8sRecords(tool.result).length && k8sColumns(tool.name).length"
            :data="k8sRecords(tool.result)"
            size="small"
            stripe
            style="width: 100%"
          >
            <el-table-column
              v-for="column in k8sColumns(tool.name)"
              :key="column"
              :prop="column"
              :label="column"
              min-width="100"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                {{ k8sValue(row, column) }}
              </template>
            </el-table-column>
          </el-table>
          <el-empty v-else description="暂无 K8s 结果" :image-size="32" />
        </div>

        <pre v-else class="tool-json">{{ toolResultPreview(tool.result) }}</pre>
      </el-collapse-item>
    </el-collapse>
  </div>
</template>

<style scoped>
.tool-list {
  margin-top: 12px;
}

.tool-title {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}

.tool-name {
  font-size: 13px;
  font-weight: 600;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.tool-error {
  color: var(--el-color-danger);
  font-size: 12px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  margin: 0;
}

.k8s-result {
  max-height: 280px;
  overflow: auto;
}

.k8s-log-lines {
  display: grid;
  gap: 4px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
}

.k8s-log-lines code {
  color: var(--el-text-color-secondary);
  overflow-wrap: anywhere;
}

.tool-json {
  max-height: 220px;
  overflow: auto;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.45;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  margin: 0;
}
</style>
