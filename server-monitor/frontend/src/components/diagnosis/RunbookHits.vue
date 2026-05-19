<script setup lang="ts">
import type { RunbookEvidence } from "../../types";

defineProps<{
  runbooks: RunbookEvidence[];
}>();

function runbookMatches(runbook: RunbookEvidence): string[] {
  return [
    ...(runbook.matched_alerts ?? []),
    ...(runbook.matched_keywords ?? []),
    ...(runbook.matched_metrics ?? []),
  ];
}

function formatScore(value?: number): string {
  return (value ?? 0).toFixed(1);
}
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <span class="card-title">Runbook 命中</span>
    </template>
    <el-empty
      v-if="runbooks.length === 0"
      description="未命中匹配 Runbook"
      :image-size="32"
    />
    <el-collapse v-else>
      <el-collapse-item
        v-for="runbook in runbooks"
        :key="`${runbook.file}-${runbook.title}`"
        :name="runbook.title"
      >
        <template #title>
          <div class="runbook-title">
            <strong>{{ runbook.title }}</strong>
            <span class="runbook-meta">{{ runbook.file }} · score {{ formatScore(runbook.score) }}</span>
          </div>
        </template>

        <div
          v-if="runbookMatches(runbook).length"
          class="tag-row"
        >
          <el-tag
            v-for="match in runbookMatches(runbook)"
            :key="match"
            size="small"
            effect="plain"
          >
            {{ match }}
          </el-tag>
        </div>

        <pre class="snippet-content">{{ runbook.snippet }}</pre>
      </el-collapse-item>
    </el-collapse>
  </el-card>
</template>

<style scoped>
.card-title {
  font-size: 14px;
  font-weight: 600;
}

.runbook-title {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}

.runbook-title strong {
  font-size: 13px;
}

.runbook-meta {
  color: var(--el-text-color-placeholder);
  font-size: 12px;
  white-space: nowrap;
}

.tag-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 8px;
}

.snippet-content {
  max-height: 220px;
  overflow: auto;
  color: var(--el-text-color-secondary);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-size: 12px;
  margin: 0;
}
</style>
