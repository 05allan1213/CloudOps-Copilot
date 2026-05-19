<script setup lang="ts">
defineProps<{
  summary: string;
  rootCause: string;
}>();
</script>

<template>
  <el-card shadow="never">
    <el-descriptions
      :column="1"
      border
    >
      <el-descriptions-item label="摘要">
        {{ summary || "-" }}
      </el-descriptions-item>
      <el-descriptions-item label="根因假设">
        {{ rootCause || "-" }}
      </el-descriptions-item>
    </el-descriptions>
  </el-card>
</template>
