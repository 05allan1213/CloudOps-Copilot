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
      icon="i-lucide-git-branch"
      title="Tempo 链路查询"
      description="配置 Trace 搜索入口、返回边界与外部链路详情跳转。"
    />
    <ProviderConnectionBoundary
      :model-value="modelValue"
      endpoint-label="Tempo Base URL"
      endpoint-help="Tempo HTTP API 或兼容 Trace Provider 的根地址。"
      result-label="Trace 结果上限"
      result-help="单次 Trace 搜索允许返回的最大数量。"
      context-label="Trace Explorer 地址"
      context-help="用于打开 Grafana Explore 或 Provider 链路详情。"
      @update:model-value="emit('update:modelValue', $event)"
    />
    <UAlert
      color="neutral"
      variant="soft"
      icon="i-lucide-fingerprint"
      title="Tenant 与查询能力"
      description="Authentication、Tenant header、TLS 与 TraceQL 能力协商尚未进入当前可写 Schema，由服务端 Provider 负责。"
    />
  </div>
</template>
