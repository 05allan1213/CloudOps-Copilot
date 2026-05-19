<script setup lang="ts">
import { useRouter } from "vue-router";

import type { K8sHostAssociation } from "../../types/k8s";

defineProps<{
  association?: K8sHostAssociation;
  nodeName: string;
}>();

const router = useRouter();
</script>

<template>
  <el-card shadow="never" class="association-card">
    <template #header>
      <div class="association-header">
        <span class="association-title">主机关联</span>
      </div>
    </template>
    <div v-if="association" class="association-content">
      <div class="association-item">
        <span class="association-label">状态</span>
        <el-tag :type="association.online ? 'success' : 'danger'" size="small">
          {{ association.online ? "在线" : "离线" }}
        </el-tag>
      </div>
      <div v-if="association.last_scrape" class="association-item">
        <span class="association-label">最近采集</span>
        <span class="association-value">{{ new Date(association.last_scrape).toLocaleString() }}</span>
      </div>
    </div>
    <div v-else class="association-empty">
      <el-tag type="info" size="small">未关联（本地环境可能不支持关联）</el-tag>
    </div>
  </el-card>
</template>

<style scoped>
.association-card :deep(.el-card__body) {
  padding: 16px;
}

.association-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.association-title {
  font-size: var(--cloudops-font-size-card-title, 14px);
  font-weight: 600;
  color: var(--cloudops-text-primary);
}

.association-content {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.association-item {
  display: flex;
  align-items: center;
  gap: 8px;
}

.association-label {
  font-size: 13px;
  color: var(--cloudops-text-secondary);
  min-width: 70px;
}

.association-value {
  font-size: 13px;
  color: var(--cloudops-text-primary);
}

.association-empty {
  display: flex;
  align-items: center;
}
</style>
