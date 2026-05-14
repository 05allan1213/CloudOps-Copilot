<script setup lang="ts">
defineProps<{
  state: "loading" | "empty" | "error" | "default";
  emptyText?: string;
  errorText?: string;
}>();
</script>

<template>
  <div class="state-wrapper">
    <el-skeleton
      v-if="state === 'loading'"
      :rows="5"
      animated
    />
    <el-empty
      v-else-if="state === 'empty'"
      :description="emptyText || '暂无数据'"
    />
    <el-result
      v-else-if="state === 'error'"
      icon="error"
      :title="errorText || '加载失败'"
      sub-title="请稍后重试"
    >
      <template #extra>
        <slot name="retry" />
      </template>
    </el-result>
    <slot v-else />
  </div>
</template>

<style scoped>
.state-wrapper {
  width: 100%;
}

.state-wrapper :deep(.el-skeleton) {
  padding: 20px;
}

.state-wrapper :deep(.el-empty) {
  padding: 40px 0;
}

.state-wrapper :deep(.el-result) {
  padding: 40px 0;
}
</style>
