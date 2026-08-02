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
      icon="i-lucide-bell-ring"
      title="Alertmanager 告警连接"
      description="配置告警读取与上下文跳转；Silence 是否允许仍受服务端权限和运行范围约束。"
    />
    <ProviderConnectionBoundary
      :model-value="modelValue"
      endpoint-label="Alertmanager Base URL"
      endpoint-help="Alertmanager v2 API 或兼容服务的根地址。"
      result-label="告警结果上限"
      result-help="单次告警读取允许返回的最大数量。"
      context-label="告警详情地址"
      context-help="用于打开外部 Alertmanager 或告警工作台。"
      @update:model-value="emit('update:modelValue', $event)"
    />
    <UAlert
      color="neutral"
      variant="soft"
      icon="i-lucide-route"
      title="Receiver 与 Silence 权限"
      description="Receiver 映射、Webhook、认证和 Silence 写权限尚未进入当前可写 Schema，不会与普通连接字段混在一起。"
    />
  </div>
</template>
