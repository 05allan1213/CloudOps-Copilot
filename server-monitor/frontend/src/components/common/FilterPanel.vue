<script setup lang="ts">
import { ElMessage } from "element-plus";

const emit = defineEmits<{
  search: [];
  reset: [];
}>();

function handleSearch() {
  emit("search");
}

function handleReset() {
  emit("reset");
}
</script>

<template>
  <div class="filter-panel">
    <el-form inline class="filter-form" @submit.prevent="handleSearch">
      <slot />
      <el-form-item>
        <el-button type="primary" @click="handleSearch">查询</el-button>
        <el-button @click="handleReset">重置</el-button>
      </el-form-item>
    </el-form>
  </div>
</template>

<style scoped>
.filter-panel {
  margin-bottom: 16px;
  padding: 16px;
  background: var(--cloudops-bg-card);
  border: 1px solid var(--cloudops-border-color);
  border-radius: 8px;
}

.filter-form {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0;
}

.filter-form :deep(.el-form-item) {
  margin-bottom: 8px;
  margin-right: 16px;
}

.filter-form :deep(.el-form-item:last-child) {
  margin-right: 0;
}

@media (max-width: 768px) {
  .filter-form :deep(.el-form-item) {
    margin-right: 8px;
    width: 100%;
  }
}
</style>
