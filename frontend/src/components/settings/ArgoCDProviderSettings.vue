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
      icon="i-lucide-git-pull-request-arrow"
      title="Argo CD 交付连接"
      description="配置 Application 读取入口和外部控制台跳转；Sync 始终受独立 authority 与精确版本保护。"
    />
    <ProviderConnectionBoundary
      :model-value="modelValue"
      endpoint-label="Argo CD Server"
      endpoint-help="Argo CD API Server 的 HTTPS 地址。"
      result-label="Application 上限"
      result-help="单次读取允许返回的最大 Application 数量。"
      context-label="Argo CD Console 地址"
      context-help="用于打开 Project 或 Application 的外部详情。"
      @update:model-value="emit('update:modelValue', $event)"
    />
    <UAlert
      color="neutral"
      variant="soft"
      icon="i-lucide-list-checks"
      title="Project 与 Sync 权限"
      description="Token、Project/Application allowlist、TLS 与 Sync 权限尚未进入当前 Revision Schema；授权与执行不会由这些通用字段推断。"
    />
  </div>
</template>
