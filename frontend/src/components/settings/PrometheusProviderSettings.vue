<script setup lang="ts">
import UAlert from "@nuxt/ui/components/Alert.vue";

import type { ProviderConfiguration } from "../../api/platform";
import ProviderConnectionBoundary from "./ProviderConnectionBoundary.vue";

defineProps<{ modelValue: ProviderConfiguration }>();
const emit = defineEmits<{ "update:modelValue": [value: ProviderConfiguration] }>();
</script>

<template>
  <div class="provider-type-settings">
    <UAlert
      color="info"
      variant="soft"
      icon="i-lucide-chart-no-axes-combined"
      title="Prometheus 指标查询"
      description="控制 PromQL 查询入口、时间边界和从 CloudOps 跳转到指标上下文的地址。"
    />
    <ProviderConnectionBoundary
      :model-value="modelValue"
      endpoint-label="Prometheus Base URL"
      endpoint-help="Prometheus 或兼容查询 API 的根地址。"
      result-label="Series 结果上限"
      result-help="单次查询允许返回的最大 Series 数量。"
      context-label="Explore 地址"
      context-help="用于打开 Grafana Explore 或 Provider 自带查询页面。"
      @update:model-value="emit('update:modelValue', $event)"
    />
    <UAlert
      color="neutral"
      variant="soft"
      icon="i-lucide-tags"
      title="Tenant 与 Label 边界"
      description="Authentication、Tenant header、Label filters 与 TLS 仍由服务端 Provider 配置管理，当前 Revision Schema 不支持从此处修改。"
    />
  </div>
</template>
