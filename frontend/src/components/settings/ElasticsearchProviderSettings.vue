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
      icon="i-lucide-file-search"
      title="Elasticsearch 日志检索"
      description="配置日志搜索入口、返回边界与外部 Discover 上下文。"
    />
    <ProviderConnectionBoundary
      :model-value="modelValue"
      endpoint-label="Elasticsearch Base URL"
      endpoint-help="Elasticsearch 或兼容日志搜索 API 的根地址。"
      result-label="日志结果上限"
      result-help="单次历史日志查询允许返回的最大行数。"
      context-label="Discover 地址"
      context-help="用于打开 Kibana Discover 或兼容日志工作台。"
      @update:model-value="emit('update:modelValue', $event)"
    />
    <UAlert
      color="neutral"
      variant="soft"
      icon="i-lucide-database"
      title="索引与 Tenant 边界"
      description="Index pattern、Authentication、Tenant header 和 TLS 仍由服务端管理，当前页面只编辑已公开的连接契约。"
    />
  </div>
</template>
