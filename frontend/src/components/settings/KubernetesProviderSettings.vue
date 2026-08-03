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
      icon="i-lucide-box"
      title="Kubernetes 集群连接"
      description="配置 API Server 读取边界与集群上下文入口；测试时会携带当前默认 Cluster Scope。"
    />
    <ProviderConnectionBoundary
      :model-value="modelValue"
      endpoint-label="API Server 地址"
      endpoint-help="留空时使用服务端当前的 in-cluster 或 kubeconfig 默认连接。"
      result-label="资源结果上限"
      result-help="单次资源或事件读取允许返回的最大数量。"
      context-label="集群 Console 地址"
      context-help="用于从资源详情跳转到可信的外部集群上下文。"
      @update:model-value="emit('update:modelValue', $event)"
    />
    <UAlert
      color="neutral"
      variant="soft"
      icon="i-lucide-shield"
      title="凭据与权限由服务端管理"
      description="kubeconfig、Context、Impersonation、TLS、QPS/Burst 和资源 allowlist 尚未进入当前前端可写契约；页面不会伪造这些字段。"
    />
  </div>
</template>
