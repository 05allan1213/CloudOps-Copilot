<script setup lang="ts">
import type { DiagnosisRuleResult } from "../../types";

defineProps<{
  ruleResults: DiagnosisRuleResult[];
}>();
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <span class="card-title">规则分析</span>
    </template>
    <el-empty v-if="ruleResults.length === 0" description="暂无规则分析" :image-size="32" />
    <el-timeline v-else>
      <el-timeline-item
        v-for="rule in ruleResults"
        :key="rule.rule"
        :type="rule.passed ? 'success' : 'primary'"
        :hollow="!rule.passed"
      >
        <div class="rule-item">
          <div class="rule-head">
            <strong>{{ rule.rule }}</strong>
            <el-tag :type="rule.passed ? 'success' : 'info'" size="small">
              {{ rule.passed ? "命中" : "未命中" }}
            </el-tag>
          </div>
          <p>{{ rule.detail }}</p>
        </div>
      </el-timeline-item>
    </el-timeline>
  </el-card>
</template>

<style scoped>
.card-title {
  font-size: 14px;
  font-weight: 600;
}

.rule-item {
  display: grid;
  gap: 4px;
}

.rule-head {
  display: flex;
  align-items: center;
  gap: 8px;
}

.rule-head strong {
  font-size: 13px;
}

.rule-item p {
  color: var(--el-text-color-placeholder);
  font-size: 12px;
  margin: 0;
}
</style>
